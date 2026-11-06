package daemon

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/events"
	"andriiklymiuk/corgi/utils/agent/watch"
)

const (
	unreadJudging = -1
	unreadNoTree  = -2
)

// mayMergeUnread is level 5's gate: a pull request a run opened, green, whose
// diff stays away from what breaks badly, whose run ended clean and whose
// exact commit satisfies the stories the builder could not read. It marks the
// draft ready and says yes. Anything short of that waits for a person, who
// hears once why. A pull request a person opened never comes through here.
func (d *Daemon) mayMergeUnread(ctx context.Context, spec WatchSpec, ref, link string, st watch.PullStatus) bool {
	if !spec.MergeUnread || !st.Mine || st.Checks != "passing" || (st.State != "open" && st.State != "draft") {
		return false
	}
	if d.watchState == nil || d.PullChanges == nil || d.JudgeScenarios == nil || d.ReadyPull == nil {
		return false
	}
	now := time.Now()
	log := d.fixLog()
	started, ok := log.RunThatStartedPull(spec.Workspace, link)
	if !ok || d.fixActiveFor(spec.Workspace, started.Ref) || d.peerLeads(spec) != "" || d.deferral(spec, now) != "" {
		return false
	}
	if st.Review == "changes" {
		d.holdUnread(spec, ref, link, "a reviewer asked for changes")
		return false
	}
	if last, ok := log.LastRunOnPull(spec.Workspace, link); ok {
		if why := watch.UnreadRunRefusal(last); why != "" {
			d.holdUnread(spec, ref, link, why)
			return false
		}
	}
	changes, err := d.PullChanges(ctx, spec.Workspace, link)
	if err != nil {
		utils.Infof("agent: reading %s: %v\n", ref, err)
		return false
	}
	if why := watch.UnreadRefusal(changes); why != "" {
		d.holdUnread(spec, ref, link, why)
		return false
	}
	share := d.scenarioShare(spec, started, link, changes.Head)
	switch {
	case share == unreadJudging:
		return false
	case share == unreadNoTree:
		d.holdUnread(spec, ref, link, "no checkout at its commit for the scenarios to judge")
		return false
	case share < watch.UnreadMinScenarios:
		d.holdUnread(spec, ref, link, fmt.Sprintf("the hidden scenarios scored %d%%", share))
		return false
	}
	if err := d.ReadyPull(ctx, spec.Workspace, link); err != nil {
		utils.Infof("agent: ready %s: %v\n", ref, err)
		d.holdUnread(spec, ref, link, "could not mark it ready")
		return false
	}
	return true
}

func (d *Daemon) holdUnread(spec WatchSpec, ref, link, why string) {
	now := time.Now()
	if !d.rangOnce("unread|"+link+"|"+why, now) {
		return
	}
	body := ref + " waits for you: " + why
	utils.Infof("agent: %s\n", body)
	if d.Events != nil {
		d.Events.Append(spec.Workspace, events.Event{At: now.UTC(), Kind: "held", Reason: body, URL: link})
	}
	go d.notifyAttentionAt(notifyTitlePrefix+spec.Workspace, body, spec.Workspace, link)
}

// scenarioShare judges a commit once, in the background: a judge takes minutes
// and tokens, and the answer holds for as long as the commit does.
func (d *Daemon) scenarioShare(spec WatchSpec, run watch.FixRecord, link, head string) int {
	key := link + "|" + head
	if v, loaded := d.unreadJudged.LoadOrStore(key, unreadJudging); loaded {
		return v.(int)
	}
	go func() {
		dir := d.checkoutAt(spec, run.Branch, head)
		if dir == "" {
			d.unreadJudged.Store(key, unreadNoTree)
			return
		}
		share, n, err := d.JudgeScenarios(context.Background(), spec.Workspace, dir, watch.PullRef(link))
		if err != nil || n == 0 {
			utils.Infof("agent: judging %s: %d scenario(s): %v\n", link, n, err)
			share = 0
		}
		d.unreadJudged.Store(key, share)
		if d.watchers != nil {
			for _, w := range d.watchers {
				w.Nudge()
			}
		}
	}()
	return unreadJudging
}

// checkoutAt finds the run's worktree that holds exactly this commit, so what
// is judged is what would merge.
func (d *Daemon) checkoutAt(spec WatchSpec, branch, head string) string {
	if d.Isolate == nil || branch == "" || head == "" {
		return ""
	}
	trees, err := d.Isolate(spec.Dir, branch)
	if err != nil {
		return ""
	}
	for _, tree := range trees {
		out, err := exec.Command("git", "-C", tree, "rev-parse", "HEAD").Output()
		if err == nil && strings.TrimSpace(string(out)) == head {
			return tree
		}
	}
	return ""
}
