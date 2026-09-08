// Package sessions is the daemon's registry of live Claude Code sessions: one
// entry per interactive `claude`, fed by the hooks `corgi agent track enable`
// installs, kept honest by a reaper, and laid out onto a fixed board of slots
// for a Stream Deck or any other key-per-session surface.
//
// It renders nothing and talks to nobody. The daemon publishes its Snapshot
// as sessions.json; a plugin watches that file; presses come back as spool
// commands. See docs/agent.md, "Sessions on a Stream Deck".
package sessions

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Status is what a session is doing, in the five words a key can show.
type Status string

const (
	// StatusWorking: the model is running or a tool is executing.
	StatusWorking Status = "working"
	// StatusNeedsInput: a permission prompt, a question, or an API failure —
	// the one state that justifies looking at the board.
	StatusNeedsInput Status = "needs_input"
	// StatusDone: the turn finished; the session waits for a prompt.
	StatusDone Status = "done"
	// StatusStale: alive, but nothing has happened for StaleAfter.
	StatusStale Status = "stale"
	// StatusGone: the process exited but the slot is pinned, so it stays
	// reserved and dimmed until unpinned.
	StatusGone Status = "gone"
	// StatusUnknown: found by rescan, no hook has spoken for it yet.
	StatusUnknown Status = "unknown"
	// StatusLimited: the account hit its usage limit. Nothing to answer;
	// Detail says when it resets.
	StatusLimited Status = "limited"
)

var (
	limitText  = regexp.MustCompile(`(?i)\b(session|usage|rate|weekly|daily) limit\b|\brate.?limited?\b`)
	limitReset = regexp.MustCompile(`(?i)\bresets?\s+(?:at\s+)?([^\n"}\\]+?)\s*(?:$|["}\\])`)
)

// LimitReset says whether a StopFailure or notification is the account's
// usage limit rather than something to answer, and when it resets ("12:10pm
// (Europe/Kiev)") when the text says.
func LimitReset(errorType, message string) (bool, string) {
	if errorType != "rate_limit" && !limitText.MatchString(message) {
		return false, ""
	}
	if m := limitReset.FindStringSubmatch(message); m != nil {
		return true, strings.TrimSpace(m[1])
	}
	return true, ""
}

// StaleAfter is how long a working or done session may sit without an event
// before it is called stale. A needs_input session never goes stale: it is
// waiting on a person, and that is the fact worth keeping on the key.
const StaleAfter = 30 * time.Minute

// HostKind says where a session's terminal lives, which decides how focus
// reaches it.
type HostKind string

const (
	// HostVSCodeTerminal: an integrated terminal. Exact window and tab when
	// the corgi VS Code extension is installed; app-level otherwise.
	HostVSCodeTerminal HostKind = "vscode-terminal"
	// HostVSCodePanel: the Claude Code extension's own panel, bound through
	// the extension host's pid.
	HostVSCodePanel HostKind = "vscode-panel"
	// HostITerm and HostTerminalApp: macOS terminal emulators, app-level.
	HostITerm       HostKind = "iterm"
	HostTerminalApp HostKind = "terminal"
	// HostUnknown: nothing recognisable; label from the cwd, no focus target.
	HostUnknown HostKind = "unknown"
)

// Event is one hook firing, as `corgi agent hook emit` writes it to the spool.
// It carries only the fields the registry reads: nothing typed at Claude, no
// tool inputs, no transcript path.
type Event struct {
	// Name is the hook_event_name.
	Name      string `json:"name"`
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd,omitempty"`
	// ConfigDir is CLAUDE_CONFIG_DIR as the hook saw it; empty for the
	// default account.
	ConfigDir string `json:"configDir,omitempty"`
	// Source is SessionStart's startup|resume|clear|compact|fork.
	Source string `json:"source,omitempty"`
	// Reason is SessionEnd's clear|resume|logout|prompt_input_exit|other.
	Reason string `json:"reason,omitempty"`
	// Tool is the tool name on PreToolUse, PostToolUse and PermissionRequest.
	Tool string `json:"tool,omitempty"`
	// Notification is Notification's notification_type.
	Notification string `json:"notification,omitempty"`
	// Message is Claude's own notification text, already trimmed.
	Message string `json:"message,omitempty"`
	// Error is StopFailure's error type.
	Error string `json:"error,omitempty"`
	// Window is CORGI_VSCODE_WINDOW, injected by the corgi VS Code extension
	// into every integrated terminal of its window.
	Window string `json:"window,omitempty"`
	// TermProgram and TermSession are TERM_PROGRAM and the emulator's own
	// session id, for sessions outside an editor.
	TermProgram string `json:"termProgram,omitempty"`
	TermSession string `json:"termSession,omitempty"`
	// ClaudePID is the process the hook belongs to, and Ancestors its parent
	// chain up to init. The reaper probes the first; window binding joins on
	// the second.
	ClaudePID int   `json:"claudePid,omitempty"`
	Ancestors []int `json:"ancestors,omitempty"`
	// Names are the ancestors' process names, in the same order. They say
	// which editor a terminal belongs to ("Cursor Helper (Plugin)") when no
	// extension is there to say so.
	Names []string `json:"names,omitempty"`
	// TTY is the claude process's controlling terminal device, which names
	// the exact iTerm2 or Terminal.app tab.
	TTY uint64    `json:"tty,omitempty"`
	At  time.Time `json:"at"`
}

// Host is where a session's terminal was found.
type Host struct {
	Kind     HostKind `json:"kind"`
	WindowID string   `json:"windowId,omitempty"`
	// App is the editor's application name ("Visual Studio Code", "Cursor"),
	// as its window reported it. What `open -a` is given.
	App string `json:"app,omitempty"`
	// Folder is the workspace folder focus should open: the window's first
	// folder when a window is known, otherwise the registered workspace or
	// the cwd.
	Folder string `json:"folder,omitempty"`
	// ShellPID and Terminal name the integrated terminal tab, when the join
	// found one.
	ShellPID    int    `json:"shellPid,omitempty"`
	Terminal    string `json:"terminal,omitempty"`
	TermProgram string `json:"termProgram,omitempty"`
	// Connected is true while the window's extension record is on disk, so
	// a tab reveal can actually be delivered.
	Connected bool `json:"connected,omitempty"`
}

// Session is one tracked Claude Code process.
type Session struct {
	ID string `json:"id"`
	// Label is the display name: the registered workspace id when the cwd is
	// inside one, else the cwd's base name. Display is the same made unique
	// across live sessions, and is what a key shows.
	Label   string `json:"label"`
	Display string `json:"display,omitempty"`
	Cwd     string `json:"cwd,omitempty"`
	// Folder is the workspace root the label came from, if any.
	Folder    string   `json:"folder,omitempty"`
	Profile   string   `json:"profile,omitempty"`
	ConfigDir string   `json:"configDir,omitempty"`
	ClaudePID int      `json:"claudePid,omitempty"`
	Ancestors []int    `json:"ancestors,omitempty"`
	Names     []string `json:"names,omitempty"`
	TTY       uint64   `json:"tty,omitempty"`
	// Window, TermProgram and TermSession are kept from the hook so the join
	// can be redone whenever the set of windows changes.
	Window      string    `json:"window,omitempty"`
	TermProgram string    `json:"termProgram,omitempty"`
	TermSession string    `json:"termSession,omitempty"`
	Host        Host      `json:"host"`
	Status      Status    `json:"status"`
	StatusSince time.Time `json:"statusSince"`
	// Tool is the running or requested tool; Detail the one line under the
	// label (a permission's tool, an error type, Claude's notification).
	Tool         string    `json:"tool,omitempty"`
	Detail       string    `json:"detail,omitempty"`
	StartedAt    time.Time `json:"startedAt"`
	LastActivity time.Time `json:"lastActivity"`
	// FocusError is the last failed focus attempt, cleared by the next event
	// or a focus that worked. FocusAt is when the attempt was made, so a
	// plugin that reconnects can tell a fresh failure from one it already
	// flashed.
	FocusError string    `json:"focusError,omitempty"`
	FocusAt    time.Time `json:"focusAt,omitempty"`
}

// Terminal is one integrated-terminal tab of an editor window.
type Terminal struct {
	Name     string `json:"name"`
	ShellPID int    `json:"shellPid"`
}

// Window is an editor window as its corgi extension instance reported it.
type Window struct {
	ID         string     `json:"id"`
	App        string     `json:"app,omitempty"`
	ExtHostPID int        `json:"extHostPid"`
	Folders    []string   `json:"folders,omitempty"`
	Terminals  []Terminal `json:"terminals,omitempty"`
	// FocusedAt is when the window last came to the front, as its extension
	// saw it; ActiveShellPID is the shell of its active terminal tab. Together
	// they say which session the user is looking at.
	FocusedAt      time.Time `json:"focusedAt,omitempty"`
	ActiveShellPID int       `json:"activeShellPid,omitempty"`
	// PanelActive is true while the Claude Code panel is the active editor
	// tab: the user is typing there, not in the terminal VS Code still
	// calls active.
	PanelActive bool `json:"panelActive,omitempty"`
	// ClaudeTabs is how many Claude Code panel tabs the window has open, when
	// its extension counts them. A finished panel session beyond that count
	// has no tab left to show it: its chat was closed and Claude Code merely
	// keeps the process for "reopen closed session".
	ClaudeTabs *int      `json:"claudeTabs,omitempty"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// EditorFromChain names the editor whose process tree a session runs in,
// from the ancestor names a hook captured: VS Code's pty host is "Code
// Helper (Plugin)" on macOS and "code" on Linux, Cursor's "Cursor Helper
// (Plugin)" and "cursor", and so on. The kernel's names are cut at 16
// characters, so prefixes are matched. Empty when nothing is recognised.
func EditorFromChain(names []string) string {
	for _, raw := range names {
		name := strings.ToLower(filepath.Base(raw))
		switch {
		case strings.HasPrefix(name, "cursor"):
			return "Cursor"
		case strings.HasPrefix(name, "windsurf"):
			return "Windsurf"
		case strings.HasPrefix(name, "codium"), strings.HasPrefix(name, "vscodium"):
			return "VSCodium"
		case strings.HasPrefix(name, "code - insiders"), strings.HasPrefix(name, "code-insiders"):
			return "Visual Studio Code - Insiders"
		case name == "code", strings.HasPrefix(name, "code helper"):
			return "Visual Studio Code"
		}
	}
	return ""
}

// Within reports whether dir is root or inside it.
func Within(dir, root string) bool {
	dir, root = filepath.Clean(dir), filepath.Clean(root)
	return dir == root || strings.HasPrefix(dir, root+string(filepath.Separator))
}

// PlaceholderID is the id a rescan gives a session no hook has named yet.
// The next hook from that process replaces it with the real session id.
func PlaceholderID(pid int) string { return "pid:" + strconv.Itoa(pid) }

// Placeholder reports whether an id came from a rescan.
func Placeholder(id string) bool { return strings.HasPrefix(id, "pid:") }

// DefaultResolve labels a session by its directory. It names no folder: a
// directory nothing registered is not a root an editor should be told to
// open.
func DefaultResolve(cwd string) (label, folder string) {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return "?", ""
	}
	return filepath.Base(cwd), ""
}

// DefaultProfile turns CLAUDE_CONFIG_DIR into a badge: ~/.claude-work becomes
// "work", an unset dir "default".
func DefaultProfile(configDir string) string {
	base := strings.TrimPrefix(filepath.Base(strings.TrimSpace(configDir)), ".")
	switch base {
	case "", ".", "claude":
		return "default"
	}
	if rest, ok := strings.CutPrefix(base, "claude-"); ok && rest != "" {
		return rest
	}
	return base
}
