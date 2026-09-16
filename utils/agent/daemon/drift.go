package daemon

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils/agent/scope"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/watch"
	"andriiklymiuk/corgi/utils/gitbase"
)

// A session drifts when the numbers say it is no longer doing what it set
// out to do: the context window nearly full, the same tool failing over
// and over, a diff far past what the ticket agreed, files outside its
// scope. The daemon does not fix that; it says so, once, and on every
// surface, with the three ways out — rewind, split, fresh from a handoff.

const (
	driftContextAt = 85
	driftFailsAt   = 3
	// driftLinesFloor is the diff at which a branch with no scope is worth
	// a look; a scope's own budget, doubled, is the line when there is one.
	// A thousand lines is a feature; five thousand is the number to ask about.
	driftLinesFloor = 3000
)

// GeneratedDiffPath says whether a file's lines are nobody's work: a lock
// file, a snapshot, a bundle. They still count for scope, never for size —
// one `npm install` is a thousand lines of package-lock.json.
func GeneratedDiffPath(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	switch base {
	case "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "bun.lock", "bun.lockb", "go.sum", "cargo.lock",
		"gemfile.lock", "podfile.lock", "poetry.lock", "uv.lock", "composer.lock", "flake.lock", "mix.lock":
		return true
	}
	for _, suffix := range []string{".snap", ".min.js", ".min.css", ".pb.go", ".pb.ts", ".generated.ts", ".generated.go", ".g.dart", ".map"} {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	for _, dir := range []string{"dist/", "build/", "node_modules/", "vendor/", "__generated__/", "__snapshots__/"} {
		if strings.HasPrefix(path, dir) || strings.Contains(path, "/"+dir) {
			return true
		}
	}
	return false
}

// driftDiff is a seam: the diff's size and the files it touches.
var driftDiff = gitDriftDiff

func gitDriftDiff(dir string) (lines int, files []string, ok bool) {
	base, _ := gitbase.MergeBase(dir)
	if base == "" {
		return 0, nil, false
	}
	out, err := exec.Command("git", "-C", dir, "diff", "--numstat", base).Output()
	if err != nil {
		return 0, nil, false
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		files = append(files, f[2])
		if GeneratedDiffPath(f[2]) {
			continue
		}
		a, _ := strconv.Atoi(f[0])
		d, _ := strconv.Atoi(f[1])
		lines += a + d
	}
	return lines, files, true
}

// workspaceRootOf is a seam: the workspace a session's cwd belongs to.
var workspaceRootOf = func(cwd string) string { return sessions.RepoRoot(cwd) }

// driftReasons is what is wrong with a session right now, in the order a
// person would want to hear it: the loud ones first — a context nearly
// full, a tool failing on repeat — then what the diff says. The diff is a
// number about the branch, not the session; it shows on the board and
// never rings.
func driftReasons(s sessions.Session) []string {
	loud, quiet := driftReasonsSplit(s)
	return append(loud, quiet...)
}

func driftReasonsSplit(s sessions.Session) (loud, quiet []string) {
	lines, files, ok := 0, []string(nil), false
	if s.Cwd != "" {
		lines, files, ok = driftDiff(s.Cwd)
	}
	return driftReasonsFrom(s, lines, files, ok)
}

// driftReasonsFrom is driftReasonsSplit given a diff already measured.
func driftReasonsFrom(s sessions.Session, lines int, files []string, ok bool) (loud, quiet []string) {
	loud = loudDriftReasons(s)
	if s.Cwd == "" {
		return loud, nil
	}
	root := workspaceRootOf(s.Cwd)
	sc, hasScope := scope.Scope{}, false
	if root != "" {
		sc, hasScope = scope.ForBranch(root, s.Branch)
	}
	if !ok {
		return loud, nil
	}
	for _, reason := range []string{conflictReason(s), sizeReason(lines, sc, hasScope), scopeReason(s.Cwd, root, sc, hasScope, files)} {
		if reason != "" {
			quiet = append(quiet, reason)
		}
	}
	return loud, quiet
}

// loudDriftReasons are the ones about the session itself, worth a ring.
func loudDriftReasons(s sessions.Session) []string {
	var loud []string
	if s.Context != nil && s.Context.Percent >= driftContextAt {
		loud = append(loud, fmt.Sprintf("context %d%% full — /compact, or fresh from a handoff", s.Context.Percent))
	}
	if s.FailStreak >= driftFailsAt {
		loud = append(loud, fmt.Sprintf("the same tool failed %d times running — step in, or /rewind to before the loop", s.FailStreak))
	}
	return loud
}

func conflictReason(s sessions.Session) string {
	if s.Behind == nil || len(s.Behind.Conflicts) == 0 {
		return ""
	}
	return "main moved: would conflict in " + sessions.JoinFiles(s.Behind.Conflicts, 3) + " — rebase before it grows"
}

// sizeReason says when the diff is past the line: a scope's budget doubled,
// else the floor.
func sizeReason(lines int, sc scope.Scope, hasScope bool) string {
	limit := driftLinesFloor
	if hasScope && sc.Lines > 0 {
		limit = 2 * sc.Lines
	}
	if lines <= limit {
		return ""
	}
	what := "far past the usual size"
	if hasScope && sc.Lines > 0 {
		what = fmt.Sprintf("twice the %d-line budget", sc.Lines)
	}
	return fmt.Sprintf("diff is %d lines, %s — split it, or trim to the spec", lines, what)
}

// scopeReason names the files the diff touches outside the branch's scope.
func scopeReason(cwd, root string, sc scope.Scope, hasScope bool, files []string) string {
	if !hasScope || len(sc.Paths) == 0 {
		return ""
	}
	outside := filesOutsideScope(cwd, root, sc, files)
	if len(outside) == 0 {
		return ""
	}
	shown := outside
	if len(shown) > 3 {
		shown = append(shown[:3], "…")
	}
	return fmt.Sprintf("%d file(s) outside the scope for %s: %s", len(outside), sc.Ref, strings.Join(shown, ", "))
}

// filesOutsideScope is the diff's files the scope does not allow. Diff
// paths are relative to the repository; they are named the way the scope
// was written when the session sits in a sub-repo or worktree.
func filesOutsideScope(cwd, root string, sc scope.Scope, files []string) []string {
	repoRoot := sessions.RepoRoot(cwd)
	var outside []string
	for _, f := range files {
		named := f
		if repoRoot != "" {
			if n := scope.InRepo(root, repoRoot, filepath.Join(repoRoot, f)); n != "" {
				named = n
			}
		}
		if !sc.Allows(named) {
			outside = append(outside, named)
		}
	}
	return outside
}

// checkDrift runs on the minute sweep over live sessions, and notifies
// once when a session starts drifting for a reason worth ringing about.
// An idle session is measured too — its branch is still there, and a
// number that stopped being true (the base branch, a diff since trimmed)
// would otherwise sit on the board until it worked again — but nothing
// rings for it.
func (d *Daemon) checkDrift(now time.Time) {
	if d.Sessions == nil {
		return
	}
	live := d.driftCandidates()
	measured := measureAll(live)
	overlaps := crossings(live, measured)
	d.checkSpend(live, now)
	for _, s := range live {
		d.recordDrift(s, measured[s.ID], overlaps[s.ID], now)
	}
}

// driftCandidates are the sessions whose branch is worth measuring.
func (d *Daemon) driftCandidates() []sessions.Session {
	live := []sessions.Session{}
	for _, s := range d.Sessions.Sessions() {
		switch s.Status {
		case sessions.StatusWorking, sessions.StatusNeedsInput, sessions.StatusDone, sessions.StatusStale:
			live = append(live, s)
		}
	}
	return live
}

// measureAll is one git diff per session, used for everything: the numbers
// on the board, who else is on the same files, and whether it is drifting.
func measureAll(live []sessions.Session) map[string]measure {
	measured := map[string]measure{}
	for _, s := range live {
		if s.Cwd == "" {
			continue
		}
		lines, files, ok := driftDiff(s.Cwd)
		measured[s.ID] = measure{lines: lines, files: files, ok: ok, repo: workspaceRootOf(s.Cwd), tree: workingTreeOf(s.Cwd)}
	}
	return measured
}

// recordDrift puts one session's numbers on the board and rings, once,
// for what began: crossing streams, or drifting for a loud reason. A
// resting session is measured but never rung for.
func (d *Daemon) recordDrift(s sessions.Session, m measure, overlap []sessions.Overlap, now time.Time) {
	d.recordBehind(s, m, now)
	resting := s.Status == sessions.StatusStale
	if m.ok && len(m.files) > 0 && !resting {
		d.ringClaims(s, m.files, now)
	}
	if _, crossed := d.Sessions.SetChanges(s.ID, changesOf(m, now), overlap); crossed && !resting {
		go d.notifyAttention(notifyTitlePrefix+label(s), "crossing streams: "+sessions.OverlapLine(overlap), s.Folder)
	}
	loud, quiet := driftReasonsFrom(s, m.lines, m.files, m.ok)
	if _, began := d.Sessions.SetDrift(s.ID, append(loud, quiet...)); began && len(loud) > 0 && !resting {
		go d.notifyAttention(notifyTitlePrefix+label(s), "drifting: "+loud[0], s.Folder)
	}
}

// recordBehind reads how far behind main a branch with changes is; a branch
// with none has nothing to be behind with.
func (d *Daemon) recordBehind(s sessions.Session, m measure, now time.Time) {
	if !m.ok || len(m.files) == 0 {
		d.Sessions.SetBehind(s.ID, nil)
		return
	}
	if commits, conflicts, upstream, ok := behindOf(s.Cwd); ok {
		d.Sessions.SetBehind(s.ID, &sessions.Behind{Commits: commits, Conflicts: conflicts, Upstream: upstream, At: now})
	}
}

// changesOf is the board's summary of a measured diff, nil when the diff
// could not be read.
func changesOf(m measure, now time.Time) *sessions.Changes {
	if !m.ok {
		return nil
	}
	touched := m.files
	if len(touched) > sessions.TouchedMax {
		touched = touched[:sessions.TouchedMax]
	}
	return &sessions.Changes{Files: len(m.files), Lines: m.lines, Touched: touched, At: now}
}

// measure is one session's diff, and where it lives: the repository (so
// only sessions in the same one are compared) and the working tree (two
// sessions in one tree share every file, whatever the diff says).
type measure struct {
	lines int
	files []string
	ok    bool
	repo  string
	tree  string
}

// worktreeOf is a seam: the working tree a directory belongs to. The
// repository root is the common one; a worktree made with git worktree add
// is its own.
var workingTreeOf = gitWorkingTreeOf

func gitWorkingTreeOf(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// crossings is, for every live session, the other sessions in the same
// repository that touch the same files — or sit in the same working tree,
// which is every file at once. Work crossing streams is the thing the
// board should say before a merge does.
func crossings(live []sessions.Session, measured map[string]measure) map[string][]sessions.Overlap {
	out := map[string][]sessions.Overlap{}
	for i, a := range live {
		ma, ok := measured[a.ID]
		if !ok || ma.repo == "" {
			continue
		}
		for j, b := range live {
			if i == j {
				continue
			}
			if o, ok := overlapWith(b, ma, measured[b.ID]); ok {
				out[a.ID] = append(out[a.ID], o)
			}
		}
	}
	return out
}

// overlapWith is how the other session crosses this one's work, if it does:
// the same checkout, or shared files in the same repository.
func overlapWith(other sessions.Session, mine, theirs measure) (sessions.Overlap, bool) {
	if theirs.repo != mine.repo {
		return sessions.Overlap{}, false
	}
	if mine.tree != "" && mine.tree == theirs.tree {
		return sessions.Overlap{ID: other.ID, Session: overlapName(other), SameCheckout: true}, true
	}
	if !mine.ok || !theirs.ok {
		return sessions.Overlap{}, false
	}
	shared := sharedFiles(mine.files, theirs.files)
	if len(shared) == 0 {
		return sessions.Overlap{}, false
	}
	return sessions.Overlap{ID: other.ID, Session: overlapName(other), Files: shared}, true
}

// sharedFiles is what both diffs touch, generated files aside, capped at
// what a row can show.
func sharedFiles(mine, theirs []string) []string {
	seen := map[string]bool{}
	for _, f := range theirs {
		seen[f] = true
	}
	var shared []string
	for _, f := range mine {
		if seen[f] && !GeneratedDiffPath(f) {
			shared = append(shared, f)
		}
	}
	if len(shared) > sessions.TouchedMax {
		shared = shared[:sessions.TouchedMax]
	}
	return shared
}

// overlapName is the other session by the name a person knows it by: its
// title, its bot, and only then the label·id the board made up.
func overlapName(s sessions.Session) string {
	if t := strings.TrimSpace(s.Title); t != "" {
		if len(t) > 32 {
			t = t[:32] + "…"
		}
		return t
	}
	if s.Bot != "" {
		return s.Bot
	}
	return label(s)
}

// ringClaims rings once when a session edits a file another live session
// claimed — advisory, like the claim: nothing is stopped.
func (d *Daemon) ringClaims(s sessions.Session, touched []string, now time.Time) {
	live := map[string]bool{}
	for _, x := range d.Sessions.Sessions() {
		if x.Status != sessions.StatusGone {
			live[x.ID] = true
		}
	}
	repo := sessions.CommonRoot(s.Cwd)
	if repo == "" {
		return
	}
	crossed := watch.Crossed(watch.LoadFileClaims(d.Dir).Live(live, now), repo, s.ID, touched)
	if len(crossed) == 0 {
		return
	}
	if d.rungClaims == nil {
		d.rungClaims = map[string]bool{}
	}
	var names []string
	for _, c := range crossed {
		key := s.ID + "|" + c.Session + "|" + c.Path
		if d.rungClaims[key] {
			continue
		}
		d.rungClaims[key] = true
		who := c.Label
		if who == "" {
			who = "another session"
		}
		names = append(names, c.Path+" ("+who+")")
	}
	if len(names) == 0 {
		return
	}
	label := s.Display
	if label == "" {
		label = s.Label
	}
	go d.notifyAttention(notifyTitlePrefix+label, "editing a claimed file: "+strings.Join(names, ", "), s.Folder)
}
