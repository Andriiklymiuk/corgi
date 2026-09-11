package daemon

import (
	"fmt"
	"os/exec"
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
	driftLinesFloor = 800
)

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
		a, _ := strconv.Atoi(f[0])
		d, _ := strconv.Atoi(f[1])
		lines += a + d
		files = append(files, f[2])
	}
	return lines, files, true
}

// workspaceRootOf is a seam: the workspace a session's cwd belongs to.
var workspaceRootOf = func(cwd string) string { return sessions.RepoRoot(cwd) }

// driftReasons is what is wrong with a session right now, in the order a
// person would want to hear it.
func driftReasons(s sessions.Session) []string {
	var reasons []string
	if s.Context != nil && s.Context.Percent >= driftContextAt {
		reasons = append(reasons, fmt.Sprintf("context %d%% full — /compact, or fresh from a handoff", s.Context.Percent))
	}
	if s.FailStreak >= driftFailsAt {
		reasons = append(reasons, fmt.Sprintf("the same tool failed %d times running — step in, or /rewind to before the loop", s.FailStreak))
	}
	if s.Cwd == "" {
		return reasons
	}
	root := workspaceRootOf(s.Cwd)
	sc, hasScope := scope.Scope{}, false
	if root != "" {
		sc, hasScope = scope.ForBranch(root, s.Branch)
	}
	lines, files, ok := driftDiff(s.Cwd)
	if !ok {
		return reasons
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
		var outside []string
		for _, f := range files {
			if !sc.Allows(f) {
				outside = append(outside, f)
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
	return reasons
}

// checkDrift runs on the minute sweep over live sessions, and notifies
// once when a session starts drifting.
func (d *Daemon) checkDrift(now time.Time) {
	if d.Sessions == nil {
		return
	}
	for _, s := range d.Sessions.Sessions() {
		if s.Status != sessions.StatusWorking && s.Status != sessions.StatusNeedsInput && s.Status != sessions.StatusDone {
			continue
		}
		reasons := driftReasons(s)
		if _, began := d.Sessions.SetDrift(s.ID, reasons); began {
			label := s.Display
			if label == "" {
				label = s.Label
			}
			go d.notifyAttention("corgi agent · "+label, "drifting: "+reasons[0], s.Folder)
		}
	}
}
