package sessions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/proc"
)

var t0 = time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)

func ev(name, id string, at time.Duration) Event {
	return Event{Name: name, SessionID: id, Cwd: "/home/me/dev/acme-api", ClaudePID: 100, Ancestors: []int{100, 90, 80}, At: t0.Add(at)}
}

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	return New(filepath.Join(t.TempDir(), "sessions.json"), 3)
}

func status(t *testing.T, r *Registry, id string) Status {
	t.Helper()
	s, err := r.Lookup(id)
	if err != nil {
		t.Fatalf("lookup %s: %v", id, err)
	}
	return s.Status
}

func TestApplyWalksTheStatusModel(t *testing.T) {
	r := newTestRegistry(t)
	start := ev("SessionStart", "s1", 0)
	start.Source = "startup"
	if !r.Apply(start) || status(t, r, "s1") != StatusDone {
		t.Fatal("SessionStart registers as done")
	}
	r.Apply(ev("UserPromptSubmit", "s1", time.Second))
	if status(t, r, "s1") != StatusWorking {
		t.Fatal("a prompt means working")
	}
	pre := ev("PreToolUse", "s1", 2*time.Second)
	pre.Tool = "Bash"
	r.Apply(pre)
	if s, _ := r.Lookup("s1"); s.Status != StatusWorking || s.Tool != "Bash" || s.Detail != "Bash" {
		t.Fatalf("tool use keeps working and names the tool, got %+v", s)
	}
	perm := ev("PermissionRequest", "s1", 3*time.Second)
	perm.Tool = "Bash"
	r.Apply(perm)
	if s, _ := r.Lookup("s1"); s.Status != StatusNeedsInput || s.Detail != "permission: Bash" {
		t.Fatalf("a permission request needs input, got %+v", s)
	}
	post := ev("PostToolUse", "s1", 4*time.Second)
	post.Tool = "Bash"
	r.Apply(post)
	if s, _ := r.Lookup("s1"); s.Status != StatusWorking || s.Detail != "" {
		t.Fatalf("the tool ran, so the prompt was answered, got %+v", s)
	}
	r.Apply(ev("Stop", "s1", 5*time.Second))
	if status(t, r, "s1") != StatusDone {
		t.Fatal("Stop means done")
	}
	fail := ev("StopFailure", "s1", 6*time.Second)
	fail.Error = "rate_limit"
	r.Apply(fail)
	if s, _ := r.Lookup("s1"); s.Status != StatusNeedsInput || s.Detail != "rate_limit" {
		t.Fatalf("an API failure needs a person, got %+v", s)
	}
	end := ev("SessionEnd", "s1", 7*time.Second)
	end.Reason = "resume"
	r.Apply(end)
	if status(t, r, "s1") != StatusStale {
		t.Fatal("SessionEnd resume keeps the session, stale")
	}
	end.Reason = "other"
	r.Apply(end)
	if _, err := r.Lookup("s1"); err == nil {
		t.Fatal("SessionEnd for any other reason deregisters")
	}
	if r.Apply(end) {
		t.Fatal("an end for an unknown session changes nothing")
	}
	if r.Apply(Event{}) {
		t.Fatal("an empty event changes nothing")
	}
}

func TestNotificationsAndQuestions(t *testing.T) {
	r := newTestRegistry(t)
	r.Apply(ev("UserPromptSubmit", "s1", 0))
	idle := ev("Notification", "s1", time.Second)
	idle.Notification = "idle_prompt"
	idle.Message = "Claude is waiting for your input"
	r.Apply(idle)
	if status(t, r, "s1") != StatusNeedsInput {
		t.Fatal("an idle nudge mid-turn is a question corgi did not see")
	}
	r.Apply(ev("Stop", "s1", 2*time.Second))
	r.Apply(idle)
	if status(t, r, "s1") != StatusDone {
		t.Fatal("an idle nudge after a finished turn is noise, not a need")
	}
	perm := ev("Notification", "s1", 3*time.Second)
	perm.Notification = "permission_prompt"
	perm.Message = "Claude needs your permission to use Bash"
	r.Apply(perm)
	if s, _ := r.Lookup("s1"); s.Status != StatusNeedsInput || s.Detail != "Claude needs your permission to use Bash" {
		t.Fatalf("permission prompt: %+v", s)
	}
	auth := ev("Notification", "s1", 4*time.Second)
	auth.Notification = "auth_success"
	r.Apply(auth)
	if status(t, r, "s1") != StatusNeedsInput {
		t.Fatal("an irrelevant notification changes no status")
	}
	q := ev("PreToolUse", "s1", 5*time.Second)
	q.Tool = "AskUserQuestion"
	r.Apply(q)
	if s, _ := r.Lookup("s1"); s.Status != StatusNeedsInput || s.Detail != "question" {
		t.Fatalf("a question needs input the moment it is asked, got %+v", s)
	}
	compact := ev("SessionStart", "s1", 6*time.Second)
	compact.Source = "compact"
	r.Apply(compact)
	if status(t, r, "s1") != StatusNeedsInput {
		t.Fatal("compaction is not a new session")
	}
}

func TestReapFreesKeysAndPinnedSlotsStayGone(t *testing.T) {
	r := newTestRegistry(t)
	a := ev("SessionStart", "a", 0)
	b := ev("SessionStart", "b", time.Second)
	b.ClaudePID, b.Ancestors = 200, []int{200}
	r.Apply(a)
	r.Apply(b)
	if !r.Pin(0, true) {
		t.Fatal("pin slot 0")
	}
	dead := map[int]bool{100: true}
	alive := func(pid int) bool { return !dead[pid] }
	if !r.Reap(alive, t0.Add(5*time.Second)) {
		t.Fatal("reap should drop a")
	}
	if status(t, r, "a") != StatusGone {
		t.Fatal("a pinned dead session is gone, not removed")
	}
	st := r.Snapshot(t0.Add(6 * time.Second))
	if st.Slots[0].SessionID != "a" || !st.Slots[0].Pinned || st.Slots[0].Status != StatusGone {
		t.Fatalf("slot 0 = %+v", st.Slots[0])
	}
	if r.Reap(alive, t0.Add(7*time.Second)) {
		t.Fatal("a second reap finds nothing new")
	}
	// It comes back (resume in the same slot) and lights up again.
	r.Apply(ev("SessionStart", "a", 8*time.Second))
	if status(t, r, "a") != StatusDone || r.Snapshot(t0).Slots[0].SessionID != "a" {
		t.Fatal("a revived pinned session keeps its key")
	}
	dead[100] = true
	r.Reap(alive, t0.Add(9*time.Second))
	r.Pin(0, false)
	if _, err := r.Lookup("a"); err == nil {
		t.Fatal("unpinning a gone session drops it")
	}
	if snap := r.Snapshot(t0); !snap.Slots[0].Empty || snap.Slots[1].SessionID != "b" {
		t.Fatalf("nothing overflows, so the freed key stays empty and b keeps its key: %+v", snap.Slots)
	}
	if r.Pin(0, false) != true || r.Pin(9, true) {
		t.Fatal("pin bounds")
	}
}

func TestSweepMarksStaleButNeverANeed(t *testing.T) {
	r := newTestRegistry(t)
	r.Apply(ev("Stop", "done", 0))
	busy := ev("UserPromptSubmit", "busy", 0)
	busy.ClaudePID = 101
	r.Apply(busy)
	need := ev("PermissionRequest", "need", 0)
	need.Tool, need.ClaudePID = "Edit", 102
	r.Apply(need)
	if r.Sweep(t0.Add(StaleAfter - time.Minute)) {
		t.Fatal("nothing is stale yet")
	}
	if !r.Sweep(t0.Add(StaleAfter + time.Minute)) {
		t.Fatal("sweep should mark")
	}
	if status(t, r, "done") != StatusStale || status(t, r, "busy") != StatusStale {
		t.Fatal("done and working go stale")
	}
	if status(t, r, "need") != StatusNeedsInput {
		t.Fatal("a session waiting on a person never goes stale")
	}
	r.Apply(ev("UserPromptSubmit", "busy", StaleAfter+2*time.Minute))
	if status(t, r, "busy") != StatusWorking {
		t.Fatal("activity wakes a stale session")
	}
}

func TestWindowJoinRules(t *testing.T) {
	r := newTestRegistry(t)
	r.Resolve = func(cwd string) (string, string) { return "acme-api", "/home/me/dev/acme-api" }
	term := ev("SessionStart", "term", 0)
	term.Window = "win-1"
	term.Ancestors = []int{100, 90, 80, 70}
	panel := ev("SessionStart", "panel", 0)
	panel.ClaudePID, panel.Ancestors = 300, []int{300, 555, 70}
	plain := ev("SessionStart", "plain", 0)
	plain.ClaudePID, plain.Ancestors, plain.TermProgram = 400, []int{400, 401}, "vscode"
	iterm := ev("SessionStart", "iterm", 0)
	iterm.ClaudePID, iterm.Ancestors, iterm.TermProgram, iterm.Cwd = 500, []int{500}, "iTerm.app", "/home/me/dev/other"
	for _, e := range []Event{term, panel, plain, iterm} {
		r.Apply(e)
	}
	before, _ := r.Lookup("term")
	if before.Host.Kind != HostVSCodeTerminal || before.Host.Connected || before.Host.WindowID != "win-1" {
		t.Fatalf("an injected window id binds even before the window connects: %+v", before.Host)
	}
	windows := []Window{{
		ID: "win-1", App: "Visual Studio Code", ExtHostPID: 555, Folders: []string{"/home/me/dev/acme-api"},
		Terminals: []Terminal{{Name: "zsh", ShellPID: 90}, {Name: "zsh 2", ShellPID: 91}}, UpdatedAt: t0,
	}}
	if !r.SetWindows(windows) {
		t.Fatal("new windows should change the join")
	}
	if r.SetWindows(windows) {
		t.Fatal("the same windows again change nothing")
	}
	got, _ := r.Lookup("term")
	if got.Host.Kind != HostVSCodeTerminal || got.Host.ShellPID != 90 || got.Host.Terminal != "zsh" || !got.Host.Connected || got.Host.App != "Visual Studio Code" {
		t.Fatalf("rule 1: %+v", got.Host)
	}
	got, _ = r.Lookup("panel")
	if got.Host.Kind != HostVSCodePanel || got.Host.WindowID != "win-1" || got.Host.Folder != "/home/me/dev/acme-api" {
		t.Fatalf("rule 2: %+v", got.Host)
	}
	got, _ = r.Lookup("plain")
	if got.Host.Kind != HostVSCodeTerminal || got.Host.WindowID != "win-1" || !got.Host.Connected {
		t.Fatalf("rule 3 matches an integrated terminal to a window by folder: %+v", got.Host)
	}
	got, _ = r.Lookup("iterm")
	if got.Host.Kind != HostITerm || got.Host.Folder != "/home/me/dev/acme-api" {
		t.Fatalf("iTerm: %+v", got.Host)
	}
	// An unregistered directory names no folder: focus activates the app
	// rather than opening a new window on the cwd.
	r.Resolve = DefaultResolve
	scratch := ev("SessionStart", "scratch", 0)
	scratch.ClaudePID, scratch.Ancestors, scratch.Cwd, scratch.TermProgram = 600, []int{600}, "/tmp/scratch", "vscode"
	r.Apply(scratch)
	got, _ = r.Lookup("scratch")
	if got.Host.Kind != HostVSCodeTerminal || got.Host.Folder != "" || got.Host.Connected {
		t.Fatalf("scratch: %+v", got.Host)
	}

	target, err := r.Focus("term")
	if err != nil || target.ShellPID != 90 || target.Folder != "/home/me/dev/acme-api" || target.Panel {
		t.Fatalf("focus term = %+v, %v", target, err)
	}
	target, _ = r.Focus("panel")
	if !target.Panel || target.WindowID != "win-1" {
		t.Fatalf("focus panel = %+v", target)
	}
	if _, err := r.Focus("nope"); err == nil {
		t.Fatal("unknown ref")
	}
	// Window gone: seats stay, focus degrades.
	r.SetWindows(nil)
	got, _ = r.Lookup("term")
	if got.Host.Connected || got.Host.ShellPID != 0 {
		t.Fatalf("after disconnect: %+v", got.Host)
	}
	r.RecordFocus("term", ErrNoSession)
	r.RecordFocus("term", ErrNoSession)
	if s, _ := r.Lookup("term"); s.FocusError == "" {
		t.Fatal("focus failure is recorded")
	}
	r.RecordFocus("nobody", nil)
	r.Apply(ev("Stop", "term", time.Minute))
	if s, _ := r.Lookup("term"); s.FocusError != "" {
		t.Fatal("the next event clears it")
	}
}

func TestLabelsAndLookup(t *testing.T) {
	r := newTestRegistry(t)
	r.Resolve = func(cwd string) (string, string) {
		if cwd == "/home/me/dev/acme-api" {
			return "acme-api", cwd
		}
		return "", ""
	}
	r.ProfileFor = func(dir string) string {
		if dir == "/home/me/.claude-work" {
			return "work"
		}
		return ""
	}
	one := ev("SessionStart", "abcd-1", 0)
	one.ConfigDir = "/home/me/.claude-work"
	two := ev("SessionStart", "efgh-2", time.Second)
	two.ClaudePID = 101
	other := ev("SessionStart", "ijkl-3", 2*time.Second)
	other.Cwd, other.ClaudePID, other.ConfigDir = "/tmp/scratch", 102, "/home/me/.claude-personal"
	for _, e := range []Event{one, two, other} {
		r.Apply(e)
	}
	st := r.Snapshot(t0.Add(time.Minute))
	if st.Slots[0].Label != "acme-api·abcd" || st.Slots[1].Label != "acme-api·efgh" {
		t.Fatalf("twins get a suffix: %q %q", st.Slots[0].Label, st.Slots[1].Label)
	}
	if st.Slots[0].Profile != "work" || st.Slots[2].Profile != "personal" {
		t.Fatalf("profiles: %q %q", st.Slots[0].Profile, st.Slots[2].Profile)
	}
	if st.Slots[2].Label != "scratch" {
		t.Fatalf("an unregistered dir is its base name, got %q", st.Slots[2].Label)
	}
	if st.Slots[0].ElapsedS != 60 {
		t.Fatalf("elapsed = %d", st.Slots[0].ElapsedS)
	}
	if s, err := r.Lookup("scratch"); err != nil || s.ID != "ijkl-3" {
		t.Fatalf("lookup by label: %+v %v", s, err)
	}
	if s, err := r.Lookup("ijk"); err != nil || s.ID != "ijkl-3" {
		t.Fatalf("lookup by prefix: %+v %v", s, err)
	}
	if _, err := r.Lookup("acme-api"); err == nil {
		t.Fatal("an ambiguous label is refused")
	}
	if s, err := r.Lookup("acme-api·efgh"); err != nil || s.ID != "efgh-2" {
		t.Fatalf("the display name is unique: %v", err)
	}
	if s, err := r.Lookup("#3"); err != nil || s.ID != "ijkl-3" {
		t.Fatalf("lookup by key number: %+v %v", s, err)
	}
	if _, err := r.Lookup("9"); err == nil {
		t.Fatal("an empty key is nothing")
	}
	if _, err := r.Lookup(""); err == nil {
		t.Fatal("empty ref")
	}
	cwd := ev("CwdChanged", "ijkl-3", 3*time.Second)
	cwd.Cwd = "/home/me/dev/acme-api"
	r.Apply(cwd)
	if s, _ := r.Lookup("ijkl-3"); s.Label != "acme-api" {
		t.Fatal("CwdChanged relabels")
	}
}

func TestPersistenceKeepsTheBoard(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	r := New(path, 3)
	r.Apply(ev("SessionStart", "a", 0))
	b := ev("SessionStart", "b", time.Second)
	b.ClaudePID = 101
	r.Apply(b)
	r.Pin(1, true)
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(); err != nil {
		t.Fatal("a clean save is a no-op")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("sessions.json must be owner-only: %v %v", info, err)
	}
	var st State
	data, _ := os.ReadFile(path)
	if json.Unmarshal(data, &st) != nil || len(st.Sessions) != 2 || st.Size != 3 {
		t.Fatalf("published state: %+v", st)
	}

	again := New(path, 6)
	again.Load()
	snap := again.Snapshot(t0)
	if snap.Size != 6 || snap.Slots[0].SessionID != "a" || snap.Slots[1].SessionID != "b" || !snap.Slots[1].Pinned {
		t.Fatalf("the board survives a restart at the configured size: %+v", snap.Slots)
	}
	again.Resize(2)
	again.Resize(2)
	if snap := again.Snapshot(t0); snap.Size != 2 || snap.Slots[1].SessionID != "b" {
		t.Fatalf("resize keeps seats: %+v", snap.Slots)
	}

	// A session with no pid to probe is not restored: it could never be reaped.
	nopid := New(path, 3)
	nopid.Apply(Event{Name: "Stop", SessionID: "ghost", At: t0})
	_ = nopid.Save()
	restored := New(path, 3)
	restored.Load()
	if _, err := restored.Lookup("ghost"); err == nil {
		t.Fatal("a pid-less session is dropped on load")
	}

	missing := New(filepath.Join(t.TempDir(), "none.json"), 2)
	missing.Load()
	if missing.Snapshot(t0).Size != 2 {
		t.Fatal("a missing file is an empty board")
	}
	_ = os.WriteFile(path, []byte("{nope"), 0o600)
	broken := New(path, 2)
	broken.Load()
	if broken.Snapshot(t0).Size != 2 {
		t.Fatal("a corrupt file is an empty board")
	}
}

func TestAdoptGivesRescannedProcessesAKeyUntilAHookNamesThem(t *testing.T) {
	r := newTestRegistry(t)
	orig := proc.Lookup
	proc.Lookup = func(pid int) (proc.Process, bool) {
		if pid == 700 {
			return proc.Process{PID: 700, PPID: 1, Name: "claude"}, true
		}
		return proc.Process{}, false
	}
	t.Cleanup(func() { proc.Lookup = orig })
	procs := []proc.Process{
		{PID: 700, Name: "claude"},
		{PID: 701, Name: "claude", Args: "claude remote-control"},
		{PID: 702, Name: "zsh"},
	}
	added := r.Adopt(procs, func(pid int) string { return "/home/me/dev/acme-api" }, t0)
	if added != 1 {
		t.Fatalf("adopted %d, want 1 (no supervised remote-control, no shells)", added)
	}
	s, err := r.Lookup(PlaceholderID(700))
	if err != nil || s.Status != StatusUnknown || s.Label != "acme-api" || s.Ancestors[0] != 700 {
		t.Fatalf("placeholder: %+v %v", s, err)
	}
	if r.Adopt(procs, nil, t0) != 0 {
		t.Fatal("already known")
	}
	// The first hook from that pid replaces the placeholder, same key.
	real := ev("UserPromptSubmit", "real-id", time.Second)
	real.ClaudePID = 700
	r.Apply(real)
	if _, err := r.Lookup(PlaceholderID(700)); err == nil {
		t.Fatal("placeholder should be gone")
	}
	if snap := r.Snapshot(t0); snap.Slots[0].SessionID != "real-id" || snap.Slots[0].Status != StatusWorking {
		t.Fatalf("slot 0 = %+v", snap.Slots[0])
	}
	if !Placeholder("pid:1") || Placeholder("abc") {
		t.Fatal("Placeholder")
	}
}

func TestPagerInSnapshot(t *testing.T) {
	r := newTestRegistry(t)
	for i, id := range []string{"a", "b", "c", "d"} {
		e := ev("SessionStart", id, time.Duration(i)*time.Second)
		e.ClaudePID = 100 + i
		r.Apply(e)
	}
	st := r.Snapshot(t0)
	if st.Overflow != 2 || !st.Slots[2].Pager || st.Slots[2].Overflow != 2 || st.Slots[2].SessionID != "" {
		t.Fatalf("pager slot = %+v overflow %d", st.Slots[2], st.Overflow)
	}
	if !r.Page(1) || r.Snapshot(t0).Slots[0].SessionID != "c" {
		t.Fatalf("page: %+v", r.Snapshot(t0).Slots)
	}
	if r.Page(0) {
		t.Fatal("direction 0 is nothing")
	}
}

func TestDefaults(t *testing.T) {
	for in, want := range map[string]string{"": "default", "/home/me/.claude": "default", "/home/me/.claude-work": "work", "/x/personal": "personal"} {
		if got := DefaultProfile(in); got != want {
			t.Errorf("DefaultProfile(%q) = %q, want %q", in, got, want)
		}
	}
	if l, f := DefaultResolve(""); l != "?" || f != "" {
		t.Fatal("empty cwd")
	}
	if l, f := DefaultResolve("/a/b/c"); l != "c" || f != "" {
		t.Fatal("base name, and no folder an editor should open")
	}
	for args, want := range map[string]bool{"claude": false, "claude remote-control": true, "claude mcp serve": true, "claude -p hi": true, "claude --print x": true, "claude -p": true} {
		if notASession(args) != want {
			t.Errorf("notASession(%q) = %v", args, !want)
		}
	}
	if itoa(0) != "0" || itoa(-12) != "-12" || itoa(345) != "345" {
		t.Fatal("itoa")
	}
	if shorten("a\nb", 5) != "a" || shorten("abcdefgh", 5) != "abcd…" {
		t.Fatal("shorten")
	}
}

func TestSameProcessUnderANewSessionIdKeepsItsKey(t *testing.T) {
	r := newTestRegistry(t)
	r.Apply(ev("Stop", "old-id", 0))
	other := ev("Stop", "other", time.Second)
	other.ClaudePID = 101
	r.Apply(other)
	r.Pin(0, true)
	end := ev("SessionEnd", "old-id", 2*time.Second)
	end.Reason = "resume"
	r.Apply(end)
	start := ev("SessionStart", "new-id", 3*time.Second)
	start.Source = "resume"
	r.Apply(start)
	if _, err := r.Lookup("old-id"); err == nil {
		t.Fatal("the old id is gone: one process, one session")
	}
	snap := r.Snapshot(t0)
	if snap.Slots[0].SessionID != "new-id" || !snap.Slots[0].Pinned || snap.Slots[0].Status != StatusDone || snap.Slots[1].SessionID != "other" {
		t.Fatalf("slots = %+v", snap.Slots)
	}
	if len(snap.Sessions) != 2 {
		t.Fatalf("sessions = %d", len(snap.Sessions))
	}
}

func TestOnlyVisibleChangesArePublished(t *testing.T) {
	r := newTestRegistry(t)
	pre := ev("PreToolUse", "s1", 0)
	pre.Tool = "Read"
	if !r.Apply(pre) {
		t.Fatal("a new session is a change")
	}
	_ = r.Save()
	pre.At = t0.Add(time.Second)
	if r.Apply(pre) {
		t.Fatal("the same tool again changes nothing a key shows")
	}
	if r.Save() != nil || r.dirty {
		t.Fatal("nothing to write")
	}
	if s, _ := r.Lookup("s1"); !s.LastActivity.Equal(t0.Add(time.Second)) {
		t.Fatal("activity is still tracked in memory")
	}
	pre.Tool = "Bash"
	if !r.Apply(pre) {
		t.Fatal("a different tool is a change")
	}
}
