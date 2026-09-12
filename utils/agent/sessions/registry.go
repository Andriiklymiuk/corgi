package sessions

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"andriiklymiuk/corgi/utils/agent/proc"
	"andriiklymiuk/corgi/utils/agent/usage"
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
	// lastFocus is the window the last successful focus landed in: where
	// a new session opens when nobody says otherwise.
	lastFocus struct {
		WindowID  string
		SessionID string
		At        time.Time
	}
	notice   string
	noticeAt time.Time
	accounts []Account
	// AutoContinue is copied onto every snapshot; the daemon sets it.
	AutoContinue bool
	// OnTransition, when set, is told about every status change after it
	// happened. The daemon turns some into notifications and metrics.
	OnTransition func(s Session, from, to Status, now time.Time)
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
	NeedsInput int    `json:"needsInput"`
	Working    int    `json:"working"`
	Slots      []Slot `json:"slots"`
	// LastFocusWindow is where the last successful focus went, and where
	// `corgi agent new` opens a session by default.
	LastFocusWindow string `json:"lastFocusWindow,omitempty"`
	// FrontWindow is the connected window most recently in front, and
	// FrontSession the session the user is looking at in it: the one in its
	// active terminal tab, else its panel session, else the one that moved
	// last. What a talk key dictates into without a key being pressed first.
	FrontWindow  string `json:"frontWindow,omitempty"`
	FrontSession string `json:"frontSession,omitempty"`
	// Notice is the last board-level failure — a `new` with no window to
	// open in, say — with its time, for a key to flash once.
	// AutoContinue says the daemon types "continue" into limited sessions
	// itself, so an editor with the same feature can stand down.
	AutoContinue bool      `json:"autoContinue,omitempty"`
	Notice       string    `json:"notice,omitempty"`
	NoticeAt     time.Time `json:"noticeAt,omitempty"`
	Sessions     []Session `json:"sessions"`
	Windows      []Window  `json:"windows,omitempty"`
	// Accounts is every account the sessions run under, with its limits.
	Accounts []Account `json:"accounts,omitempty"`
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
	// Context is the context-window fill in percent, 0 when unknown.
	Context int `json:"context,omitempty"`
	// Pending names the tool of a permission prompt the key could answer.
	Pending string `json:"pending,omitempty"`
	Note    string `json:"note,omitempty"`
	Stuck   bool   `json:"stuck,omitempty"`
	Branch  string `json:"branch,omitempty"`
	Summary string `json:"summary,omitempty"`
	PR      string `json:"pr,omitempty"`
	Ticket  string `json:"ticket,omitempty"`
	// TurnS is how long the current turn has been running, 0 unless working.
	TurnS int `json:"turnS,omitempty"`
	// Limit is quota or overload on a limited key; ResumeAt when the daemon
	// will continue it on its own, so the key can say "continues 14:02".
	Limit    LimitKind `json:"limit,omitempty"`
	ResumeAt time.Time `json:"resumeAt,omitzero"`
	// Drift is the first reason the daemon thinks a person should look.
	Drift string `json:"drift,omitempty"`
	// Changes is the branch in one line — "4 files · 120 lines"; Tests the
	// last test run — "tests ✓" or "tests ✗ go test"; Overlap the first
	// other session on the same files — "api·2 on registry.go".
	Changes string `json:"changes,omitempty"`
	Tests   string `json:"tests,omitempty"`
	Overlap string `json:"overlap,omitempty"`
	// Spend is the running total in one word — "52M" — and OverCap says
	// it passed the budget it was given.
	Spend   string `json:"spend,omitempty"`
	OverCap bool   `json:"overCap,omitempty"`
}

// SpendLine is a token count as the board says it: "52M", "980k", "412".
func SpendLine(sp *Spend) string {
	if sp == nil || sp.Tokens == 0 {
		return ""
	}
	return Tokens(sp.Tokens)
}

// Tokens is a count in one word.
func Tokens(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1e9)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%dk", n/1_000)
	}
	return fmt.Sprintf("%d", n)
}

// ParseTokens reads a budget the way a person types it: 50M, 800k, 2B, or
// a plain number.
func ParseTokens(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, fmt.Errorf("a token count is required, like 50M")
	}
	mult := int64(1)
	switch s[len(s)-1] {
	case 'k':
		mult, s = 1_000, s[:len(s)-1]
	case 'm':
		mult, s = 1_000_000, s[:len(s)-1]
	case 'b':
		mult, s = 1_000_000_000, s[:len(s)-1]
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("not a token count: %q (try 50M, 800k)", s)
	}
	return int64(n * float64(mult)), nil
}

// ChangesLine is the board's one line for a branch: files and lines that
// are somebody's work.
func ChangesLine(c *Changes) string {
	if c == nil || (c.Files == 0 && c.Lines == 0) {
		return ""
	}
	return fmt.Sprintf("%d file%s · %d line%s", c.Files, plural(c.Files), c.Lines, plural(c.Lines))
}

// TestsLine says how the last test run went, in three characters.
func TestsLine(t *TestRun) string {
	if t == nil {
		return ""
	}
	if t.OK {
		return "tests ✓"
	}
	return "tests ✗ " + t.Cmd
}

// OverlapLine names the first session on the same files, or the same
// checkout.
func OverlapLine(o []Overlap) string {
	if len(o) == 0 {
		return ""
	}
	first := o[0]
	if first.SameCheckout {
		return "same checkout as " + first.Session
	}
	files := first.Files
	if len(files) > 2 {
		files = append(files[:2], "…")
	}
	return first.Session + " on " + strings.Join(files, ", ")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
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
	Label, Profile, Detail, Tool, Branch, Summary string
	Status                                        Status
	Host                                          Host
}

func (s *Session) visible() visible {
	return visible{Label: s.Label, Profile: s.Profile, Detail: s.Detail, Tool: s.Tool, Status: s.Status, Host: s.Host, Branch: s.Branch, Summary: s.Summary}
}

// transition is the status model: what each hook event means for a session.
func (r *Registry) transition(s *Session, ev Event, now time.Time) {
	switch ev.Name {
	case "SessionStart":
		r.applyStart(s, ev, now)
	case "UserPromptSubmit":
		s.Tool, s.Detail, s.Pending = "", "", nil
		s.TurnStartedAt = now
		r.setStatus(s, StatusWorking, now)
	case "PreToolUse":
		r.applyToolStart(s, ev, now)
	case "PostToolUse", "PostToolUseFailure":
		// A tool that finished means whatever prompt preceded it was
		// answered.
		s.Tool, s.Pending = "", nil
		if strings.HasPrefix(s.Detail, ev.Tool) || s.Status == StatusNeedsInput {
			s.Detail = ""
		}
		subject := ev.Tool + " " + ev.Subject
		if ev.Tool == "Bash" && IsTestCommand(ev.Subject) {
			s.Tests = &TestRun{OK: ev.Name == "PostToolUse", At: now, Cmd: ev.Subject}
		}
		if ev.Name == "PostToolUseFailure" && subject == s.failSubject {
			s.FailStreak++
		} else if ev.Name == "PostToolUseFailure" {
			s.FailStreak, s.failSubject = 1, subject
		} else if subject == s.failSubject {
			// The same thing succeeded: whatever was looping is over.
			s.FailStreak, s.failSubject = 0, ""
		}
		r.setStatus(s, StatusWorking, now)
	case "PermissionRequest":
		s.Tool = ev.Tool
		s.Detail = "permission: " + toolLine(ev.Tool, ev.Subject)
		s.Pending = &Pending{Tool: ev.Tool, Subject: ev.Subject, At: now}
		r.setStatus(s, StatusNeedsInput, now)
	case "Notification":
		r.applyNotification(s, ev, now)
	case "Stop":
		s.Tool, s.Detail, s.Pending = "", "", nil
		s.FailStreak, s.failSubject = 0, ""
		r.setStatus(s, StatusDone, now)
	case "StopFailure":
		s.Tool, s.Pending = "", nil
		if kind, reset, limited := ClassifyLimit(ev.Error, ev.Message); limited {
			r.applyLimit(s, kind, reset, now)
			return
		}
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
	s.Tool, s.Detail, s.Pending = "", "", nil
	r.setStatus(s, StatusDone, now)
}

// toolLine is the detail a key shows for a tool: "Edit registry.go", "Bash
// go test", or just the tool when the hook had nothing safe to add.
func toolLine(tool, subject string) string {
	if subject == "" {
		return tool
	}
	return tool + " " + subject
}

func (r *Registry) applyToolStart(s *Session, ev Event, now time.Time) {
	s.Tool = ev.Tool
	s.Detail = toolLine(ev.Tool, ev.Subject)
	s.Pending = nil
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

// applyLimit: the account is out of quota. Not a question, so not
// needs_input; the key says when to come back instead.
func (r *Registry) applyLimit(s *Session, kind LimitKind, reset string, now time.Time) {
	s.Detail = "limit reached"
	if kind == LimitOverload {
		s.Detail = "API overloaded"
	}
	if reset != "" {
		s.Detail = "resets " + reset
	}
	s.Limit, s.ResumeAt = kind, time.Time{}
	r.setStatus(s, StatusLimited, now)
}

func (r *Registry) applyNotification(s *Session, ev Event, now time.Time) {
	if kind, reset, limited := ClassifyLimit("", ev.Message); limited {
		r.applyLimit(s, kind, reset, now)
		return
	}
	switch ev.Notification {
	case "permission_prompt", "agent_needs_input", "elicitation_dialog", "elicitation_url_dialog":
		s.Detail = firstNonEmpty(shorten(ev.Message, 48), "needs you")
		if ev.Notification == "permission_prompt" && s.Pending == nil && s.Tool != "" {
			s.Pending = &Pending{Tool: s.Tool, At: now}
		}
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
	case "quota_auto_resume_fired":
		// The limit lifted and Claude picked the turn back up.
		s.Tool, s.Detail, s.Pending = "", "", nil
		r.setStatus(s, StatusWorking, now)
	case "auth_success", "agent_completed", "elicitation_complete", "elicitation_response", "quota_auto_resume_stale", "quota_auto_resume_disabled":
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
	if s.Profile == "" || (ev.ConfigDir != "" && ev.ConfigDir != s.ConfigDir) {
		// An event that carries no config dir (a hook run without the env,
		// or a synthetic one) must not demote a session to "default".
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
	if ev.Context != nil {
		c := *ev.Context
		s.Context = &c
	}
	if ev.Title != "" {
		s.Title = ev.Title
	}
	if ev.Branch != "" {
		s.Branch = ev.Branch
	}
	if ev.Ticket != "" {
		s.Ticket, s.TicketKey = ev.Ticket, ev.TicketKey
	}
	if ev.Summary != "" {
		s.Summary = ev.Summary
	}
	if ev.PR != "" {
		s.PR = ev.PR
	}
	// Any event at all means the process is alive and talking.
	s.Stuck = false
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
	from := s.Status
	s.Status = st
	s.StatusSince = now
	if st != StatusLimited {
		s.Limit, s.ResumeAt = "", time.Time{}
	}
	// A turn that finished or asked something is real progress; the count
	// of continues is for one limit episode, not the session's life.
	if st == StatusDone || st == StatusNeedsInput {
		s.Resumes = 0
	}
	if r.OnTransition != nil {
		r.OnTransition(*s, from, st, now)
	}
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
		quiet := now.Sub(s.LastActivity)
		if s.Status == StatusWorking && !s.Stuck && quiet >= StuckAfter && quiet < StaleAfter {
			s.Stuck = true
			changed = true
		}
		if quiet < StaleAfter {
			continue
		}
		s.Stuck = false
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
	r.dropClosedPanelsLocked(time.Now())
	r.touch()
	return true
}

// dropClosedPanelsLocked frees the keys of panel sessions whose chat tab is
// gone. A window that counts its Claude tabs can have more finished panel
// sessions bound to it than tabs; the quietest surplus ones are dropped. A
// session still working or waiting is never touched, and a dropped one
// comes back with its next hook event, so a miscount (the sidebar view is
// not a tab) costs a key for a moment, not a session.
func (r *Registry) dropClosedPanelsLocked(now time.Time) {
	for _, w := range r.windows {
		if w.ClaudeTabs == nil {
			continue
		}
		var idle []*Session
		for _, s := range r.sortedLocked() {
			if s.Host.Kind != HostVSCodePanel || s.Host.WindowID != w.ID {
				continue
			}
			switch s.Status {
			case StatusDone, StatusStale, StatusUnknown, StatusLimited:
				idle = append(idle, s)
			}
		}
		surplus := len(idle) - *w.ClaudeTabs
		if surplus <= 0 {
			continue
		}
		sort.Slice(idle, func(i, j int) bool { return idle[i].LastActivity.Before(idle[j].LastActivity) })
		for _, s := range idle[:surplus] {
			r.dropLocked(s, now)
		}
	}
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
		for i := range o.Terminals {
			if o.Terminals[i] != w.Terminals[i] {
				return false
			}
		}
		if !o.FocusedAt.Equal(w.FocusedAt) || o.ActiveShellPID != w.ActiveShellPID || o.PanelActive != w.PanelActive {
			return false
		}
		if (o.ClaudeTabs == nil) != (w.ClaudeTabs == nil) || (o.ClaudeTabs != nil && *o.ClaudeTabs != *w.ClaudeTabs) {
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

// Dismiss takes a session off the board — and off its pin — until its next
// hook event brings it back. For a chat that was closed while Claude Code
// kept its process, or any finished session hogging a key. A session still
// working or waiting on a person is refused: nothing should hide those.
func (r *Registry) Dismiss(ref string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, err := r.lookupLocked(ref)
	if err != nil {
		return err
	}
	if s.Status == StatusWorking || s.Status == StatusNeedsInput {
		return fmt.Errorf("%s is %s — not dismissing a live session", s.Label, s.Status)
	}
	if i := r.board.IndexOf(s.ID); i >= 0 {
		r.board.Pin(i, false)
	}
	s.Note = ""
	r.dropLocked(s, now)
	r.touch()
	return nil
}

// SetNote puts the owner's line on a session, or clears it with "".
func (r *Registry) SetNote(ref, note string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, err := r.lookupLocked(ref)
	if err != nil {
		return err
	}
	note = strings.TrimSpace(note)
	if len(note) > 80 {
		note = note[:80]
	}
	if s.Note == note {
		return nil
	}
	s.Note = note
	r.touch()
	return nil
}

// SetDrift records what the daemon concluded about a session; true when
// the reasons changed, and whether drift just began (worth one notice).
func (r *Registry) SetDrift(id string, reasons []string) (changed, began bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	if !ok {
		return false, false
	}
	if strings.Join(s.Drift, "|") == strings.Join(reasons, "|") {
		return false, false
	}
	began = len(s.Drift) == 0 && len(reasons) > 0
	s.Drift = reasons
	r.touch()
	return true, began
}

// SetChanges records what the sweep measured on a session's branch and who
// else is on the same files. Reported as changed only when the numbers or
// the names moved, so a quiet board is not rewritten every minute.
func (r *Registry) SetChanges(id string, c *Changes, overlap []Overlap) (changed, crossed bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	if !ok {
		return false, false
	}
	same := (s.Changes == nil) == (c == nil) && overlapKey(s.Overlap) == overlapKey(overlap)
	if same && c != nil {
		same = s.Changes.Files == c.Files && s.Changes.Lines == c.Lines && strings.Join(s.Changes.Touched, "|") == strings.Join(c.Touched, "|")
	}
	if same {
		return false, false
	}
	// crossed: this session's work just started meeting somebody else's on
	// a file — said once, the moment it happens.
	crossed = len(overlap) > 0 && !overlap[0].SameCheckout && len(s.Overlap) == 0
	s.Changes, s.Overlap = c, overlap
	r.touch()
	return true, crossed
}

// SetSpend records what a session has cost and whether that passed its
// budget (its own cap, else fallback). crossed is true the moment it does,
// so the daemon rings once.
func (r *Registry) SetSpend(id string, sp Spend, fallback int64) (changed, crossed bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	if !ok {
		return false, false
	}
	limit := s.Cap
	if limit == 0 {
		limit = fallback
	}
	over := limit > 0 && sp.Tokens > limit
	if s.Spend != nil && s.Spend.Tokens == sp.Tokens && s.OverCap == over {
		return false, false
	}
	crossed = over && !s.OverCap
	sp2 := sp
	s.Spend, s.OverCap = &sp2, over
	r.touch()
	return true, crossed
}

// SetCap gives one session its own budget; zero takes it away, and the
// daemon's default applies again on the next sweep.
func (r *Registry) SetCap(ref string, tokens int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, err := r.lookupLocked(ref)
	if err != nil {
		return err
	}
	if s.Cap == tokens {
		return nil
	}
	s.Cap = tokens
	if tokens > 0 && s.Spend != nil {
		s.OverCap = s.Spend.Tokens > tokens
	}
	r.touch()
	return nil
}

func overlapKey(o []Overlap) string {
	parts := make([]string, 0, len(o))
	for _, x := range o {
		parts = append(parts, x.ID+":"+strings.Join(x.Files, ",")+":"+strconv.FormatBool(x.SameCheckout))
	}
	return strings.Join(parts, "|")
}

// PlanResume records when the daemon will continue a limited session; a
// zero time clears the plan. Nothing else changes, so no transition fires.
func (r *Registry) PlanResume(id string, at time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	if !ok || s.Status != StatusLimited || s.ResumeAt.Equal(at) {
		return false
	}
	s.ResumeAt = at
	return true
}

// MarkResumed counts one continue typed into a limited session and clears
// the plan; the next hook event decides whether it worked.
func (r *Registry) MarkResumed(id string) (Session, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	if !ok {
		return Session{}, false
	}
	s.Resumes++
	s.ResumeAt = time.Time{}
	return *s, true
}

// SetAccounts replaces the accounts block. Sessions per account are counted
// here so callers pass only what they read.
func (r *Registry) SetAccounts(accounts []Account) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range accounts {
		accounts[i].Sessions = 0
		for _, s := range r.sessions {
			if s.ConfigDir == accounts[i].ConfigDir && s.Status != StatusGone {
				accounts[i].Sessions++
			}
		}
	}
	if sameAccounts(r.accounts, accounts) {
		return false
	}
	r.accounts = accounts
	r.touch()
	return true
}

func sameAccounts(a, b []Account) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		if x.Profile != y.Profile || x.ConfigDir != y.ConfigDir || x.Sessions != y.Sessions {
			return false
		}
		if (x.Limits == nil) != (y.Limits == nil) || (x.Limits != nil && *x.Limits != *y.Limits) {
			return false
		}
		if (x.Forecast == nil) != (y.Forecast == nil) {
			return false
		}
		if x.Forecast != nil && !sameWindowForecast(x.Forecast.FiveHour, y.Forecast.FiveHour) {
			return false
		}
		if x.Forecast != nil && !sameWindowForecast(x.Forecast.SevenDay, y.Forecast.SevenDay) {
			return false
		}
	}
	return true
}

func sameWindowForecast(a, b *usage.WindowForecast) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	return a == nil || (a.PercentPerHour == b.PercentPerHour && a.Safe == b.Safe && a.ExhaustAt.Equal(b.ExhaustAt))
}

// Sessions is a copy of every tracked session, board order.
func (r *Registry) Sessions() []Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Session
	for _, s := range r.sortedLocked() {
		c := *s
		c.Display = r.displayLocked(s)
		out = append(out, c)
	}
	return out
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
	// New asks the window for a fresh terminal running claude, in Folder.
	New bool
	// Connected says a reveal request will be read by a live extension.
	Connected bool
	// Title is the chat tab's name, for a window with several panels open.
	Title string
	// Label is the session's display name, for messages.
	Label string
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
		Title: s.Title, Label: r.displayLocked(s),
	}, nil
}

// PendingAnswer resolves a permission answer into the keys that give it,
// refusing what should not be answered blind. answer is allow, always or
// deny.
func (r *Registry) PendingAnswer(ref, answer string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, err := r.lookupLocked(ref)
	if err != nil {
		return "", err
	}
	if s.Status != StatusNeedsInput || s.Pending == nil {
		return "", fmt.Errorf("%s has no permission prompt to answer", r.displayLocked(s))
	}
	switch answer {
	case "deny":
		return "\x1b", nil
	case "allow", "always":
		if s.Pending.Risky() {
			return "", fmt.Errorf("%s asks to run %q — look at it before allowing", r.displayLocked(s), s.Pending.Subject)
		}
		if answer == "always" {
			return "2\r", nil
		}
		return "\r", nil
	}
	return "", fmt.Errorf("answer is allow, always or deny, not %q", answer)
}

// InterruptKeys is what stops a session's turn — Escape — and an error
// when there is no turn to stop: Claude Code takes Escape at rest as
// nothing, but a key pressed into a session for no reason is a surprise.
func (r *Registry) InterruptKeys(ref string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, err := r.lookupLocked(ref)
	if err != nil {
		return "", err
	}
	if s.Status != StatusWorking {
		return "", fmt.Errorf("%s is not working on anything to interrupt", r.displayLocked(s))
	}
	return "\x1b", nil
}

// Interrupted marks a working session stopped by Escape: done, with the
// row saying so. Claude Code fires no hook for it, so the board would
// otherwise say working until the next message. A session that had already
// moved on is left as it is, and its next event corrects the row either way.
func (r *Registry) Interrupted(ref string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, err := r.lookupLocked(ref)
	if err != nil || s.Status != StatusWorking {
		return false
	}
	s.Tool, s.Pending = "", nil
	s.Detail = "interrupted"
	r.setStatus(s, StatusDone, now)
	r.touch()
	return true
}

// Risky says whether the prompt must not be approved unseen.
func (p *Pending) Risky() bool {
	return p != nil && p.Tool == "Bash" && riskyCommand.MatchString(p.Subject)
}

// riskyCommand is what an Allow button must not approve unseen. The subject
// is the program and two words, so this is coarse on purpose.
var riskyCommand = regexp.MustCompile(`(?i)(^|\s)(rm|sudo|mkfs|dd|shutdown|reboot|kill|pkill|killall|chmod|chown|launchctl|diskutil)(\s|$)|--force|--hard|--no-verify|\bdrop\b|\btruncate\b|\bpurge\b`)

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
	if err == nil && s.Host.WindowID != "" {
		r.lastFocus.WindowID, r.lastFocus.SessionID, r.lastFocus.At = s.Host.WindowID, s.ID, s.FocusAt
	}
	if s.FocusError == msg && msg == "" {
		return
	}
	s.FocusError = msg
	r.touch()
}

// SetNotice records a board-level failure for the next publish.
func (r *Registry) SetNotice(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	r.notice, r.noticeAt = msg, time.Now()
	r.touch()
}

// ErrNoWindow is returned when nothing can open a new session.
var ErrNoWindow = errors.New("no editor window connected — open a folder in VS Code with the corgi extension installed")

// NewSessionTarget picks the window a fresh session opens in: the one
// named, else the one in front, else the most recently updated. The window
// must be connected: only its extension can open a terminal in it.
func (r *Registry) NewSessionTarget(windowID string) (FocusTarget, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var w Window
	var ok bool
	if windowID != "" {
		if w, ok = r.windows[windowID]; !ok {
			return FocusTarget{}, fmt.Errorf("window %s is not connected", windowID)
		}
	} else {
		w, ok = r.frontWindowLocked()
	}
	if !ok {
		for _, cand := range r.sortedWindowsLocked() {
			if !ok || cand.UpdatedAt.After(w.UpdatedAt) {
				w, ok = cand, true
			}
		}
	}
	if !ok {
		return FocusTarget{}, ErrNoWindow
	}
	t := FocusTarget{Kind: HostVSCodeTerminal, App: w.App, WindowID: w.ID, Connected: true, New: true}
	if len(w.Folders) > 0 {
		t.Folder = w.Folders[0]
	}
	return t, nil
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
	st := State{UpdatedAt: r.updatedAt, Size: r.board.Size, Overflow: r.board.Hidden(),
		LastFocusWindow: r.lastFocus.WindowID, Notice: r.notice, NoticeAt: r.noticeAt, AutoContinue: r.AutoContinue}
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
		sl.Note, sl.Stuck = s.Note, s.Stuck
		sl.Limit, sl.ResumeAt = s.Limit, s.ResumeAt
		if len(s.Drift) > 0 {
			sl.Drift = s.Drift[0]
		}
		sl.Changes, sl.Tests, sl.Overlap = ChangesLine(s.Changes), TestsLine(s.Tests), OverlapLine(s.Overlap)
		sl.Spend, sl.OverCap = SpendLine(s.Spend), s.OverCap
		sl.Branch, sl.Summary, sl.PR, sl.Ticket = s.Branch, s.Summary, s.PR, s.Ticket
		if s.Status == StatusWorking && !s.TurnStartedAt.IsZero() && now.After(s.TurnStartedAt) {
			sl.TurnS = int(now.Sub(s.TurnStartedAt).Seconds())
		}
		if s.Context != nil {
			sl.Context = s.Context.Percent
		}
		if s.Pending != nil {
			sl.Pending = s.Pending.Tool
		}
		if !s.StatusSince.IsZero() && now.After(s.StatusSince) {
			sl.ElapsedS = int(now.Sub(s.StatusSince).Seconds())
		}
		st.Slots = append(st.Slots, sl)
	}
	for _, s := range r.groupedLocked() {
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
	st.Accounts = append([]Account(nil), r.accounts...)
	if w, ok := r.frontWindowLocked(); ok {
		st.FrontWindow = w.ID
		if s := r.frontSessionLocked(w); s != nil {
			st.FrontSession = s.ID
		}
	}
	return st
}

// frontWindowLocked is the connected window most recently in front: the
// newest focus its extension reported, or where corgi's own last focus
// landed when that is newer. A lone window with no word either way is the
// front one too.
func (r *Registry) frontWindowLocked() (Window, bool) {
	var best Window
	found := false
	for _, w := range r.sortedWindowsLocked() {
		if !w.FocusedAt.IsZero() && (!found || w.FocusedAt.After(best.FocusedAt)) {
			best, found = w, true
		}
	}
	if w, ok := r.windows[r.lastFocus.WindowID]; ok && (!found || r.lastFocus.At.After(best.FocusedAt)) {
		return w, true
	}
	if !found && len(r.windows) == 1 {
		for _, w := range r.windows {
			return w, true
		}
	}
	return best, found
}

// frontSessionLocked is the session the user sees in a window: the one
// corgi just focused there, until the window reports something newer; else
// its panel session while the Claude Code panel is the active tab; else the
// one in its active terminal tab, else its panel session, else the one that
// moved last. Nil when no live session is bound to the window.
func (r *Registry) frontSessionLocked(w Window) *Session {
	if r.lastFocus.WindowID == w.ID && r.lastFocus.At.After(w.FocusedAt) {
		if s := r.sessions[r.lastFocus.SessionID]; s != nil && s.Status != StatusGone {
			return s
		}
	}
	var tab, panel, latest *Session
	for _, s := range r.sortedLocked() {
		if s.Host.WindowID != w.ID || s.Status == StatusGone {
			continue
		}
		if w.ActiveShellPID != 0 && s.Host.ShellPID == w.ActiveShellPID {
			tab = s
		}
		if s.Host.Kind == HostVSCodePanel && (panel == nil || s.LastActivity.After(panel.LastActivity)) {
			panel = s
		}
		if latest == nil || s.LastActivity.After(latest.LastActivity) {
			latest = s
		}
	}
	switch {
	case w.PanelActive && panel != nil:
		return panel
	case tab != nil:
		return tab
	case panel != nil:
		return panel
	}
	return latest
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
	// The chat's own title first: a tab named by corgi or by Claude repeats
	// the label or the title with a glyph in front.
	if s.Title != "" && r.titleUniqueLocked(s) {
		return s.Label + "·" + s.Title
	}
	if term := s.Host.Terminal; term != "" && !strings.Contains(term, s.Label) && !glyphNamed(term) && r.terminalNameUniqueLocked(s) {
		return s.Label + "·" + term
	}
	id := strings.TrimPrefix(s.ID, "pid:")
	if len(id) > 4 {
		id = id[:4]
	}
	return s.Label + "·" + id
}

// glyphNamed: a tab Claude Code or corgi titled itself ("✻ Fix login", "▲ api NEEDS YOU").
func glyphNamed(term string) bool {
	r := []rune(strings.TrimSpace(term))
	return len(r) > 0 && !unicode.IsLetter(r[0]) && !unicode.IsDigit(r[0])
}

// titleUniqueLocked: the chat's own name tells twins apart when they differ.
func (r *Registry) titleUniqueLocked(s *Session) bool {
	for _, o := range r.sessions {
		if o.ID != s.ID && o.Label == s.Label && o.Title == s.Title {
			return false
		}
	}
	return true
}

// terminalNameUniqueLocked: a tab name only tells sessions apart when the
// twins have different ones. A tab corgi named itself ("✓ acme-api 55%")
// repeats the label and tells nothing either. VS Code names every tab running claude by the
// process ("2.1.263"), which tells nothing.
func (r *Registry) terminalNameUniqueLocked(s *Session) bool {
	for _, o := range r.sessions {
		if o.ID != s.ID && o.Label == s.Label && o.Host.Terminal == s.Host.Terminal {
			return false
		}
	}
	return true
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

// groupedLocked is the published order: sessions of one workspace together,
// workspaces alphabetically, oldest session first within each. A list that
// reads top to bottom by repository, and does not reshuffle when a status
// changes.
func (r *Registry) groupedLocked() []*Session {
	out := r.sortedLocked()
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Label) < strings.ToLower(out[j].Label)
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
