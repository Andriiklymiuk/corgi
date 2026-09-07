package sessions

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils/agent/proc"
	"andriiklymiuk/corgi/utils/atomicfile"
)

// Registry holds every tracked session and the board they sit on. One mutex,
// in memory, persisted as the same JSON the plugin reads.
type Registry struct {
	// Resolve labels a session from its cwd: the registered workspace id and
	// its root when the cwd is inside one, otherwise the directory itself.
	// Injected by cmd, which owns the workspace registry.
	Resolve func(cwd string) (label, folder string)
	// ProfileFor names the badge for a CLAUDE_CONFIG_DIR: a corgi profile
	// name when one points at that directory, else the directory's own name.
	ProfileFor func(configDir string) string

	mu sync.Mutex
	// saveMu orders writers: the command loop, the reaper and a focus
	// goroutine all Save, and two snapshots racing for the same .tmp file
	// could leave the older one on disk.
	saveMu    sync.Mutex
	path      string
	sessions  map[string]*Session
	board     Board
	windows   map[string]Window
	updatedAt time.Time
	dirty     bool
}

// State is the published board: what sessions.json holds and what
// `corgi agent sessions --json` prints.
type State struct {
	UpdatedAt time.Time `json:"updatedAt"`
	Size      int       `json:"size"`
	// Overflow is how many sessions have no key of their own.
	Overflow int `json:"overflow"`
	// NeedsInput and Working count sessions in those states, wherever they
	// sit — a pager key or a status bar can say "2 waiting" without walking
	// the board.
	NeedsInput int       `json:"needsInput"`
	Working    int       `json:"working"`
	Slots      []Slot    `json:"slots"`
	Sessions   []Session `json:"sessions"`
	Windows    []Window  `json:"windows,omitempty"`
}

// Slot is one key, ready to draw.
type Slot struct {
	Index int  `json:"index"`
	Empty bool `json:"empty,omitempty"`
	// Pager marks the "+N" key; Overflow is that N.
	Pager     bool     `json:"pager,omitempty"`
	Overflow  int      `json:"overflow,omitempty"`
	SessionID string   `json:"sessionId,omitempty"`
	Label     string   `json:"label,omitempty"`
	Profile   string   `json:"profile,omitempty"`
	Status    Status   `json:"status,omitempty"`
	Pinned    bool     `json:"pinned,omitempty"`
	ElapsedS  int      `json:"elapsedS,omitempty"`
	Detail    string   `json:"detail,omitempty"`
	Host      HostKind `json:"host,omitempty"`
	// FocusError is set when the last press on this key could not land;
	// FocusAt says when.
	FocusError string    `json:"focusError,omitempty"`
	FocusAt    time.Time `json:"focusAt,omitempty"`
}

// New returns a registry persisted at path, with a board of size keys.
func New(path string, size int) *Registry {
	return &Registry{
		path:       path,
		sessions:   map[string]*Session{},
		board:      NewBoard(size),
		windows:    map[string]Window{},
		Resolve:    DefaultResolve,
		ProfileFor: DefaultProfile,
	}
}

// Path is where the registry persists.
func (r *Registry) Path() string { return r.path }

// Load restores a previous daemon's board so a restart keeps the keys where
// they were. Windows are not restored: their extensions re-register. A file
// that is missing or unreadable is an empty board, never an error worth
// refusing to start over.
func (r *Registry) Load() {
	data, err := os.ReadFile(r.path)
	if err != nil {
		return
	}
	var st State
	if json.Unmarshal(data, &st) != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// The configured size wins over the file's: a `--slots` change applies
	// on the next start, and seats past the new edge spill into overflow.
	size := r.board.Size
	r.board = NewBoard(size)
	for i := range st.Sessions {
		s := st.Sessions[i]
		if s.ID == "" || s.ClaudePID <= 0 {
			// Nothing to probe: a session with no pid could sit on a key
			// forever. Its next event brings it back.
			continue
		}
		r.sessions[s.ID] = &s
	}
	for _, sl := range st.Slots {
		if sl.SessionID == "" || sl.Index < 0 || sl.Index >= size || r.sessions[sl.SessionID] == nil {
			continue
		}
		r.board.Slots[sl.Index] = sl.SessionID
		r.board.Pinned[sl.Index] = sl.Pinned
	}
	for _, s := range r.sortedLocked() {
		r.board.Place(s.ID)
		// No window has reconnected yet: a host restored as connected would
		// send reveal requests nobody reads.
		r.bind(s)
	}
	r.dirty = true
}

// Save writes the board when something changed. Cheap to call after every
// drain: a batch of twenty tool events is one write.
func (r *Registry) Save() error {
	r.saveMu.Lock()
	defer r.saveMu.Unlock()
	r.mu.Lock()
	if !r.dirty {
		r.mu.Unlock()
		return nil
	}
	r.dirty = false
	st := r.snapshotLocked(time.Now())
	r.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(r.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	// 0600: cwds, pids and window ids are the owner's business.
	return atomicfile.Write(r.path, data, 0o600)
}

// Resize changes the board, keeping every seat that still fits.
func (r *Registry) Resize(size int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if size == r.board.Size {
		return
	}
	r.board.Resize(size)
	r.touch()
}

// Apply folds one hook event into the registry. Returns whether anything
// visible changed.
func (r *Registry) Apply(ev Event) bool {
	if ev.SessionID == "" || ev.Name == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := ev.At
	if now.IsZero() {
		now = time.Now()
	}

	s := r.sessions[ev.SessionID]
	created := s == nil
	if created {
		if ev.Name == "SessionEnd" {
			return false
		}
		s = r.adoptLocked(ev, now)
	}
	before := s.visible()
	r.refresh(s, ev)
	s.LastActivity = now
	s.FocusError = ""

	r.transition(s, ev, now)
	// A tool call that changes nothing a key shows must not rewrite the
	// file: twenty of them in a row would be twenty redraws for nothing.
	if created || r.sessions[s.ID] == nil || s.visible() != before {
		r.touch()
		return true
	}
	return false
}

// visible is the part of a session a key draws, compared to decide whether
// an event is worth publishing.
type visible struct {
	Label, Profile, Detail, Tool string
	Status                       Status
	Host                         Host
}

func (s *Session) visible() visible {
	return visible{Label: s.Label, Profile: s.Profile, Detail: s.Detail, Tool: s.Tool, Status: s.Status, Host: s.Host}
}

// transition is the status model: what each hook event means for a session.
func (r *Registry) transition(s *Session, ev Event, now time.Time) {
	switch ev.Name {
	case "SessionStart":
		r.applyStart(s, ev, now)
	case "UserPromptSubmit":
		s.Tool, s.Detail = "", ""
		r.setStatus(s, StatusWorking, now)
	case "PreToolUse":
		r.applyToolStart(s, ev, now)
	case "PostToolUse", "PostToolUseFailure":
		// A tool that finished means whatever prompt preceded it was
		// answered.
		s.Tool = ""
		if s.Detail == ev.Tool || s.Status == StatusNeedsInput {
			s.Detail = ""
		}
		r.setStatus(s, StatusWorking, now)
	case "PermissionRequest":
		s.Tool = ev.Tool
		s.Detail = "permission: " + ev.Tool
		r.setStatus(s, StatusNeedsInput, now)
	case "Notification":
		r.applyNotification(s, ev, now)
	case "Stop":
		s.Tool, s.Detail = "", ""
		r.setStatus(s, StatusDone, now)
	case "StopFailure":
		s.Tool = ""
		s.Detail = firstNonEmpty(ev.Error, "api error")
		r.setStatus(s, StatusNeedsInput, now)
	case "SessionEnd":
		r.applyEnd(s, ev, now)
	default:
		// CwdChanged was refreshed already; anything else still says the
		// session is alive.
	}
}

func (r *Registry) applyStart(s *Session, ev Event, now time.Time) {
	if ev.Source == "compact" {
		// Compaction is mid-session housekeeping, not a new session and
		// not a change of state.
		return
	}
	if s.Status == StatusGone {
		// Pinned and dead, now back: the same key lights up again.
		r.board.Pin(r.board.IndexOf(s.ID), true)
	}
	s.Tool, s.Detail = "", ""
	r.setStatus(s, StatusDone, now)
}

func (r *Registry) applyToolStart(s *Session, ev Event, now time.Time) {
	s.Tool = ev.Tool
	s.Detail = ev.Tool
	if ev.Tool == "AskUserQuestion" {
		// The question is on screen the moment the tool starts; the idle
		// nudge would only confirm it a minute later.
		s.Detail = "question"
		r.setStatus(s, StatusNeedsInput, now)
		return
	}
	r.setStatus(s, StatusWorking, now)
}

func (r *Registry) applyEnd(s *Session, ev Event, now time.Time) {
	if ev.Reason == "resume" || ev.Reason == "clear" {
		// The same process is about to start another session under a
		// new id; the record waits so that SessionStart renames it in
		// place and the key stays where it was.
		r.setStatus(s, StatusStale, now)
		return
	}
	r.dropLocked(s, now)
}

func (r *Registry) applyNotification(s *Session, ev Event, now time.Time) {
	switch ev.Notification {
	case "permission_prompt", "agent_needs_input", "elicitation_dialog", "elicitation_url_dialog":
		s.Detail = firstNonEmpty(shorten(ev.Message, 48), "needs you")
		r.setStatus(s, StatusNeedsInput, now)
	case "idle_prompt":
		// "Waiting for your input" fires a minute into ANY wait — after a
		// finished turn as much as after a question. A done session that
		// wants nothing stays done; only a session still mid-turn has a
		// question corgi did not otherwise see.
		if s.Status == StatusWorking {
			s.Detail = firstNonEmpty(shorten(ev.Message, 48), "waiting for input")
			r.setStatus(s, StatusNeedsInput, now)
		}
	case "auth_success", "agent_completed", "elicitation_complete", "elicitation_response":
		// Nothing a key needs to say.
	}
}

// adoptLocked creates a session for an event that arrived without a
// SessionStart — the daemon was down, or the session predates the hooks. A
// rescan placeholder for the same pid is upgraded in place, keeping its key.
func (r *Registry) adoptLocked(ev Event, now time.Time) *Session {
	if old := r.samePIDLocked(ev.ClaudePID); old != nil {
		// A rescan placeholder, or the same process under a new session id
		// (/clear, /resume): the key stays, the id changes.
		delete(r.sessions, old.ID)
		r.board.Rename(old.ID, ev.SessionID)
		old.ID = ev.SessionID
		old.StartedAt = now
		r.sessions[ev.SessionID] = old
		return old
	}
	s := &Session{ID: ev.SessionID, StartedAt: now, Status: StatusUnknown, StatusSince: now}
	r.sessions[s.ID] = s
	r.board.Place(s.ID)
	return s
}

// samePIDLocked finds the session already tracked for a process: the
// placeholder first, else whichever last spoke for that pid. One process
// hosts one session at a time, so a second id on it replaces the first.
func (r *Registry) samePIDLocked(pid int) *Session {
	if pid <= 0 {
		return nil
	}
	if old := r.sessions[PlaceholderID(pid)]; old != nil {
		return old
	}
	var latest *Session
	for _, s := range r.sessions {
		if s.ClaudePID == pid && (latest == nil || s.LastActivity.After(latest.LastActivity)) {
			latest = s
		}
	}
	return latest
}

// refresh copies the identity fields every event carries. A hook that could
// not determine a pid does not erase one an earlier hook found.
func (r *Registry) refresh(s *Session, ev Event) {
	if ev.Cwd != "" && ev.Cwd != s.Cwd {
		s.Cwd = ev.Cwd
		s.Label, s.Folder = r.resolve(ev.Cwd)
	}
	if s.Label == "" {
		s.Label, s.Folder = r.resolve(s.Cwd)
	}
	if s.Profile == "" || ev.ConfigDir != s.ConfigDir {
		s.ConfigDir = ev.ConfigDir
		s.Profile = r.profile(ev.ConfigDir)
	}
	if ev.ClaudePID > 0 {
		s.ClaudePID = ev.ClaudePID
	}
	if len(ev.Ancestors) > 0 {
		s.Ancestors = ev.Ancestors
		s.Names = ev.Names
	}
	if ev.TTY != 0 {
		s.TTY = ev.TTY
	}
	if ev.Window != "" {
		s.Window = ev.Window
	}
	if ev.TermProgram != "" {
		s.TermProgram = ev.TermProgram
	}
	if ev.TermSession != "" {
		s.TermSession = ev.TermSession
	}
	r.bind(s)
}

func (r *Registry) resolve(cwd string) (string, string) {
	if r.Resolve == nil {
		return DefaultResolve(cwd)
	}
	label, folder := r.Resolve(cwd)
	if label == "" {
		return DefaultResolve(cwd)
	}
	return label, folder
}

func (r *Registry) profile(configDir string) string {
	if r.ProfileFor == nil {
		return DefaultProfile(configDir)
	}
	if p := r.ProfileFor(configDir); p != "" {
		return p
	}
	return DefaultProfile(configDir)
}

func (r *Registry) setStatus(s *Session, st Status, now time.Time) {
	if s.Status == st {
		return
	}
	s.Status = st
	s.StatusSince = now
}

// dropLocked ends a session: off the board, or gone-but-pinned.
func (r *Registry) dropLocked(s *Session, now time.Time) {
	if r.board.Remove(s.ID) {
		s.Tool, s.Detail = "", ""
		r.setStatus(s, StatusGone, now)
		return
	}
	delete(r.sessions, s.ID)
}

// Reap drops every session whose process is gone. SessionEnd never fires for
// a force-quit window, so this is what actually frees a key. alive is
// injected: the daemon passes proc.Alive.
func (r *Registry) Reap(alive func(pid int) bool, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	changed := false
	for _, s := range r.sortedLocked() {
		if s.Status == StatusGone {
			continue
		}
		if s.ClaudePID <= 0 {
			// Nothing to probe (no process table on this platform, or a
			// chain the hook could not read): the session leaves once it
			// has been silent for as long as one would take to go stale,
			// instead of sitting on a key until the next restart.
			if now.Sub(s.LastActivity) >= StaleAfter {
				r.dropLocked(s, now)
				changed = true
			}
			continue
		}
		if alive(s.ClaudePID) {
			continue
		}
		r.dropLocked(s, now)
		changed = true
	}
	if changed {
		r.touch()
	}
	return changed
}

// Sweep marks sessions nothing has happened to for StaleAfter.
func (r *Registry) Sweep(now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	changed := false
	for _, s := range r.sessions {
		if s.Status != StatusWorking && s.Status != StatusDone && s.Status != StatusUnknown {
			continue
		}
		if now.Sub(s.LastActivity) < StaleAfter {
			continue
		}
		r.setStatus(s, StatusStale, now)
		changed = true
	}
	if changed {
		r.touch()
	}
	return changed
}

// SetWindows replaces the set of connected editor windows and re-runs the
// join for every session. Sessions keep their keys; only how focus reaches
// them changes.
func (r *Registry) SetWindows(windows []Window) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	next := make(map[string]Window, len(windows))
	for _, w := range windows {
		if w.ID != "" {
			next[w.ID] = w
		}
	}
	if sameWindows(r.windows, next) {
		return false
	}
	r.windows = next
	for _, s := range r.sessions {
		r.bind(s)
	}
	r.touch()
	return true
}

func sameWindows(a, b map[string]Window) bool {
	if len(a) != len(b) {
		return false
	}
	for id, w := range a {
		o, ok := b[id]
		if !ok || !o.UpdatedAt.Equal(w.UpdatedAt) || o.ExtHostPID != w.ExtHostPID || len(o.Terminals) != len(w.Terminals) {
			return false
		}
	}
	return true
}

// bind joins a session to a window and a tab, in the order that trusts the
// most exact evidence first:
//  1. The window id the extension injected into the terminal's environment,
//     then the tab whose shell is in the session's parent chain.
//  2. A window whose extension host is in the parent chain: the Claude Code
//     panel, which spawns claude from the extension host itself.
//  3. TERM_PROGRAM: an integrated terminal without the extension (matched to
//     a window by folder when one is connected), or a terminal emulator.
func (r *Registry) bind(s *Session) {
	// Folder is only ever a folder a connected window reported open: an
	// editor told to open any other folder opens a NEW window on it, or
	// reloads one, which is worse than just bringing the app forward. App
	// likewise comes from the window, or from the process names in the
	// session's parent chain — never a default, since Cursor, Windsurf and
	// VSCodium all claim TERM_PROGRAM=vscode.
	h := Host{Kind: HostUnknown, TermProgram: s.TermProgram, App: EditorFromChain(s.Names)}
	switch {
	case s.Window != "":
		r.bindInjectedWindow(s, &h)
	case r.bindPanel(s, &h):
	default:
		r.bindTermProgram(s, &h)
	}
	s.Host = h
}

// bindInjectedWindow is rule 1: the window id the extension put in the
// terminal's environment, then the tab whose shell is in the parent chain.
func (r *Registry) bindInjectedWindow(s *Session, h *Host) {
	h.Kind = HostVSCodeTerminal
	h.WindowID = s.Window
	w, ok := r.windows[s.Window]
	if !ok {
		return
	}
	h.Connected, h.App = true, w.App
	if len(w.Folders) > 0 {
		h.Folder = w.Folders[0]
	}
	for _, t := range w.Terminals {
		if containsInt(s.Ancestors, t.ShellPID) {
			h.ShellPID, h.Terminal = t.ShellPID, t.Name
			return
		}
	}
}

// bindPanel is rule 2: a window whose extension host is in the parent
// chain — the Claude Code panel, which spawns claude from the extension
// host itself. An editor in the chain with no window connected yet and no
// TERM_PROGRAM is most likely the panel too.
func (r *Registry) bindPanel(s *Session, h *Host) bool {
	for _, w := range r.sortedWindowsLocked() {
		if w.ExtHostPID > 0 && containsInt(s.Ancestors, w.ExtHostPID) {
			h.Kind, h.WindowID, h.App, h.Connected = HostVSCodePanel, w.ID, w.App, true
			if len(w.Folders) > 0 {
				h.Folder = w.Folders[0]
			}
			return true
		}
	}
	if h.App != "" && s.TermProgram == "" {
		h.Kind = HostVSCodePanel
		return true
	}
	return false
}

// bindTermProgram is rule 3: an integrated terminal without the extension
// (matched to a window by folder when one is connected), or an emulator.
func (r *Registry) bindTermProgram(s *Session, h *Host) {
	switch strings.ToLower(s.TermProgram) {
	case "vscode":
		h.Kind = HostVSCodeTerminal
		if w, ok := r.windowForDir(s.Cwd); ok {
			h.WindowID, h.App, h.Connected = w.ID, w.App, true
			h.Folder = w.Folders[0]
		}
	case "iterm.app":
		h.Kind = HostITerm
	case "apple_terminal":
		h.Kind = HostTerminalApp
	}
}

// windowForDir finds the connected window whose folder contains dir. The
// deepest folder wins, so a window on the repo beats one on the whole
// projects directory.
func (r *Registry) windowForDir(dir string) (Window, bool) {
	var best Window
	bestLen := -1
	for _, w := range r.sortedWindowsLocked() {
		for _, f := range w.Folders {
			if f == "" || !Within(dir, f) || len(f) <= bestLen {
				continue
			}
			best, bestLen = Window{ID: w.ID, App: w.App, ExtHostPID: w.ExtHostPID, Folders: []string{f}}, len(f)
		}
	}
	return best, bestLen >= 0
}

func containsInt(list []int, n int) bool {
	if n <= 0 {
		return false
	}
	for _, x := range list {
		if x == n {
			return true
		}
	}
	return false
}

// Pin toggles a key's reservation. Unpinning a gone session drops it.
func (r *Registry) Pin(index int, on bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.board.Pin(index, on) {
		return false
	}
	if !on {
		if s := r.sessions[r.board.Slots[index]]; s != nil && s.Status == StatusGone {
			r.board.Free(index)
			delete(r.sessions, s.ID)
		}
	}
	r.touch()
	return true
}

// Page rotates the unpinned keys through the overflow.
func (r *Registry) Page(direction int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.board.Page(direction) {
		return false
	}
	r.touch()
	return true
}

// Adopt registers Claude processes no hook has reported — sessions started
// while the daemon was down. Each gets a placeholder id, an unknown status
// and whatever cwd the platform will give up; the first hook from it fills
// in the rest. Processes already tracked are left alone.
func (r *Registry) Adopt(procs []proc.Process, cwd func(pid int) string, now time.Time) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	known := map[int]bool{}
	for _, s := range r.sessions {
		known[s.ClaudePID] = true
	}
	added := 0
	for _, p := range procs {
		if known[p.PID] || !proc.LooksLikeClaude(p) || notASession(p.Args) {
			continue
		}
		known[p.PID] = true
		chain := proc.Ancestors(p.PID)
		s := &Session{
			ID: PlaceholderID(p.PID), ClaudePID: p.PID, Ancestors: proc.PIDs(chain), Names: proc.Names(chain),
			StartedAt: now, LastActivity: now, Status: StatusUnknown, StatusSince: now,
		}
		if cwd != nil {
			s.Cwd = cwd(p.PID)
		}
		s.Label, s.Folder = r.resolve(s.Cwd)
		s.Profile = r.profile("")
		r.bind(s)
		r.sessions[s.ID] = s
		r.board.Place(s.ID)
		added++
	}
	if added > 0 {
		r.touch()
	}
	return added
}

// notASession recognises claude processes that are not an interactive
// session: the supervised remote-control server, an MCP server, a one-shot
// --print run. Adopting one would park it on a key forever.
func notASession(args string) bool {
	for _, marker := range []string{"remote-control", " mcp ", " mcp serve", "--print", " -p ", " -p\n"} {
		if strings.Contains(args+"\n", marker) {
			return true
		}
	}
	return false
}

// FocusTarget is what the daemon needs to bring a session to the front.
type FocusTarget struct {
	SessionID string
	Kind      HostKind
	App       string
	Folder    string
	WindowID  string
	ShellPID  int
	// Panel asks the window to reveal the Claude Code panel rather than a
	// terminal tab.
	Panel bool
	// TTY is the controlling terminal device for an emulator session.
	TTY uint64
	// Connected says a reveal request will be read by a live extension.
	Connected bool
}

// ErrNoSession is returned for an id or label nothing matches.
var ErrNoSession = errors.New("no such session")

// Focus resolves a session reference into a focus target.
func (r *Registry) Focus(ref string) (FocusTarget, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, err := r.lookupLocked(ref)
	if err != nil {
		return FocusTarget{}, err
	}
	if s.Status == StatusGone {
		return FocusTarget{}, fmt.Errorf("%s has exited", s.Label)
	}
	h := s.Host
	return FocusTarget{
		SessionID: s.ID, Kind: h.Kind, App: h.App, Folder: h.Folder, WindowID: h.WindowID,
		ShellPID: h.ShellPID, Panel: h.Kind == HostVSCodePanel, Connected: h.Connected, TTY: s.TTY,
	}, nil
}

// RecordFocus stores the outcome of a focus attempt on the session, so the
// key can flash a failure on the next push.
func (r *Registry) RecordFocus(id string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.sessions[id]
	if s == nil {
		return
	}
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	s.FocusAt = time.Now()
	if s.FocusError == msg && msg == "" {
		return
	}
	s.FocusError = msg
	r.touch()
}

// Lookup finds a session by id, id prefix, label, or slot index.
func (r *Registry) Lookup(ref string) (Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, err := r.lookupLocked(ref)
	if err != nil {
		return Session{}, err
	}
	return *s, nil
}

func (r *Registry) lookupLocked(ref string) (*Session, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, ErrNoSession
	}
	if s := r.sessions[ref]; s != nil {
		return s, nil
	}
	// A key number comes before an id prefix: ids are hex, so "2" would
	// otherwise match a sixteenth of them and focus the wrong window.
	if n, ok := slotRef(ref); ok {
		if n >= 0 && n < r.board.Size && r.board.Slots[n] != "" {
			return r.sessions[r.board.Slots[n]], nil
		}
		return nil, fmt.Errorf("key %s is empty", ref)
	}
	var byPrefix, byLabel []*Session
	for _, s := range r.sortedLocked() {
		if strings.HasPrefix(s.ID, ref) {
			byPrefix = append(byPrefix, s)
		}
		if strings.EqualFold(s.Label, ref) || strings.EqualFold(r.displayLocked(s), ref) {
			byLabel = append(byLabel, s)
		}
	}
	switch {
	case len(byPrefix) == 1:
		return byPrefix[0], nil
	case len(byPrefix) > 1:
		return nil, fmt.Errorf("%q matches %d sessions — use more of the id", ref, len(byPrefix))
	case len(byLabel) == 1:
		return byLabel[0], nil
	case len(byLabel) > 1:
		return nil, fmt.Errorf("%d sessions are called %q — use the id", len(byLabel), ref)
	}
	return nil, ErrNoSession
}

// slotRef reads a key number: "3" or "#3", 1-based as printed on the board.
func slotRef(ref string) (int, bool) {
	ref = strings.TrimPrefix(ref, "#")
	n := 0
	for _, c := range ref {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n - 1, ref != ""
}

// Snapshot is the board as of now.
func (r *Registry) Snapshot(now time.Time) State {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.snapshotLocked(now)
}

func (r *Registry) snapshotLocked(now time.Time) State {
	st := State{UpdatedAt: r.updatedAt, Size: r.board.Size, Overflow: r.board.Hidden()}
	if st.UpdatedAt.IsZero() {
		st.UpdatedAt = now
	}
	pager := r.board.PagerIndex()
	for i := 0; i < r.board.Size; i++ {
		sl := Slot{Index: i, Pinned: r.board.Pinned[i]}
		if i == pager {
			sl.Pager, sl.Overflow = true, r.board.Hidden()
			st.Slots = append(st.Slots, sl)
			continue
		}
		s := r.sessions[r.board.Slots[i]]
		if s == nil {
			sl.Empty = true
			st.Slots = append(st.Slots, sl)
			continue
		}
		sl.SessionID, sl.Label, sl.Profile, sl.Status = s.ID, r.displayLocked(s), s.Profile, s.Status
		sl.Detail, sl.Host, sl.FocusError, sl.FocusAt = s.Detail, s.Host.Kind, s.FocusError, s.FocusAt
		if !s.StatusSince.IsZero() && now.After(s.StatusSince) {
			sl.ElapsedS = int(now.Sub(s.StatusSince).Seconds())
		}
		st.Slots = append(st.Slots, sl)
	}
	for _, s := range r.sortedLocked() {
		c := *s
		c.Display = r.displayLocked(s)
		st.Sessions = append(st.Sessions, c)
		switch s.Status {
		case StatusNeedsInput:
			st.NeedsInput++
		case StatusWorking:
			st.Working++
		}
	}
	st.Windows = r.sortedWindowsLocked()
	return st
}

// displayLocked makes a label unique among live sessions: two sessions in
// the same repo get their terminal name, or a piece of the id, appended.
func (r *Registry) displayLocked(s *Session) string {
	twins := 0
	for _, o := range r.sessions {
		if o.Label == s.Label {
			twins++
		}
	}
	if twins <= 1 {
		return s.Label
	}
	if s.Host.Terminal != "" {
		return s.Label + "·" + s.Host.Terminal
	}
	id := strings.TrimPrefix(s.ID, "pid:")
	if len(id) > 4 {
		id = id[:4]
	}
	return s.Label + "·" + id
}

func (r *Registry) sortedLocked() []*Session {
	out := make([]*Session, 0, len(r.sessions))
	for _, s := range r.sessions {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].StartedAt.Before(out[j].StartedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (r *Registry) sortedWindowsLocked() []Window {
	out := make([]Window, 0, len(r.windows))
	for _, w := range r.windows {
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (r *Registry) touch() {
	r.updatedAt = time.Now()
	r.dirty = true
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func shorten(s string, max int) string {
	s = strings.TrimSpace(strings.SplitN(s, "\n", 2)[0])
	if runes := []rune(s); len(runes) > max {
		return strings.TrimSpace(string(runes[:max-1])) + "…"
	}
	return s
}
