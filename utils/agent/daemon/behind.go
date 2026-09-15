package daemon

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/gitbase"
)

// Main moves while a session works. The minute sweep measures how far a
// branch has fallen behind its base and whether the two would conflict —
// the thing nobody wants to learn at merge time — and the board says so.
// With hand-over on, a session that stops with conflicts is told; with
// rebase on, one that stops clean is rebased where it sits.

// behindOf is a seam: commits on the base branch since the branch left it,
// and the files the two would fight over. ok is false when there is no
// base to measure against.
var behindOf = gitBehind

func gitBehind(dir string) (commits int, conflicts []string, upstream string, ok bool) {
	for _, b := range gitbase.Refs(dir) {
		out, err := exec.Command("git", "-C", dir, "rev-list", "--count", "HEAD.."+b).Output()
		if err != nil {
			continue
		}
		upstream = b
		commits, _ = strconv.Atoi(strings.TrimSpace(string(out)))
		break
	}
	if upstream == "" {
		return 0, nil, "", false
	}
	if commits == 0 {
		return 0, nil, upstream, true
	}
	// merge-tree without touching the tree: exit 1 is a conflict; the
	// lines after the tree id, up to the blank one, name the files (the
	// messages that follow are for a person).
	out, err := exec.Command("git", "-C", dir, "merge-tree", "--write-tree", "--name-only", "HEAD", upstream).Output()
	if err != nil {
		lines := strings.Split(string(out), "\n")
		for _, l := range lines[1:] {
			if l = strings.TrimSpace(l); l == "" {
				break
			} else {
				conflicts = append(conflicts, l)
			}
		}
	}
	return commits, conflicts, upstream, true
}

// gitClean says the working tree has nothing uncommitted: the one state a
// rebase may run in without a person.
var gitClean = func(dir string) bool {
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	return err == nil && strings.TrimSpace(string(out)) == ""
}

// gitRebase rebases the tree's branch onto upstream, and aborts on any
// failure so the tree is as it was.
var gitRebase = func(ctx context.Context, dir, upstream string) error {
	if err := exec.CommandContext(ctx, "git", "-C", dir, "rebase", "--quiet", upstream).Run(); err != nil {
		_ = exec.Command("git", "-C", dir, "rebase", "--abort").Run()
		return err
	}
	return nil
}

// onMainMoved runs when a session stops behind its base: with rebase on
// and nothing in the way, the branch is rebased where it sits; with
// hand-over on and conflicts, the session is told what to resolve. Each
// once per state — the sweep sets Told back when the numbers change.
func (d *Daemon) onMainMoved(s sessions.Session) {
	if d.Policy == nil || s.Behind == nil || s.Behind.Commits == 0 || s.Cwd == "" || s.Detail == "interrupted" || s.Behind.Told {
		return
	}
	p := d.Policy(s)
	label := s.Display
	if label == "" {
		label = s.Label
	}
	switch {
	case len(s.Behind.Conflicts) == 0 && p.Rebase:
		if !gitClean(s.Cwd) {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := gitRebase(ctx, s.Cwd, s.Behind.Upstream); err != nil {
			utils.Infof("agent: rebase %s onto %s: %v\n", label, s.Behind.Upstream, err)
			d.Sessions.MainMovedTold(s.ID, false)
			return
		}
		utils.Infof("agent: rebased %s onto %s (%d commits)\n", label, s.Behind.Upstream, s.Behind.Commits)
		d.Sessions.MainMovedTold(s.ID, true)
		d.flushSessions()
	case len(s.Behind.Conflicts) > 0 && p.HandOver:
		msg := "Main moved: " + s.Behind.Upstream + " is " + strconv.Itoa(s.Behind.Commits) + " commits past your base and would conflict in " + strings.Join(s.Behind.Conflicts, ", ") + ". Rebase onto " + s.Behind.Upstream + ", resolve those, run the tests, then stop."
		d.Sessions.MainMovedTold(s.ID, false)
		d.sendToSession(context.Background(), s.ID, msg, true)
	}
}
