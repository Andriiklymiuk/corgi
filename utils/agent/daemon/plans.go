package daemon

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/watch"
)

var plansMu sync.Mutex

func (d *Daemon) advancePlans(ctx context.Context) {
	if d.watchState == nil || len(d.Watches) == 0 {
		return
	}
	plansMu.Lock()
	defer plansMu.Unlock()
	plans := watch.LoadPlans(d.Dir)
	running := plans.Running()
	if len(running) == 0 {
		return
	}
	tasks := watch.LoadTasks(d.Dir)
	now := time.Now()
	for _, p := range running {
		spec, ok := d.watchSpecFor(p.Workspace)
		if !ok {
			continue
		}
		pr := p.Progress(tasks)
		if pr.Over() {
			_, _ = plans.SetState(p.ID, watch.PlanDone, now)
			utils.Infof("agent: plan %s is done: %d done, %d in review, %d canceled\n", p.Ref(), pr.Done, pr.Review, pr.Canceled)
			go d.notifyAttentionAt(notifyTitlePrefix+p.Workspace,
				fmt.Sprintf("plan %s finished: %s — %d in review, %d done", p.Ref(), clipGoal(p.Goal), pr.Review, pr.Done), p.Workspace, "")
			continue
		}
		busy := 0
		for _, id := range p.TaskIDs() {
			if d.fixActiveFor(p.Workspace, fmt.Sprintf("TASK-%d", id)) {
				busy++
			}
		}
		for _, t := range pr.Ready {
			if busy >= p.Slots {
				break
			}
			if d.planTaskTried(p.Workspace, t) {
				continue
			}
			if reason := fixDeferral(spec, d.watchState.Fixes, now); reason != "" {
				utils.Infof("agent: plan %s: %s waits: %s\n", p.Ref(), t.Ref(), reason)
				break
			}
			if !d.claimFix(p.Workspace, t.Ref()) {
				busy++
				continue
			}
			e := t.Event()
			e.Body = planTaskBody(p, t)
			_, _ = tasks.Move(t.Ref(), "Doing", now)
			d.watchState.Fixes.StartFor(e, now)
			d.spawnFix(ctx, spec, e)
			utils.Infof("agent: plan %s: started %s — %s\n", p.Ref(), t.Ref(), t.Title)
			busy++
		}
	}
}

func (d *Daemon) planTaskTried(workspace string, t watch.Task) bool {
	for _, f := range d.watchState.Fixes.RecentFixes(workspace, 200) {
		if f.Key == t.Key() && !f.FinishedAt.IsZero() && !strings.HasPrefix(f.Error, "not started") {
			return true
		}
	}
	return false
}

func planTaskBody(p watch.Plan, t watch.Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "This task is part of plan %s: %s\n", p.Ref(), p.Goal)
	if p.Summary != "" {
		fmt.Fprintf(&b, "The plan in short: %s\n", p.Summary)
	}
	b.WriteString("Other tasks of the plan are run by other sessions in their own worktrees; do only this one, and do not touch what another task owns.\n\n")
	b.WriteString(t.Body)
	return b.String()
}

func (d *Daemon) watchSpecFor(workspace string) (WatchSpec, bool) {
	for _, spec := range d.Watches {
		if spec.Workspace == workspace {
			spec.AgentDir = d.Dir
			return spec, true
		}
	}
	return WatchSpec{}, false
}

func clipGoal(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) > 80 {
		return s[:77] + "…"
	}
	return s
}
