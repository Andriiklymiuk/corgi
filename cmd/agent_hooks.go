package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/daemon"

	"github.com/spf13/cobra"
)

// corgi writes these into the workspace's LOCAL settings, never the committed
// file.
const (
	hookEventNotification = "Notification"
	hookEventStop         = "Stop"
	hookMarker            = "corgi agent hook"
)

var agentHooksCmd = &cobra.Command{
	Use:   "hooks",
	Short: "Get notified when a session in this workspace needs you",
}

var agentHooksEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Notify when a Claude session here asks for permission or finishes",
	Long: `Writes two Claude Code hooks into .claude/settings.local.json (local to this
machine, never committed): one for permission prompts and questions, one for a
finished turn. Both call ` + "`corgi agent hook`" + `, which tells the corgi
daemon, which sends the same notification as a restart — including the phone
push when notifyUrl is set.

Covers every Claude session in the directory, not just supervised ones.`,
	Run: runAgentHooksEnable,
}

var agentHooksDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Remove the hooks corgi added to this workspace",
	Run:   runAgentHooksDisable,
}

var agentHookCmd = &cobra.Command{
	Use:    "hook",
	Short:  "Internal: called by a Claude Code hook to report a session needs attention",
	Args:   cobra.MaximumNArgs(1),
	Hidden: true,
	Run:    runAgentHook,
}

// samePath compares directories through their symlinks: on macOS a registered
// /var/... path and the same directory as os.Getwd() reports it (/private/var)
// are the same place, and a string compare would call them different.
func samePath(a, b string) bool {
	if a == b {
		return true
	}
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		return false
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		return false
	}
	return ra == rb
}

func claudeLocalSettingsPath(dir string) string {
	return filepath.Join(dir, ".claude", "settings.local.json")
}

func runAgentHooksEnable(_ *cobra.Command, _ []string) {
	cwd, err := os.Getwd()
	if err != nil {
		exitWithError("agent_cwd", err, 1)
	}
	registry, _ := mustLoadRegistry()
	registry.Reconcile(dirIsWorkspace)
	id := ""
	for _, w := range registry.Sorted() {
		if samePath(w.AbsPath, cwd) {
			id = w.ID
			break
		}
	}
	if id == "" {
		exitWithError("agent_hooks", fmt.Errorf(
			"this directory is not a registered workspace — run `corgi agent init` here first"), 2)
	}

	path := claudeLocalSettingsPath(cwd)
	settings := readJSONObject(path)
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	for _, event := range []string{hookEventNotification, hookEventStop} {
		hooks[event] = withCorgiHook(hooks[event], id)
	}
	settings["hooks"] = hooks

	if err := writeJSONObject(path, settings); err != nil {
		exitWithError("agent_hooks", err, 1)
	}
	utils.Infof("✓ %s will notify you when a session here needs you (%s)\n", id, path)
	utils.Info("phone push needs notifyUrl in the agent config — see docs/agent.md")
	utils.Info("undo with `corgi agent hooks disable`")
}

func withCorgiHook(existing any, workspaceID string) []any {
	out := stripCorgiHooks(existing)
	return append(out, map[string]any{
		"hooks": []any{map[string]any{
			"type":    "command",
			"command": fmt.Sprintf("corgi agent hook --workspace %s", workspaceID),
		}},
	})
}

func stripCorgiHooks(existing any) []any {
	list, _ := existing.([]any)
	out := []any{}
	for _, entry := range list {
		if !strings.Contains(marshalCompact(entry), hookMarker) {
			out = append(out, entry)
		}
	}
	return out
}

func runAgentHooksDisable(_ *cobra.Command, _ []string) {
	cwd, err := os.Getwd()
	if err != nil {
		exitWithError("agent_cwd", err, 1)
	}
	path := claudeLocalSettingsPath(cwd)
	settings := readJSONObject(path)
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		utils.Info("no corgi hooks here")
		return
	}
	for _, event := range []string{hookEventNotification, hookEventStop} {
		remaining := stripCorgiHooks(hooks[event])
		if len(remaining) == 0 {
			delete(hooks, event)
			continue
		}
		hooks[event] = remaining
	}
	if len(hooks) == 0 {
		delete(settings, "hooks")
	} else {
		settings["hooks"] = hooks
	}
	if err := writeJSONObject(path, settings); err != nil {
		exitWithError("agent_hooks", err, 1)
	}
	utils.Infof("✓ removed corgi's hooks from %s\n", path)
}

func runAgentHook(cmd *cobra.Command, args []string) {
	utils.NonInteractive = true
	id, _ := cmd.Flags().GetString("workspace")
	if strings.TrimSpace(id) == "" {
		return
	}
	event := ""
	if len(args) > 0 {
		event = args[0]
	}
	detail := hookDetail(event, os.Stdin)

	dir, err := agentDir()
	if err != nil {
		return
	}
	info, err := daemon.ReadInfo(dir)
	if err != nil || info == nil || !info.Commands {
		return // no daemon to tell; the session is unaffected either way
	}
	if _, err := command.Write(dir, command.Command{
		Action: command.ActionAttention, WorkspaceID: id, Detail: detail, Source: "hook",
	}); err != nil {
		return
	}
	daemon.Nudge(info)
}

// Only Claude's own message is used, never session content.
func hookDetail(event string, stdin io.Reader) string {
	msg := ""
	if stdin != nil {
		var payload struct {
			Message string `json:"message"`
			Event   string `json:"hook_event_name"`
		}
		if data, err := io.ReadAll(io.LimitReader(stdin, 8<<10)); err == nil {
			_ = json.Unmarshal(data, &payload)
			msg = strings.TrimSpace(payload.Message)
			if event == "" {
				event = strings.TrimSpace(payload.Event)
			}
		}
	}
	switch {
	case msg != "":
		return truncateLine(msg, 160)
	case event == hookEventStop:
		return "a session finished its turn"
	default:
		return "a session is waiting for you"
	}
}

func truncateLine(s string, max int) string {
	s = strings.TrimSpace(strings.SplitN(s, "\n", 2)[0])
	if len(s) > max {
		return strings.TrimSpace(s[:max]) + "…"
	}
	return s
}

func readJSONObject(path string) map[string]any {
	out := map[string]any{}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	if json.Unmarshal(data, &out) != nil {
		return map[string]any{}
	}
	return out
}

func writeJSONObject(path string, v map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func marshalCompact(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(data)
}

func init() {
	agentHookCmd.Flags().String("workspace", "", "Workspace id the hook belongs to")
	agentHooksCmd.AddCommand(agentHooksEnableCmd, agentHooksDisableCmd)
	agentCmd.AddCommand(agentHooksCmd, agentHookCmd)
}
