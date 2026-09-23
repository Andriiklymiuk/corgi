package sessions

import (
	"andriiklymiuk/corgi/utils/agent/peers"
	"bytes"
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

type Registry struct {
	Resolve    func(cwd string) (label, folder string)
	ProfileFor func(configDir string) string
	PullFor    func(link string) (PullFacts, bool)

	mu        sync.Mutex
	saveMu    sync.Mutex
	path      string
	sessions  map[string]*Session
	board     Board
	windows   map[string]Window
	updatedAt time.Time
	dirty     bool
	lastFocus struct {
		WindowID  string
		SessionID string
		At        time.Time
	}
	notice       string
	noticeAt     time.Time
	accounts     []Account
	peers        []PeerBoard
	ended        []Session
	AutoContinue bool
	mutedUntil   time.Time
	OnTransition func(s Session, from, to Status, now time.Time)
}

type State struct {
	UpdatedAt       time.Time `json:"updatedAt"`
	Size            int       `json:"size"`
	Overflow        int       `json:"overflow"`
	NeedsInput      int       `json:"needsInput"`
	Working         int       `json:"working"`
	Slots           []Slot    `json:"slots"`
	Ended           []Session `json:"ended,omitempty"`
	LastFocusWindow string    `json:"lastFocusWindow,omitempty"`
	FrontWindow     string    `json:"frontWindow,omitempty"`
	FrontSession    string    `json:"frontSession,omitempty"`
	AutoContinue    bool      `json:"autoContinue,omitempty"`
	MutedUntil      time.Time `json:"mutedUntil,omitzero"`
	Notice          string    `json:"notice,omitempty"`
	NoticeAt        time.Time `json:"noticeAt,omitempty"`
	Sessions        []Session `json:"sessions"`
	Groups          []Group   `json:"groups,omitempty"`
	Windows         []Window  `json:"windows,omitempty"`
	Accounts        []Account `json:"accounts,omitempty"`
	// Peers are the other laptops' boards, as their last pulse told them (2.29.1).
	Peers []PeerBoard `json:"peers,omitempty"`
}

// PeerBoard is what another laptop is doing: never its token or key.
type PeerBoard struct {
	Name     string              `json:"name"`
	Alive    bool                `json:"alive"`
	Lead     bool                `json:"lead,omitempty"`
	Budget   int                 `json:"budget,omitempty"`
	SeenAt   time.Time           `json:"seenAt,omitempty"`
	Sessions []peers.PeerSession `json:"sessions,omitempty"`
	Runs     []peers.PeerRun     `json:"runs,omitempty"`
}

type Slot struct {
	Index      int       `json:"index"`
	Empty      bool      `json:"empty,omitempty"`
	Pager      bool      `json:"pager,omitempty"`
	Overflow   int       `json:"overflow,omitempty"`
	SessionID  string    `json:"sessionId,omitempty"`
	Label      string    `json:"label,omitempty"`
	Profile    string    `json:"profile,omitempty"`
	Status     Status    `json:"status,omitempty"`
	Pinned     bool      `json:"pinned,omitempty"`
	ElapsedS   int       `json:"elapsedS,omitempty"`
	Detail     string    `json:"detail,omitempty"`
	Host       HostKind  `json:"host,omitempty"`
	FocusError string    `json:"focusError,omitempty"`
	FocusAt    time.Time `json:"focusAt,omitempty"`
	Context    int       `json:"context,omitempty"`
	Pending    string    `json:"pending,omitempty"`
	Risk       string    `json:"risk,omitempty"`
	Note       string    `json:"note,omitempty"`
	Stuck      bool      `json:"stuck,omitempty"`
	Standing   string    `json:"standing,omitempty"`
	Branch     string    `json:"branch,omitempty"`
	Summary    string    `json:"summary,omitempty"`
	PR         string    `json:"pr,omitempty"`
	Ticket     string    `json:"ticket,omitempty"`
	TurnS      int       `json:"turnS,omitempty"`
	Limit      LimitKind `json:"limit,omitempty"`
	ResumeAt   time.Time `json:"resumeAt,omitzero"`
	Drift      string    `json:"drift,omitempty"`
	Changes    string    `json:"changes,omitempty"`
	Tests      string    `json:"tests,omitempty"`
	Overlap    string    `json:"overlap,omitempty"`
	Behind     string    `json:"behind,omitempty"`
	Spend      string    `json:"spend,omitempty"`
	OverCap    bool      `json:"overCap,omitempty"`
	Reading    bool      `json:"reading,omitempty"`
}

func SpendLine(sp *Spend) string {
	if sp == nil || sp.Tokens == 0 {
		return ""
	}
	return Tokens(sp.Tokens)
}

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

func ChangesLine(c *Changes) string {
	if c == nil || (c.Files == 0 && c.Lines == 0) {
		return ""
	}
	return fmt.Sprintf("%d file%s · %d line%s", c.Files, plural(c.Files), c.Lines, plural(c.Lines))
}

func TestsLine(t *TestRun) string {
	if t == nil {
		return ""
	}
	if t.OK {
		return "tests ✓"
	}
	return "tests ✗ " + t.Cmd
}

func BehindLine(b *Behind) string {
	if b == nil || b.Commits == 0 {
		return ""
	}
	line := "main moved " + strconv.Itoa(b.Commits)
	if len(b.Conflicts) > 0 {
		line += " · conflicts in " + JoinFiles(b.Conflicts, 3)
	}
	return line
}

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

func (r *Registry) Path() string { return r.path }

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
	size := r.board.Size
	r.board = NewBoard(size)
	for i := range st.Sessions {
		s := st.Sessions[i]
		if s.ID == "" || s.ClaudePID <= 0 {
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
		r.bind(s)
	}
	r.dirty = true
}

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
	return atomicfile.Write(r.path, data, 0o600)
}

func (r *Registry) Resize(size int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if size == r.board.Size {
		return
	}
	r.board.Resize(size)
	r.touch()
}

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
	if created || r.sessions[s.ID] == nil || s.visible() != before {
		r.touch()
		return true
	}
	return false
}

type visible struct {
	Label, Profile, Detail, Tool, Branch, Summary string
	Status                                        Status
	Host                                          Host
}

func (s *Session) visible() visible {
	return visible{Label: s.Label, Profile: s.Profile, Detail: s.Detail, Tool: s.Tool, Status: s.Status, Host: s.Host, Branch: s.Branch, Summary: s.Summary}
}

func (r *Registry) transition(s *Session, ev Event, now time.Time) {
	switch ev.Name {
	case "SessionStart":
		r.applyStart(s, ev, now)
	case "UserPromptSubmit":
		if s.Status == StatusLimited {
			s.ResumedBy = "person"
		}
		s.Tool, s.Detail, s.Pending = "", "", nil
		s.TurnStartedAt = now
		r.setStatus(s, StatusWorking, now)
	case "PreToolUse":
		r.applyToolStart(s, ev, now)
	case "PostToolUse", "PostToolUseFailure":
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
			s.FailStreak, s.failSubject = 0, ""
		}
		r.setStatus(s, StatusWorking, now)
	case "PermissionRequest":
		s.Tool = ev.Tool
		s.Detail = "permission: " + toolLine(ev.Tool, ev.Subject)
		s.Pending = &Pending{Tool: ev.Tool, Subject: ev.Subject, Risk: ev.Risk, At: now}
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
	}
}

func (r *Registry) applyStart(s *Session, ev Event, now time.Time) {
	if ev.Source == "compact" {
		return
	}
	if s.Status == StatusGone {
		r.board.Pin(r.board.IndexOf(s.ID), true)
	}
	s.Tool, s.Detail, s.Pending = "", "", nil
	r.setStatus(s, StatusDone, now)
}

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
		s.Detail = "question"
		r.setStatus(s, StatusNeedsInput, now)
		return
	}
	r.setStatus(s, StatusWorking, now)
}

func (r *Registry) applyEnd(s *Session, ev Event, now time.Time) {
	if ev.Reason == "resume" || ev.Reason == "clear" {
		r.setStatus(s, StatusStale, now)
		return
	}
	r.dropLocked(s, now)
}

func (r *Registry) applyLimit(s *Session, kind LimitKind, reset string, now time.Time) {
	s.Detail = "limit reached"
	if kind == LimitOverload {
		s.Detail = "API overloaded"
	}
	if reset != "" {
		s.Detail = "resets " + reset
	}
	s.Limit, s.ResumeAt = kind, time.Time{}
	s.ResumedBy = ""
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
		if s.Status == StatusWorking {
			s.Detail = firstNonEmpty(shorten(ev.Message, 48), "waiting for input")
			r.setStatus(s, StatusNeedsInput, now)
		}
	case "quota_auto_resume_fired":
		s.Tool, s.Detail, s.Pending = "", "", nil
		s.ResumedBy = "clock"
		r.setStatus(s, StatusWorking, now)
	case "auth_success", "agent_completed", "elicitation_complete", "elicitation_response", "quota_auto_resume_stale", "quota_auto_resume_disabled":
	}
}

func (r *Registry) adoptLocked(ev Event, now time.Time) *Session {
	if old := r.samePIDLocked(ev.ClaudePID); old != nil {
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

func (r *Registry) refresh(s *Session, ev Event) {
	if ev.Cwd != "" && ev.Cwd != s.Cwd {
		s.Cwd = ev.Cwd
		s.Label, s.Folder = r.resolve(ev.Cwd)
	}
	if s.Label == "" {
		s.Label, s.Folder = r.resolve(s.Cwd)
	}
	if s.Profile == "" || (ev.ConfigDir != "" && ev.ConfigDir != s.ConfigDir) {
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
	if ev.TmuxPane != "" {
		s.TmuxPane = ev.TmuxPane
	}
	if ev.Context != nil {
		c := *ev.Context
		s.Context = &c
	}
	if ev.Title != "" {
		s.Title = ev.Title
	}
	if ev.Model != "" {
		s.Model = ev.Model
	} else if ev.Context != nil && ev.Context.Model != "" {
		s.Model = ev.Context.Model
	}
	if ev.Branch != "" {
		s.Branch = ev.Branch
	}
	if ev.Bot != "" {
		s.Bot = ev.Bot
	}
	if ev.Agent != "" {
		s.Agent = ev.Agent
	}
	if s.Home == "" && ev.Cwd != "" {
		s.Home = ev.Cwd
	}
	if ev.Ticket != "" {
		s.Ticket, s.TicketKey = ev.Ticket, ev.TicketKey
	}
	if ev.Attempt != "" {
		s.Attempt = ev.Attempt
	}
	if ev.Summary != "" {
		s.Summary = ev.Summary
	}
	if ev.PR != "" {
		s.PR = ev.PR
	}
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
	if st == StatusDone || st == StatusNeedsInput {
		s.Resumes = 0
	}
	if r.OnTransition != nil {
		r.OnTransition(*s, from, st, now)
	}
}

const endedKeep = 20

func (r *Registry) dropLocked(s *Session, now time.Time) {
	r.rememberEndedLocked(s, now)
	if r.board.Remove(s.ID) {
		s.Tool, s.Detail = "", ""
		r.setStatus(s, StatusGone, now)
		return
	}
	delete(r.sessions, s.ID)
}

func (r *Registry) rememberEndedLocked(s *Session, now time.Time) {
	if s == nil || Placeholder(s.ID) {
		return
	}
	kept := []Session{{ID: s.ID, Label: s.Label, Display: r.displayLocked(s), Cwd: s.Cwd, Folder: s.Folder, Profile: s.Profile, ConfigDir: s.ConfigDir,
		Status: StatusGone, StatusSince: now, Title: s.Title, Branch: s.Branch, Summary: s.Summary, PR: s.PR, Ticket: s.Ticket, TicketKey: s.TicketKey, Bot: s.Bot, Agent: s.Agent, Home: s.Home, Headless: s.Headless}}
	for _, e := range r.ended {
		if e.ID != s.ID && len(kept) < endedKeep {
			kept = append(kept, e)
		}
	}
	r.ended = kept
}

func (r *Registry) LookupEnded(ref string) (Session, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return Session{}, false
	}
	for _, e := range r.ended {
		if e.ID == ref || strings.HasPrefix(e.ID, ref) {
			return e, true
		}
	}
	return Session{}, false
}

func (r *Registry) Reap(alive func(pid int) bool, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	changed := false
	for _, s := range r.sortedLocked() {
		if s.Status == StatusGone {
			continue
		}
		if s.ClaudePID <= 0 {
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

func (r *Registry) bind(s *Session) {
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

func (r *Registry) bindTermProgram(s *Session, h *Host) {
	if s.TmuxPane != "" {
		h.Kind, h.Pane = HostTmux, s.TmuxPane
		return
	}
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

func (r *Registry) Dismiss(ref string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, err := r.lookupLocked(ref)
	if err != nil {
		return err
	}
	if s.Status == StatusWorking || s.Status == StatusNeedsInput {
		return fmt.Errorf("%s is %s - not dismissing a live session", s.Label, s.Status)
	}
	if i := r.board.IndexOf(s.ID); i >= 0 {
		r.board.Pin(i, false)
	}
	s.Note = ""
	r.dropLocked(s, now)
	r.touch()
	return nil
}

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
	crossed = len(overlap) > 0 && !overlap[0].SameCheckout && len(s.Overlap) == 0
	s.Changes, s.Overlap = c, overlap
	r.touch()
	return true, crossed
}

func (r *Registry) SetBehind(id string, b *Behind) (changed bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	if !ok {
		return false
	}
	if (s.Behind == nil) != (b == nil) {
		if b != nil && s.Behind != nil {
			b.Rebased = s.Behind.Rebased
		}
		s.Behind = b
		r.touch()
		return true
	}
	if b == nil {
		return false
	}
	if s.Behind.Commits == b.Commits && strings.Join(s.Behind.Conflicts, "|") == strings.Join(b.Conflicts, "|") {
		return false
	}
	b.Rebased = s.Behind.Rebased
	s.Behind = b
	r.touch()
	return true
}

func (r *Registry) MainMovedTold(id string, rebased bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	if !ok || s.Behind == nil {
		return
	}
	s.Behind.Told = true
	if rebased {
		s.Behind.Rebased++
	}
	r.touch()
}

func (r *Registry) SetHeadless(id string, running bool, errText string, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var h *Headless
	if s, exists := r.sessions[id]; exists {
		if s.Headless == nil {
			s.Headless = &Headless{}
		}
		h = s.Headless
	} else {
		for i := range r.ended {
			if r.ended[i].ID == id {
				if r.ended[i].Headless == nil {
					r.ended[i].Headless = &Headless{}
				}
				h = r.ended[i].Headless
			}
		}
	}
	if h == nil {
		return
	}
	h.Running, h.Error, h.At = running, errText, now
	if !running && errText == "" {
		h.Turns++
	}
	r.touch()
}

func (r *Registry) SetGate(id string, ok bool, cmd string, now time.Time) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, exists := r.sessions[id]
	if !exists {
		return 0
	}
	fails := 0
	if !ok {
		if s.Gate != nil {
			fails = s.Gate.Fails
		}
		fails++
	}
	s.Gate = &GateRun{OK: ok, At: now, Cmd: cmd, Fails: fails}
	if cmd != "" {
		s.Tests = &TestRun{OK: ok, At: now, Cmd: cmd}
	}
	r.touch()
	return fails
}

func (r *Registry) Compacted(id string, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	if !ok {
		return
	}
	s.Compacted++
	s.CompactedAt = now
	r.touch()
}

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

// SetPeers replaces the other laptops' boards; true when anything changed.
func (r *Registry) SetPeers(list []PeerBoard) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	// SeenAt moves every minute; it alone does not republish the board.
	a, _ := json.Marshal(withoutSeen(r.peers))
	b, _ := json.Marshal(withoutSeen(list))
	if bytes.Equal(a, b) {
		r.peers = list
		return false
	}
	r.peers = list
	r.touch()
	return true
}

func withoutSeen(list []PeerBoard) []PeerBoard {
	out := make([]PeerBoard, len(list))
	for i, p := range list {
		p.SeenAt = time.Time{}
		out[i] = p
	}
	return out
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

func (r *Registry) Peers() []PeerBoard {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]PeerBoard(nil), r.peers...)
}

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

func (r *Registry) Page(direction int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.board.Page(direction) {
		return false
	}
	r.touch()
	return true
}

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
		if h := proc.HarnessOf(p); h != "claude" {
			s.Agent = h
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

func notASession(args string) bool {
	for _, marker := range []string{"remote-control", " mcp ", " mcp serve", "--print", " -p ", " -p\n", " exec "} {
		if strings.Contains(args+"\n", marker) {
			return true
		}
	}
	return false
}

type FocusTarget struct {
	SessionID string
	Kind      HostKind
	App       string
	Folder    string
	WindowID  string
	ShellPID  int
	Panel     bool
	TTY       uint64
	Pane      string
	New       bool
	Connected bool
	Title     string
	Label     string
}

var ErrNoSession = errors.New("no such session")

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
		ShellPID: h.ShellPID, Panel: h.Kind == HostVSCodePanel, Connected: h.Connected, TTY: s.TTY, Pane: h.Pane,
		Title: s.Title, Label: r.displayLocked(s),
	}, nil
}

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
			return "", fmt.Errorf("%s asks to run %q - look at it before allowing", r.displayLocked(s), s.Pending.Subject)
		}
		if answer == "always" {
			return "2\r", nil
		}
		return "\r", nil
	}
	return "", fmt.Errorf("answer is allow, always or deny, not %q", answer)
}

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

func (r *Registry) ReadBy(ref string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, err := r.lookupLocked(ref)
	if err != nil {
		return err
	}
	if now.Sub(s.ReadAt) < 20*time.Second {
		return nil
	}
	s.ReadAt = now
	r.touch()
	return nil
}

func (r *Registry) AutoAllowed(ref string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, err := r.lookupLocked(ref)
	if err != nil {
		return
	}
	s.AutoAllowed++
	r.touch()
}

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

func (p *Pending) Risky() bool {
	if p == nil {
		return false
	}
	if p.Risk != "" {
		return p.Risk == "destructive"
	}
	return p.Tool == "Bash" && riskyCommand.MatchString(p.Subject)
}

var riskyCommand = regexp.MustCompile(`(?i)(^|\s)(rm|sudo|mkfs|dd|shutdown|reboot|kill|pkill|killall|chmod|chown|launchctl|diskutil)(\s|$)|--force|--hard|--no-verify|\bdrop\b|\btruncate\b|\bpurge\b`)

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

func (r *Registry) SetMuted(until time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.mutedUntil.Equal(until) {
		return false
	}
	r.mutedUntil = until
	r.touch()
	return true
}

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

var ErrNoWindow = errors.New("no editor window connected - open a folder in VS Code with the corgi extension installed")

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
		return nil, fmt.Errorf("%q matches %d sessions - use more of the id", ref, len(byPrefix))
	case len(byLabel) == 1:
		return byLabel[0], nil
	case len(byLabel) > 1:
		return nil, fmt.Errorf("%d sessions are called %q - use the id", len(byLabel), ref)
	}
	return nil, ErrNoSession
}

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

func (r *Registry) Snapshot(now time.Time) State {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.snapshotLocked(now)
}

func (r *Registry) snapshotLocked(now time.Time) State {
	st := State{UpdatedAt: r.updatedAt, Size: r.board.Size, Overflow: r.board.Hidden(),
		LastFocusWindow: r.lastFocus.WindowID, Notice: r.notice, NoticeAt: r.noticeAt, AutoContinue: r.AutoContinue, MutedUntil: r.mutedUntil}
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
		sl.Standing = StandingOf(s.Facts(r.pullFactsLocked(s))).Word
		sl.Limit, sl.ResumeAt = s.Limit, s.ResumeAt
		if len(s.Drift) > 0 {
			sl.Drift = s.Drift[0]
		}
		sl.Changes, sl.Tests, sl.Overlap, sl.Behind = ChangesLine(s.Changes), TestsLine(s.Tests), OverlapLine(s.Overlap), BehindLine(s.Behind)
		sl.Spend, sl.OverCap = SpendLine(s.Spend), s.OverCap
		sl.Reading = !s.ReadAt.IsZero() && now.Sub(s.ReadAt) < time.Minute
		sl.Branch, sl.Summary, sl.PR, sl.Ticket = s.Branch, s.Summary, s.PR, s.Ticket
		if s.Status == StatusWorking && !s.TurnStartedAt.IsZero() && now.After(s.TurnStartedAt) {
			sl.TurnS = int(now.Sub(s.TurnStartedAt).Seconds())
		}
		if s.Context != nil {
			sl.Context = s.Context.Percent
		}
		if s.Pending != nil {
			sl.Pending, sl.Risk = s.Pending.Tool, s.Pending.Risk
		}
		if !s.StatusSince.IsZero() && now.After(s.StatusSince) {
			sl.ElapsedS = int(now.Sub(s.StatusSince).Seconds())
		}
		st.Slots = append(st.Slots, sl)
	}
	for _, s := range r.groupedLocked() {
		c := *s
		c.Display = r.displayLocked(s)
		standing := StandingOf(s.Facts(r.pullFactsLocked(s)))
		c.Standing = &standing
		st.Sessions = append(st.Sessions, c)
		switch s.Status {
		case StatusNeedsInput:
			st.NeedsInput++
		case StatusWorking:
			st.Working++
		}
	}
	st.Groups = Groups(st.Sessions)
	st.Windows = r.sortedWindowsLocked()
	st.Accounts = append([]Account(nil), r.accounts...)
	st.Peers = append([]PeerBoard(nil), r.peers...)
	st.Ended = append([]Session(nil), r.ended...)
	if w, ok := r.frontWindowLocked(); ok {
		st.FrontWindow = w.ID
		if s := r.frontSessionLocked(w); s != nil {
			st.FrontSession = s.ID
		}
	}
	return st
}

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

func glyphNamed(term string) bool {
	r := []rune(strings.TrimSpace(term))
	return len(r) > 0 && !unicode.IsLetter(r[0]) && !unicode.IsDigit(r[0])
}

func (r *Registry) titleUniqueLocked(s *Session) bool {
	for _, o := range r.sessions {
		if o.ID != s.ID && o.Label == s.Label && o.Title == s.Title {
			return false
		}
	}
	return true
}

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

func shorten(s string, limit int) string {
	s = strings.TrimSpace(strings.SplitN(s, "\n", 2)[0])
	if runes := []rune(s); len(runes) > limit {
		return strings.TrimSpace(string(runes[:limit-1])) + "…"
	}
	return s
}

func (r *Registry) pullFactsLocked(s *Session) *PullFacts {
	if r.PullFor == nil || s.PR == "" {
		return nil
	}
	if p, ok := r.PullFor(s.PR); ok {
		return &p
	}
	return nil
}
