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
)

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
	ClaudePID int       `json:"claudePid,omitempty"`
	Ancestors []int     `json:"ancestors,omitempty"`
	At        time.Time `json:"at"`
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
	Folder    string `json:"folder,omitempty"`
	Profile   string `json:"profile,omitempty"`
	ConfigDir string `json:"configDir,omitempty"`
	ClaudePID int    `json:"claudePid,omitempty"`
	Ancestors []int  `json:"ancestors,omitempty"`
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
	// or a focus that worked. A key flashes it once.
	FocusError string `json:"focusError,omitempty"`
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
	UpdatedAt  time.Time  `json:"updatedAt"`
}

// PlaceholderID is the id a rescan gives a session no hook has named yet.
// The next hook from that process replaces it with the real session id.
func PlaceholderID(pid int) string { return "pid:" + itoa(pid) }

// Placeholder reports whether an id came from a rescan.
func Placeholder(id string) bool { return strings.HasPrefix(id, "pid:") }

// DefaultResolve labels a session by its directory.
func DefaultResolve(cwd string) (label, folder string) {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return "?", ""
	}
	return filepath.Base(cwd), cwd
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

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
