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
	"andriiklymiuk/corgi/utils/agent/workspace"

	"github.com/spf13/cobra"
)

// Session tracking is the user-level cousin of `corgi agent hooks`: where
// those notify about one workspace, these tell the daemon what EVERY Claude
// session on the machine is doing, so a Stream Deck (or `corgi agent
// sessions`) can show the board and a key press can land on the right
// window. They live in the account's own settings.json, one per config
// directory, because a session started in a scratch directory is still a
// session you might be waiting on.

const (
	hookEmit     = "corgi agent hook emit"
	hookTabTitle = "corgi agent hook tab"
	// trackMarkers identify corgi's tracking hooks in a settings file, so
	// enable and disable never touch anyone else's.
	trackMarkerEmit = "agent hook emit"
	trackMarkerTab  = "agent hook tab"
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

// trackedEvents is the whole event map. Every entry gets the async emit hook;
// the ones with a title also get the synchronous tab-title hook.
var trackedEvents = []struct {
	Event   string
	Matcher string
	Title   bool
}{
	{Event: "SessionStart", Matcher: "startup|resume|clear|fork"},
	{Event: "UserPromptSubmit", Title: true},
	{Event: "PreToolUse"},
	{Event: "PostToolUse"},
	{Event: "PostToolUseFailure"},
	{Event: "PermissionRequest"},
	{Event: "Notification", Matcher: "permission_prompt|idle_prompt|agent_needs_input|elicitation_dialog|elicitation_url_dialog", Title: true},
	{Event: "Stop", Title: true},
	{Event: "StopFailure", Title: true},
	{Event: "SessionEnd"},
	{Event: "CwdChanged"},
}

func runAgentTrackEnable(cmd *cobra.Command, _ []string) {
	extra, _ := cmd.Flags().GetStringArray("config-dir")
	noTab, _ := cmd.Flags().GetBool("no-tab-title")
	slots, _ := cmd.Flags().GetInt("slots")
	dir := mustAgentDir()
	if slots != 0 {
		if err := setTrackSlots(dir, slots); err != nil {
			exitWithError("agent_track", err, 1)
		}
		utils.Infof("✓ board has %d keys (restart the daemon to apply)\n", slots)
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
	if info, err := daemon.ReadInfo(dir); err != nil || info == nil {
		utils.Info("the daemon is not running — `corgi agent install` starts it at login, `corgi agent serve` now")
	}
	utils.Info("undo with `corgi agent track disable`")
}

func runAgentTrackDisable(cmd *cobra.Command, _ []string) {
	extra, _ := cmd.Flags().GetStringArray("config-dir")
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

// setTrackSlots records the board size in the trusted user config.
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

// trackConfigDirs is every Claude config directory hooks go into: the
// account in use now, each corgi profile's, and any passed explicitly. A
// profile IS a config directory, so a second account never needs naming
// twice.
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
	if profiles, err := loadProfiles(agentDir); err == nil {
		for _, name := range sortedProfileNames(profiles) {
			add(profiles[name].ConfigDir)
		}
	}
	for _, d := range extra {
		add(d)
	}
	return out
}

// defaultClaudeConfigDir is where Claude Code keeps the account in use:
// CLAUDE_CONFIG_DIR, or ~/.claude.
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

// corgiCommandPath is the corgi the hooks should run. An absolute path,
// because a hook fired from an editor launched from the Dock has the Dock's
// PATH, which rarely includes Homebrew. The PATH entry (a stable symlink) is
// preferred over the executable itself, which under Homebrew names a
// versioned Cellar directory that the next upgrade removes.
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

// enableTrackingIn merges the tracking hooks into one settings.json. Unlike
// the per-workspace file, an account's settings.json holds everything the
// person configured, so a file that does not parse is refused rather than
// replaced.
func enableTrackingIn(path, bin string, tabTitle bool) error {
	settings, err := readUserSettings(path)
	if err != nil {
		return err
	}
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	for _, ev := range trackedEvents {
		entries := stripTrackingHooks(hooks[ev.Event])
		handlers := []any{map[string]any{"type": "command", "command": hookCommand(bin, hookEmit), "async": true}}
		if ev.Title && tabTitle {
			handlers = append(handlers, map[string]any{"type": "command", "command": hookCommand(bin, hookTabTitle)})
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
		if strings.Contains(text, trackMarkerEmit) || strings.Contains(text, trackMarkerTab) {
			continue
		}
		out = append(out, entry)
	}
	return out
}

// hasTrackingHooks reports whether a settings file carries the emit hook.
func hasTrackingHooks(path string) bool {
	settings, err := readUserSettings(path)
	if err != nil {
		return false
	}
	return strings.Contains(marshalCompact(settings["hooks"]), trackMarkerEmit)
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

// --- the hooks themselves ---------------------------------------------------

// hookInput is the slice of a hook's stdin the tracking hooks read. Tool
// inputs, prompts and the transcript path are deliberately not here.
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
}

func readHookInput(stdin io.Reader) (hookInput, bool) {
	var in hookInput
	if stdin == nil {
		return in, false
	}
	// The payload carries the whole tool input — a Write of a large file is
	// megabytes — and a truncated one would drop the event, so the cap is
	// generous. Decoding it is the hook's one real cost.
	data, err := io.ReadAll(io.LimitReader(stdin, 64<<20))
	if err != nil || json.Unmarshal(data, &in) != nil {
		return in, false
	}
	return in, in.SessionID != "" && in.Event != ""
}

// errorType reads StopFailure's error however it is shaped: a type string,
// or an object with one.
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
		return firstNonEmptyString(obj.Type, obj.Kind)
	}
	return ""
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// runEmitHook turns one hook firing into a spool entry and a nudge. It
// never prints, never blocks on the daemon and never fails: the session is
// unaffected whatever corgi's state is. Returns what it built, for tests.
func runEmitHook(stdin io.Reader, getenv func(string) string, parent int) (sessions.Event, bool) {
	in, ok := readHookInput(stdin)
	if !ok || in.AgentID != "" {
		// A subagent is not a session; a hook with no session id is not
		// telling corgi anything.
		return sessions.Event{}, false
	}
	if strings.EqualFold(getenv("CLAUDE_CODE_REMOTE"), "true") {
		// Remote sessions have no window on this machine to focus.
		return sessions.Event{}, false
	}
	ev := sessions.Event{
		Name: in.Event, SessionID: in.SessionID, Cwd: in.Cwd,
		ConfigDir: getenv("CLAUDE_CONFIG_DIR"), Source: in.Source, Reason: in.Reason,
		Tool: in.Tool, Notification: in.NotificationType, Message: truncateLine(in.Message, 160),
		Error: in.errorType(), Window: getenv("CORGI_VSCODE_WINDOW"),
		TermProgram: getenv("TERM_PROGRAM"),
		TermSession: firstNonEmptyString(getenv("ITERM_SESSION_ID"), getenv("TERM_SESSION_ID")),
		At:          time.Now().UTC(),
	}
	chain := proc.Ancestors(parent)
	ev.Ancestors = proc.PIDs(chain)
	if owner, ok := proc.Owner(chain); ok {
		ev.ClaudePID = owner.PID
	}
	return ev, true
}

// deliverEvent spools the event for a running daemon and rings its bell.
// No daemon, no file: with hooks on every tool call, a spool nobody drains
// would grow without bound. The next daemon rescans instead.
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

// tabTitle composes the terminal title for an event, or "" for one that
// should leave the title alone. The label is the registered workspace id
// when the cwd is inside one, else the directory name — the same word the
// board shows.
func tabTitle(in hookInput, label string) string {
	switch in.Event {
	case "UserPromptSubmit":
		return "● " + label
	case "Stop":
		return "✓ " + label
	case "StopFailure":
		return "▲ " + label + " FAILED"
	case "Notification":
		switch in.NotificationType {
		case "permission_prompt", "agent_needs_input", "elicitation_dialog", "elicitation_url_dialog":
			return "▲ " + label + " NEEDS YOU"
		}
	}
	return ""
}

// runTabTitleHook prints the terminalSequence Claude Code forwards to the
// terminal: OSC 2 sets the window/tab title. No I/O beyond stdin and stdout;
// it is the synchronous hook, so it must be instant.
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

// workspaceLabel resolves a cwd against the workspace registry: the
// workspace whose root contains it (deepest wins), else the directory name.
// Returns the label and the folder the label stands for.
func workspaceLabel(registry *workspace.Registry, cwd string) (string, string) {
	if registry != nil && cwd != "" {
		best, bestLen := workspace.Workspace{}, -1
		for _, w := range registry.Workspaces {
			if w.AbsPath == "" || len(w.AbsPath) <= bestLen || !pathWithin(cwd, w.AbsPath) {
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

func pathWithin(dir, root string) bool {
	dir, root = filepath.Clean(dir), filepath.Clean(root)
	if dir == root {
		return true
	}
	return strings.HasPrefix(dir, root+string(filepath.Separator))
}

// workspaceResolver is the daemon's label function, reloading the registry
// on demand so a workspace registered after the daemon started still names
// its sessions. The read is cached briefly: a burst of tool events must not
// become a burst of file reads.
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

// profileResolver names a CLAUDE_CONFIG_DIR after the corgi profile that
// points at it, so the badge reads "work" rather than ".claude-work".
func profileResolver(agentDir string) func(configDir string) string {
	return func(configDir string) string {
		profiles, err := loadProfiles(agentDir)
		if err != nil || configDir == "" {
			return ""
		}
		want := expandTilde(configDir)
		names := make([]string, 0, len(profiles))
		for name, p := range profiles {
			if p.ConfigDir != "" && samePath(expandTilde(p.ConfigDir), want) {
				names = append(names, name)
			}
		}
		if len(names) == 0 {
			return ""
		}
		sort.Strings(names)
		return names[0]
	}
}

func init() {
	agentTrackEnableCmd.Flags().StringArray("config-dir", nil, "Also hook this Claude config directory (repeatable); corgi profiles are included automatically")
	agentTrackEnableCmd.Flags().Bool("no-tab-title", false, "Do not set the terminal tab title to the session's status")
	agentTrackEnableCmd.Flags().Int("slots", 0, "Number of keys on the board (default 6, a Stream Deck Mini)")
	agentTrackDisableCmd.Flags().StringArray("config-dir", nil, "Also clean this Claude config directory (repeatable)")
	agentTrackCmd.AddCommand(agentTrackEnableCmd, agentTrackDisableCmd)
	agentCmd.AddCommand(agentTrackCmd)
}
