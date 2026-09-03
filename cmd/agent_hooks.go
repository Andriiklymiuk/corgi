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
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/daemon"

	"github.com/spf13/cobra"
)

// corgi writes these into the workspace's LOCAL settings, never the committed
// file.
const (
	hookEventNotification = "Notification"
	hookEventStop         = "Stop"
	hookEventPrompt       = "UserPromptSubmit"
	hookMarker            = "corgi agent hook"
)

// corgiHookEvents is every event corgi writes, so enable and disable can never
// disagree about what to clean up.
var corgiHookEvents = []string{hookEventNotification, hookEventStop, hookEventPrompt}

var agentHooksCmd = &cobra.Command{
	Use:   "hooks",
	Short: "Get notified when a session in this workspace needs you",
}

var agentHooksEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Notify when a Claude session here asks for permission or finishes",
	Long: `Writes Claude Code hooks into .claude/settings.local.json (local to this
machine, never committed): one for permission prompts and questions, one that
names a session after the first thing you ask it, and with --turns one for a
finished turn. They call ` + "`corgi agent hook`" + `, which tells the corgi
daemon, which sends the same notification as a restart — including the phone
push when notifyUrl is set.

The naming one answers nothing to the daemon: it replies to Claude Code with a
title, so "corgi · main · 18:55" becomes "corgi · fix the login redirect" as
soon as you say what you want. It only ever replaces a name corgi composed or
Claude Code derived — one you typed, or one it already set, is left alone —
and --no-title skips it entirely.

By default only the first: a permission prompt blocks the session until you
answer it, while a finished turn is just noise once several workspaces are busy.
Add --turns if you do want one on every turn.

Claude also nudges after about a minute of no input, with nothing blocked.
corgi drops that one — it is the notification that arrives when the session
wants nothing, and it is why people stop reading them. --idle keeps it.

Covers every Claude session in the directory, not just supervised ones.

--all does the same for every registered workspace, so a machine with several
stacks does not need one visit each.`,
	Run: runAgentHooksEnable,
}

var agentHooksDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Remove the hooks corgi added to this workspace",
	Long: `Removes the two hooks corgi wrote, leaving any other hooks in the file alone.

--all removes them from every registered workspace.`,
	Run: runAgentHooksDisable,
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

func runAgentHooksEnable(cmd *cobra.Command, _ []string) {
	turns := wantsTurnHook(cmd)
	idle := wantsIdleHook(cmd)
	title := wantsTitleHook(cmd)
	for _, target := range hookTargets(cmd) {
		if err := enableHooksIn(target.dir, target.id, turns, idle, title); err != nil {
			exitWithError("agent_hooks", err, 1)
		}
		utils.Infof("✓ %s will notify you when a session there needs you (%s)\n",
			target.id, claudeLocalSettingsPath(target.dir))
	}
	if turns {
		utils.Info("also notifying on every finished turn (--turns)")
	}
	if idle {
		utils.Info("also notifying when a session has just been idle a while (--idle)")
	}
	if title {
		utils.Info("sessions here will also be named after the first thing you ask them (--no-title to skip)")
	}
	if !notifyURLConfigured() {
		printNotifyUrlHelp()
	}
	utils.Infof("undo with `corgi agent hooks disable%s`\n", allSuffix(cmd))
}

func printNotifyUrlHelp() {
	utils.Info("")
	utils.Info("these reach this machine only. to also get them on your phone:")
	utils.Info("")
	utils.Info("  corgi agent notify telegram --token <TOKEN>   # @BotFather → /newbot")
	utils.Info("  corgi agent notify set <slack-or-discord-webhook-url>")
	utils.Info("")
	utils.Info("  then: corgi agent restart")
}

// The title hook is on by default: a list of sessions all called after their
// repo is the thing people ask corgi to fix, and the hook only ever renames a
// session corgi named itself.
func wantsTitleHook(cmd *cobra.Command) bool {
	if cmd == nil {
		return true
	}
	skip, _ := cmd.Flags().GetBool("no-title")
	return !skip
}

func wantsIdleHook(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	idle, _ := cmd.Flags().GetBool("idle")
	return idle
}

func wantsTurnHook(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	turns, _ := cmd.Flags().GetBool("turns")
	return turns
}

func agentDirOrEmpty() string {
	dir, err := agentDir()
	if err != nil {
		return ""
	}
	return dir
}

func notifyURLConfigured() bool {
	dir := agentDirOrEmpty()
	if dir == "" {
		return false
	}
	user, err := config.LoadUser(agentUserConfigPath(dir))
	if err != nil {
		return false
	}
	return strings.TrimSpace(user.NotifyUrl) != ""
}

type hookTarget struct {
	id  string
	dir string
}

func hookTargets(cmd *cobra.Command) []hookTarget {
	registry, _ := mustLoadRegistry()
	registry.Reconcile(dirIsWorkspace)

	if wantsAllWorkspaces(cmd) {
		var targets []hookTarget
		for _, w := range registry.Sorted() {
			if w.AbsPath == "" {
				continue
			}
			targets = append(targets, hookTarget{id: w.ID, dir: w.AbsPath})
		}
		if len(targets) == 0 {
			exitWithError("agent_hooks", fmt.Errorf(
				"no registered workspaces — run `corgi agent init` in a repo first"), 2)
		}
		return targets
	}

	cwd, err := os.Getwd()
	if err != nil {
		exitWithError("agent_cwd", err, 1)
	}
	for _, w := range registry.Sorted() {
		if samePath(w.AbsPath, cwd) {
			return []hookTarget{{id: w.ID, dir: cwd}}
		}
	}
	exitWithError("agent_hooks", fmt.Errorf(
		"this directory is not a registered workspace — run `corgi agent init` here first, or pass --all"), 2)
	return nil
}

func wantsAllWorkspaces(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	all, _ := cmd.Flags().GetBool("all")
	return all
}

func allSuffix(cmd *cobra.Command) string {
	if wantsAllWorkspaces(cmd) {
		return " --all"
	}
	return ""
}

func enableHooksIn(dir, id string, turns, idle, title bool) error {
	path := claudeLocalSettingsPath(dir)
	settings := readJSONObject(path)
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}

	wanted := map[string]bool{hookEventNotification: true, hookEventStop: turns, hookEventPrompt: title}
	for _, event := range corgiHookEvents {
		if wanted[event] {
			hooks[event] = withCorgiHook(hooks[event], id, event, idle)
			continue
		}
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
	return writeJSONObject(path, settings)
}

func withCorgiHook(existing any, workspaceID, event string, idle bool) []any {
	out := stripCorgiHooks(existing)
	// The choice rides in the command corgi writes, so it is per workspace and
	// visible in the settings file rather than hidden in another config.
	command := fmt.Sprintf("corgi agent hook --workspace %s", workspaceID)
	if event == hookEventPrompt {
		command += " title"
	}
	if idle && event != hookEventPrompt {
		command += " --idle"
	}
	return append(out, map[string]any{
		"hooks": []any{map[string]any{
			"type":    "command",
			"command": command,
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

func runAgentHooksDisable(cmd *cobra.Command, _ []string) {
	for _, target := range hookTargets(cmd) {
		removed, err := disableHooksIn(target.dir)
		if err != nil {
			exitWithError("agent_hooks", err, 1)
		}
		if !removed {
			utils.Infof("%s had no corgi hooks\n", target.id)
			continue
		}
		utils.Infof("✓ removed corgi's hooks from %s\n", claudeLocalSettingsPath(target.dir))
	}
}

func disableHooksIn(dir string) (bool, error) {
	path := claudeLocalSettingsPath(dir)
	settings := readJSONObject(path)
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		return false, nil
	}
	for _, event := range corgiHookEvents {
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
	return true, writeJSONObject(path, settings)
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
	if event == "title" {
		// The one hook that answers rather than reports: it prints a session
		// title on stdout and tells the daemon nothing.
		runAgentTitleHook(id, titleHookStdin, os.Stdout)
		return
	}
	detail := hookDetail(event, os.Stdin)

	// Claude Code fires Notification for two different things: a permission
	// prompt, which blocks the session until someone answers, and a 60-second
	// idle nudge, which blocks nothing at all. Only the first is worth a toast
	// on your desk or a push to your phone — the second arrives when the
	// session is simply sitting at its prompt, and reading "waiting for your
	// input" for something that wants nothing is how people learn to ignore
	// every notification corgi sends.
	if idle, _ := cmd.Flags().GetBool("idle"); isIdleNudge(detail) && !idle {
		return
	}

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

// isIdleNudge recognises Claude Code's "nothing is blocked, you have just been
// away" message. Matched on the message rather than the event name because the
// event is Notification for both kinds.
func isIdleNudge(detail string) bool {
	return strings.Contains(strings.ToLower(detail), "waiting for your input")
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
	agentHookCmd.Flags().Bool("idle", false, "Report Claude's idle nudge too, not just a prompt that is actually blocking")
	agentHooksEnableCmd.Flags().Bool("all", false, "Apply to every registered workspace, not just this directory")
	agentHooksEnableCmd.Flags().Bool("turns", false, "Also notify when a session finishes a turn (noisy across several workspaces)")
	agentHooksEnableCmd.Flags().Bool("idle", false, "Also notify on Claude's \"waiting for your input\" nudge, which fires when nothing is actually blocked")
	agentHooksEnableCmd.Flags().Bool("no-title", false, "Do not name sessions after the first thing you ask them")
	agentHooksDisableCmd.Flags().Bool("all", false, "Apply to every registered workspace, not just this directory")
	agentHooksCmd.AddCommand(agentHooksEnableCmd, agentHooksDisableCmd)
	agentCmd.AddCommand(agentHooksCmd, agentHookCmd)
}
