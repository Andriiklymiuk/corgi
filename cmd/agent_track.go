package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/proc"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/usage"
	"andriiklymiuk/corgi/utils/agent/watch"
	"andriiklymiuk/corgi/utils/agent/workspace"

	"github.com/spf13/cobra"
)

const (
	flagConfigDir      = "config-dir"
	hookEmit           = "corgi agent hook emit"
	hookTabTitle       = "corgi agent hook tab"
	hookContext        = "corgi agent hook context"
	hookScope          = "corgi agent hook scope"
	hookBudget         = "corgi agent hook budget"
	trackMarkerEmit    = "agent hook emit"
	trackMarkerTab     = "agent hook tab"
	trackMarkerContext = "agent hook context"
	trackMarkerScope   = "agent hook scope"
	trackMarkerBudget  = "agent hook budget"
	writingTools       = "Edit|Write|MultiEdit|NotebookEdit"
	promptingTools     = "Bash|Edit|Write|MultiEdit|NotebookEdit|Task|WebFetch|WebSearch|AskUserQuestion"
)

var agentTrackCmd = &cobra.Command{
	Use:   "track",
	Short: "Track every Claude Code session on this machine for a Stream Deck or the CLI",
	Long: `Session tracking installs hooks into your Claude Code account settings
(~/.claude/settings.json, and each profile's config directory) that report
every session's status to the corgi daemon: working, needs you, done, idle.
The daemon keeps them on a fixed board of keys and publishes it as
sessions.json for a Stream Deck plugin; ` + "`corgi agent sessions`" + ` prints the
same board and ` + "`corgi agent focus`" + ` brings a session's window to the front.

Every hook is asynchronous and exits 0 whatever happens: a daemon that is
down costs nothing and shows nothing in the transcript. The one exception
prints a terminal title (● repo / ▲ repo NEEDS YOU) into the tab the session
runs in, which VS Code and most terminals show — ` + "`--no-tab-title`" + ` skips it.

  corgi agent track enable                  # this account, plus every corgi profile
  corgi agent track enable --config-dir ~/.claude-work
  corgi agent track enable --slots 15       # a bigger deck
  corgi agent sessions                      # the board
  corgi agent focus acme-api                # the window and tab it runs in`,
}

var agentTrackEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Install the session-tracking hooks into your Claude account settings",
	Run:   runAgentTrackEnable,
}

var agentTrackDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Remove the session-tracking hooks, leaving every other hook alone",
	Run:   runAgentTrackDisable,
}

var trackedEvents = []struct {
	Event   string
	Matcher string
	Title   bool
	Context bool
	Scope   bool
	Budget  bool
	NoEmit  bool
}{
	{Event: "SessionStart", Matcher: "startup|resume|clear|fork", Context: true},
	{Event: "UserPromptSubmit", Title: true},
	{Event: "PreToolUse", Matcher: promptingTools},
	{Event: "PreToolUse", Matcher: writingTools, Scope: true, NoEmit: true},
	{Event: "PostToolUse"},
	{Event: "PostToolUseFailure"},
	{Event: "PermissionRequest"},
	{Event: "Notification", Matcher: "permission_prompt|idle_prompt|agent_needs_input|elicitation_dialog|elicitation_url_dialog", Title: true},
	{Event: "Stop", Title: true, Budget: true},
	{Event: "StopFailure", Title: true},
	{Event: "SessionEnd"},
	{Event: "CwdChanged"},
}

func runAgentTrackEnable(cmd *cobra.Command, _ []string) {
	extra, _ := cmd.Flags().GetStringArray(flagConfigDir)
	noTab, _ := cmd.Flags().GetBool("no-tab-title")
	slots, _ := cmd.Flags().GetInt("slots")
	dir := mustAgentDir()
	if slots != 0 {
		if err := setTrackSlots(dir, slots); err != nil {
			exitWithError("agent_track", err, 1)
		}
		if resizeRunningBoard(dir, slots) {
			utils.Infof("✓ board has %d keys\n", slots)
		} else {
			utils.Infof("✓ board will have %d keys once the daemon runs\n", slots)
		}
	}
	bin := corgiCommandPath()
	for _, cfgDir := range trackConfigDirs(dir, extra) {
		path := claudeUserSettingsPath(cfgDir)
		if err := enableTrackingIn(path, bin, !noTab); err != nil {
			exitWithError("agent_track", err, 1)
		}
		utils.Infof("✓ sessions under %s are tracked (%s)\n", cfgDir, path)
	}
	utils.Info("new sessions report from their next event; `corgi agent sessions` shows the board")
	if !noTab {
		utils.Info("VS Code shows the tab titles once terminal.integrated.tabs.title is \"${sequence}\" (the corgi extension offers this)")
	}
	if info, err := daemon.ReadInfo(dir); err != nil || info == nil {
		utils.Info("the daemon is not running — `corgi agent install` starts it at login, `corgi agent serve` now")
	}
	utils.Info("undo with `corgi agent track disable`")
}

func runAgentTrackDisable(cmd *cobra.Command, _ []string) {
	extra, _ := cmd.Flags().GetStringArray(flagConfigDir)
	dir := mustAgentDir()
	for _, cfgDir := range trackConfigDirs(dir, extra) {
		path := claudeUserSettingsPath(cfgDir)
		removed, err := disableTrackingIn(path)
		if err != nil {
			exitWithError("agent_track", err, 1)
		}
		if removed {
			utils.Infof("✓ removed corgi's tracking hooks from %s\n", path)
		} else {
			utils.Infof("%s had no tracking hooks\n", path)
		}
	}
}

func resizeRunningBoard(dir string, slots int) bool {
	info, err := daemon.ReadInfo(dir)
	if err != nil || info == nil || !info.Commands {
		return false
	}
	if _, err := command.Write(dir, command.Command{Action: command.ActionResize, Size: slots, Source: "cli"}); err != nil {
		return false
	}
	daemon.Nudge(info)
	return true
}

func setTrackSlots(dir string, slots int) error {
	if slots < 1 || slots > 64 {
		return fmt.Errorf("--slots must be between 1 and 64, got %d", slots)
	}
	path := agentUserConfigPath(dir)
	user, err := config.LoadUser(path)
	if err != nil {
		return err
	}
	user.TrackSlots = slots
	return writeUserConfig(path, user)
}

func trackConfigDirs(agentDir string, extra []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(d string) {
		d = expandTilde(strings.TrimSpace(d))
		if d == "" {
			return
		}
		if abs, err := filepath.Abs(d); err == nil {
			d = abs
		}
		if seen[d] {
			return
		}
		seen[d] = true
		out = append(out, d)
	}
	add(defaultClaudeConfigDir())
	if user, err := config.LoadUser(agentUserConfigPath(agentDir)); err == nil && user != nil {
		for _, name := range sortedProfileNames(user.Profiles) {
			add(user.Profiles[name].ConfigDir)
		}
		add(user.Defaults.ConfigDir)
		for _, id := range sortedProfileNames(user.Workspaces) {
			add(user.Workspaces[id].ConfigDir)
		}
	}
	for _, d := range extra {
		add(d)
	}
	return out
}

func defaultClaudeConfigDir() string {
	if d := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}

func claudeUserSettingsPath(configDir string) string {
	return filepath.Join(configDir, "settings.json")
}

func corgiCommandPath() string {
	if p, err := exec.LookPath("corgi"); err == nil {
		if abs, err := filepath.Abs(p); err == nil {
			return abs
		}
		return p
	}
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return "corgi"
}

func hookCommand(bin, hook string) string {
	rest := strings.TrimPrefix(hook, "corgi ")
	if strings.ContainsAny(bin, " \t\"'") {
		return fmt.Sprintf("%q %s", bin, rest)
	}
	return bin + " " + rest
}

func enableTrackingIn(path, bin string, tabTitle bool) error {
	settings, err := readUserSettings(path)
	if err != nil {
		return err
	}
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	seen := map[string]bool{}
	for _, ev := range trackedEvents {
		var entries []any
		if !seen[ev.Event] {
			entries = stripTrackingHooks(hooks[ev.Event])
			seen[ev.Event] = true
		} else {
			entries, _ = hooks[ev.Event].([]any)
		}
		var handlers []any
		if !ev.NoEmit {
			handlers = append(handlers, map[string]any{"type": "command", "command": hookCommand(bin, hookEmit), "async": true})
		}
		if ev.Title && tabTitle {
			handlers = append(handlers, map[string]any{"type": "command", "command": hookCommand(bin, hookTabTitle)})
		}
		if ev.Context {
			handlers = append(handlers, map[string]any{"type": "command", "command": hookCommand(bin, hookContext), "timeout": 5})
		}
		if ev.Scope {
			handlers = append(handlers, map[string]any{"type": "command", "command": hookCommand(bin, hookScope), "timeout": 5})
		}
		if ev.Budget {
			handlers = append(handlers, map[string]any{"type": "command", "command": hookCommand(bin, hookBudget), "timeout": 15})
		}
		entry := map[string]any{"hooks": handlers}
		if ev.Matcher != "" {
			entry["matcher"] = ev.Matcher
		}
		hooks[ev.Event] = append(entries, entry)
	}
	settings["hooks"] = hooks
	return writeJSONObject(path, settings)
}

func disableTrackingIn(path string) (bool, error) {
	settings, err := readUserSettings(path)
	if err != nil {
		return false, err
	}
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		return false, nil
	}
	removed := false
	for event, existing := range hooks {
		remaining := stripTrackingHooks(existing)
		if list, _ := existing.([]any); len(list) == len(remaining) {
			continue
		}
		removed = true
		if len(remaining) == 0 {
			delete(hooks, event)
			continue
		}
		hooks[event] = remaining
	}
	if !removed {
		return false, nil
	}
	if len(hooks) == 0 {
		delete(settings, "hooks")
	} else {
		settings["hooks"] = hooks
	}
	return true, writeJSONObject(path, settings)
}

func stripTrackingHooks(existing any) []any {
	list, _ := existing.([]any)
	out := []any{}
	for _, entry := range list {
		text := marshalCompact(entry)
		if strings.Contains(text, trackMarkerEmit) || strings.Contains(text, trackMarkerTab) || strings.Contains(text, trackMarkerContext) || strings.Contains(text, trackMarkerScope) || strings.Contains(text, trackMarkerBudget) {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func hasTrackingHooks(path string) bool {
	settings, err := readUserSettings(path)
	if err != nil {
		return false
	}
	return strings.Contains(marshalCompact(settings["hooks"]), trackMarkerEmit)
}

func trackingHooksStale(path string) bool {
	settings, err := readUserSettings(path)
	if err != nil {
		return false
	}
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		return false
	}
	tab := strings.Contains(marshalCompact(hooks), trackMarkerTab)
	for _, ev := range trackedEvents {
		if !eventHookCurrent(hooks[ev.Event], ev.Matcher, ev.Context, ev.Title && tab, ev.Scope, ev.Budget, ev.NoEmit) {
			return true
		}
	}
	return false
}

func eventHookCurrent(existing any, matcher string, wantContext, wantTab, wantScope, wantBudget, noEmit bool) bool {
	list, _ := existing.([]any)
	for _, entry := range list {
		text := marshalCompact(entry)
		obj, _ := entry.(map[string]any)
		got, _ := obj["matcher"].(string)
		mine := strings.Contains(text, trackMarkerEmit) || strings.Contains(text, trackMarkerScope) || strings.Contains(text, trackMarkerBudget)
		if !mine || got != matcher {
			continue
		}
		if noEmit == strings.Contains(text, trackMarkerEmit) {
			return false
		}
		if wantContext != strings.Contains(text, trackMarkerContext) {
			return false
		}
		if wantScope != strings.Contains(text, trackMarkerScope) || wantBudget != strings.Contains(text, trackMarkerBudget) {
			return false
		}
		return wantTab == strings.Contains(text, trackMarkerTab)
	}
	return false
}

func readUserSettings(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if len(strings.TrimSpace(string(data))) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON (%v) — fix it before corgi adds hooks to it", path, err)
	}
	return out, nil
}

type hookInput struct {
	SessionID        string          `json:"session_id"`
	Event            string          `json:"hook_event_name"`
	Cwd              string          `json:"cwd"`
	AgentID          string          `json:"agent_id"`
	Source           string          `json:"source"`
	Reason           string          `json:"reason"`
	Tool             string          `json:"tool_name"`
	NotificationType string          `json:"notification_type"`
	Message          string          `json:"message"`
	ErrorType        string          `json:"error_type"`
	Error            json.RawMessage `json:"error"`
	ToolInput        json.RawMessage `json:"tool_input"`
	TranscriptPath   string          `json:"transcript_path"`
}

func (in hookInput) readsTranscript() (context, title bool) {
	switch in.Event {
	case "Stop", "StopFailure":
		return true, true
	case "SessionStart":
		return in.Source != "startup", true
	case "PostToolUse", "PostToolUseFailure", "UserPromptSubmit":
		return true, false
	}
	return false, false
}

func readHookInput(stdin io.Reader) (hookInput, bool) {
	var in hookInput
	if stdin == nil {
		return in, false
	}
	data, err := io.ReadAll(io.LimitReader(stdin, 64<<20))
	if err != nil || json.Unmarshal(data, &in) != nil {
		return in, false
	}
	return in, in.SessionID != "" && in.Event != ""
}

func (in hookInput) errorType() string {
	if in.ErrorType != "" {
		return in.ErrorType
	}
	var s string
	if json.Unmarshal(in.Error, &s) == nil {
		return s
	}
	var obj struct {
		Type string `json:"type"`
		Kind string `json:"kind"`
	}
	if json.Unmarshal(in.Error, &obj) == nil {
		return firstNonEmpty(obj.Type, obj.Kind)
	}
	return ""
}

func (in hookInput) errorMessage() string {
	if in.Message != "" {
		return in.Message
	}
	var s string
	if json.Unmarshal(in.Error, &s) == nil {
		return s
	}
	var obj struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(in.Error, &obj) == nil && obj.Message != "" {
		return obj.Message
	}
	return strings.TrimSpace(string(in.Error))
}

func runEmitHook(stdin io.Reader, getenv func(string) string, parent int) (sessions.Event, bool) {
	in, ok := readHookInput(stdin)
	if !ok || in.AgentID != "" {
		return sessions.Event{}, false
	}
	if strings.EqualFold(getenv("CLAUDE_CODE_REMOTE"), "true") {
		return sessions.Event{}, false
	}
	ev := sessions.Event{
		Name: in.Event, SessionID: in.SessionID, Cwd: in.Cwd,
		ConfigDir: getenv("CLAUDE_CONFIG_DIR"), Source: in.Source, Reason: in.Reason,
		Tool: in.Tool, Notification: in.NotificationType, Message: truncateLine(in.errorMessage(), 160),
		Error: in.errorType(), Window: getenv("CORGI_VSCODE_WINDOW"),
		Ticket: getenv("CORGI_TICKET"), TicketKey: getenv("CORGI_TICKET_KEY"), Bot: getenv("CORGI_BOT"),
		Attempt:     getenv("CORGI_ATTEMPT"),
		TermProgram: getenv("TERM_PROGRAM"),
		TermSession: firstNonEmpty(getenv("ITERM_SESSION_ID"), getenv("TERM_SESSION_ID")),
		TmuxPane:    getenv("TMUX_PANE"),
		Subject:     subjectOf(in.Tool, in.ToolInput),
		Risk:        riskOf(in.Tool, in.ToolInput),
		At:          time.Now().UTC(),
	}
	if wantContext, wantTitle := in.readsTranscript(); in.TranscriptPath != "" && (wantContext || wantTitle) {
		if wantContext {
			if c, ok := usage.ContextOf(in.TranscriptPath); ok {
				ev.Context = &c
			}
		}
		if wantTitle {
			ev.Title = usage.TitleOf(in.TranscriptPath)
		}
		if in.Event == "Stop" {
			if sum, ok := usage.SummaryOf(in.TranscriptPath); ok {
				ev.Summary, ev.PR = sum.Line, sum.PR
			}
		}
	}
	switch in.Event {
	case "SessionStart", "UserPromptSubmit", "Stop", "CwdChanged":
		ev.Branch = sessions.Branch(in.Cwd)
	}
	chain := proc.Ancestors(parent)
	if proc.HasCorgi(chain) {
		return sessions.Event{}, false
	}
	ev.Ancestors = proc.PIDs(chain)
	ev.Names = proc.Names(chain)
	if owner, ok := proc.Owner(chain); ok {
		ev.ClaudePID = owner.PID
		ev.TTY = owner.TTY
	}
	return ev, true
}

func deliverEvent(ev sessions.Event) {
	dir, err := agentDir()
	if err != nil {
		return
	}
	info, err := daemon.ReadInfo(dir)
	if err != nil || info == nil || !info.Commands {
		return
	}
	if _, err := command.Write(dir, command.Command{Action: command.ActionSession, Event: &ev, Source: "hook"}); err != nil {
		return
	}
	daemon.Nudge(info)
}

func tabTitle(in hookInput, label string) string {
	switch in.Event {
	case "UserPromptSubmit":
		return "● " + label
	case "Stop":
		if in.TranscriptPath != "" {
			if c, ok := usage.ContextOf(in.TranscriptPath); ok && c.Percent >= 50 {
				return fmt.Sprintf("✓ %s %d%%", label, c.Percent)
			}
		}
		return "✓ " + label
	case "StopFailure":
		if limited, reset := sessions.LimitReset(in.errorType(), in.errorMessage()); limited {
			return "⏳ " + label + " LIMIT " + shortReset(reset)
		}
		return "▲ " + label + " FAILED"
	case "Notification":
		switch in.NotificationType {
		case "permission_prompt", "agent_needs_input", "elicitation_dialog", "elicitation_url_dialog":
			return "▲ " + label + " NEEDS YOU"
		}
	}
	return ""
}

func shortReset(reset string) string {
	if i := strings.Index(reset, " ("); i > 0 {
		reset = reset[:i]
	}
	return strings.TrimSpace(reset)
}

func runTabTitleHook(stdin io.Reader, stdout io.Writer, label func(cwd string) string) {
	in, ok := readHookInput(stdin)
	if !ok || in.AgentID != "" {
		return
	}
	title := tabTitle(in, label(in.Cwd))
	if title == "" {
		return
	}
	title = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, title)
	_ = json.NewEncoder(stdout).Encode(map[string]string{
		"terminalSequence": "\x1b]2;" + title + "\x07",
	})
}

func workspaceLabel(registry *workspace.Registry, cwd string) (string, string) {
	if registry != nil && cwd != "" {
		best, bestLen := workspace.Workspace{}, -1
		for _, w := range registry.Workspaces {
			if w.AbsPath == "" || len(w.AbsPath) <= bestLen || !sessions.Within(cwd, w.AbsPath) {
				continue
			}
			best, bestLen = w, len(w.AbsPath)
		}
		if bestLen >= 0 {
			return best.ID, best.AbsPath
		}
	}
	return sessions.DefaultResolve(cwd)
}

func workspaceResolver(agentDir string) func(cwd string) (string, string) {
	var (
		cached   *workspace.Registry
		loadedAt time.Time
	)
	return func(cwd string) (string, string) {
		if cached == nil || time.Since(loadedAt) > 10*time.Second {
			cached, _ = workspace.Load(agentRegistryPath(agentDir))
			loadedAt = time.Now()
		}
		return workspaceLabel(cached, cwd)
	}
}

func profileResolver(agentDir string) func(configDir string) string {
	var (
		cached   map[string]config.WorkspaceConfig
		loadedAt time.Time
	)
	return func(configDir string) string {
		if configDir == "" {
			return ""
		}
		if cached == nil || time.Since(loadedAt) > 10*time.Second {
			cached, _ = loadProfiles(agentDir)
			loadedAt = time.Now()
		}
		return profileNamed(cached, expandTilde(configDir))
	}
}

func pullResolver(agentDir string) func(link string) (sessions.PullFacts, bool) {
	var (
		cached   *watch.PullLog
		loadedAt time.Time
	)
	return func(link string) (sessions.PullFacts, bool) {
		if cached == nil || time.Since(loadedAt) > 10*time.Second {
			cached = watch.LoadPullLog(agentDir)
			loadedAt = time.Now()
		}
		st, ok := cached.Get(link)
		if !ok {
			return sessions.PullFacts{}, false
		}
		return st.Facts(), true
	}
}

func profileNamed(profiles map[string]config.WorkspaceConfig, dir string) string {
	var names []string
	for name, p := range profiles {
		if p.ConfigDir != "" && samePath(expandTilde(p.ConfigDir), dir) {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	return names[0]
}

func init() {
	agentTrackEnableCmd.Flags().StringArray(flagConfigDir, nil, "Also hook this Claude config directory (repeatable); corgi profiles are included automatically")
	agentTrackEnableCmd.Flags().Bool("no-tab-title", false, "Do not set the terminal tab title to the session's status")
	agentTrackEnableCmd.Flags().Int("slots", 0, "Number of keys on the board (default 6, a Stream Deck Mini)")
	agentTrackDisableCmd.Flags().StringArray(flagConfigDir, nil, "Also clean this Claude config directory (repeatable)")
	agentTrackCmd.AddCommand(agentTrackEnableCmd, agentTrackDisableCmd)
	agentCmd.AddCommand(agentTrackCmd)
}
