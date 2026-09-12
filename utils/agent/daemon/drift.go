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

// generatedDiffPath says whether a file's lines are nobody's work: a lock
// file, a snapshot, a bundle. They still count for scope, never for size —
// one `npm install` is a thousand lines of package-lock.json.
func generatedDiffPath(path string) bool {
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
	base := ""
	for _, b := range []string{"origin/main", "origin/master", "main", "master"} {
		out, err := exec.Command("git", "-C", dir, "merge-base", "HEAD", b).Output()
		if err == nil && strings.TrimSpace(string(out)) != "" {
			base = strings.TrimSpace(string(out))
			break
		}
	}
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
		if generatedDiffPath(f[2]) {
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
	if s.Context != nil && s.Context.Percent >= driftContextAt {
		loud = append(loud, fmt.Sprintf("context %d%% full — /compact, or fresh from a handoff", s.Context.Percent))
	}
	if s.FailStreak >= driftFailsAt {
		loud = append(loud, fmt.Sprintf("the same tool failed %d times running — step in, or /rewind to before the loop", s.FailStreak))
	}
	if s.Cwd == "" {
		return loud, nil
	}
	reasons := quiet
	root := workspaceRootOf(s.Cwd)
	sc, hasScope := scope.Scope{}, false
	if root != "" {
		sc, hasScope = scope.ForBranch(root, s.Branch)
	}
	if !ok {
		return loud, nil
	}
	limit := driftLinesFloor
	if hasScope && sc.Lines > 0 {
		limit = 2 * sc.Lines
	}
	if lines > limit {
		what := "far past the usual size"
		if hasScope && sc.Lines > 0 {
			what = fmt.Sprintf("twice the %d-line budget", sc.Lines)
		}
		reasons = append(reasons, fmt.Sprintf("diff is %d lines, %s — split it, or trim to the spec", lines, what))
	}
	if hasScope && len(sc.Paths) > 0 {
		// Diff paths are relative to the repository; name them the way the
		// scope was written when the session sits in a sub-repo or worktree.
		repoRoot := sessions.RepoRoot(s.Cwd)
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
		if len(outside) > 0 {
			shown := outside
			if len(shown) > 3 {
				shown = append(shown[:3], "…")
			}
			reasons = append(reasons, fmt.Sprintf("%d file(s) outside the scope for %s: %s", len(outside), sc.Ref, strings.Join(shown, ", ")))
		}
	}
	return loud, reasons
}

// checkDrift runs on the minute sweep over live sessions, and notifies
// once when a session starts drifting for a reason worth ringing about.
func (d *Daemon) checkDrift(now time.Time) {
	if d.Sessions == nil {
		return
	}
	live := []sessions.Session{}
	for _, s := range d.Sessions.Sessions() {
		if s.Status != sessions.StatusWorking && s.Status != sessions.StatusNeedsInput && s.Status != sessions.StatusDone {
			continue
		}
		live = append(live, s)
	}
	// One git diff per session, used for everything: the numbers on the
	// board, who else is on the same files, and whether it is drifting.
	measured := map[string]measure{}
	for _, s := range live {
		if s.Cwd == "" {
			continue
		}
		lines, files, ok := driftDiff(s.Cwd)
		measured[s.ID] = measure{lines: lines, files: files, ok: ok, repo: workspaceRootOf(s.Cwd), tree: workingTreeOf(s.Cwd)}
	}
	overlaps := crossings(live, measured)
	d.checkSpend(live, now)
	for _, s := range live {
		m := measured[s.ID]
		var c *sessions.Changes
		if m.ok {
			touched := m.files
			if len(touched) > sessions.TouchedMax {
				touched = touched[:sessions.TouchedMax]
			}
			c = &sessions.Changes{Files: len(m.files), Lines: m.lines, Touched: touched, At: now}
		}
		if _, crossed := d.Sessions.SetChanges(s.ID, c, overlaps[s.ID]); crossed {
			label := s.Display
			if label == "" {
				label = s.Label
			}
			go d.notifyAttention("corgi agent · "+label, "crossing streams: "+sessions.OverlapLine(overlaps[s.ID]), s.Folder)
		}
		loud, quiet := driftReasonsFrom(s, m.lines, m.files, m.ok)
		if _, began := d.Sessions.SetDrift(s.ID, append(loud, quiet...)); began && len(loud) > 0 {
			label := s.Display
			if label == "" {
				label = s.Label
			}
			go d.notifyAttention("corgi agent · "+label, "drifting: "+loud[0], s.Folder)
		}
	}
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
	// The other session by the name a person knows it by: its title, its
	// bot, and only then the label·id the board made up.
	name := func(s sessions.Session) string {
		if t := strings.TrimSpace(s.Title); t != "" {
			if len(t) > 32 {
				t = t[:32] + "…"
			}
			return t
		}
		if s.Bot != "" {
			return s.Bot
		}
		if s.Display != "" {
			return s.Display
		}
		return s.Label
	}
	for i, a := range live {
		ma, ok := measured[a.ID]
		if !ok || ma.repo == "" {
			continue
		}
		for j, b := range live {
			if i == j {
				continue
			}
			mb, ok := measured[b.ID]
			if !ok || mb.repo != ma.repo {
				continue
			}
			if ma.tree != "" && ma.tree == mb.tree {
				out[a.ID] = append(out[a.ID], sessions.Overlap{ID: b.ID, Session: name(b), SameCheckout: true})
				continue
			}
			if !ma.ok || !mb.ok {
				continue
			}
			theirs := map[string]bool{}
			for _, f := range mb.files {
				theirs[f] = true
			}
			var shared []string
			for _, f := range ma.files {
				if theirs[f] && !generatedDiffPath(f) {
					shared = append(shared, f)
				}
			}
			if len(shared) == 0 {
				continue
			}
			if len(shared) > sessions.TouchedMax {
				shared = shared[:sessions.TouchedMax]
			}
			out[a.ID] = append(out[a.ID], sessions.Overlap{ID: b.ID, Session: name(b), Files: shared})
		}
	}
	return out
}
