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
		if spec, ok := d.watchSpecFor(p.Workspace); ok {
			d.advancePlan(ctx, spec, plans, tasks, p, now)
		}
	}
}

func (d *Daemon) advancePlan(ctx context.Context, spec WatchSpec, plans *watch.PlanLog, tasks *watch.TaskLog, p watch.Plan, now time.Time) {
	pr := p.Progress(tasks)
	if pr.Over() {
		_, _ = plans.SetState(p.ID, watch.PlanDone, now)
		utils.Infof("agent: plan %s is done: %d done, %d in review, %d canceled\n", p.Ref(), pr.Done, pr.Review, pr.Canceled)
		go d.notifyAttentionAt(notifyTitlePrefix+p.Workspace,
			fmt.Sprintf("plan %s finished: %s - %d in review, %d done", p.Ref(), clipGoal(p.Goal), pr.Review, pr.Done), p.Workspace, "")
		return
	}
	busy := 0
	for _, id := range p.TaskIDs() {
		if d.fixActiveFor(p.Workspace, fmt.Sprintf("TASK-%d", id)) {
			busy++
		}
	}
	if p.Retries > 0 {
		busy += d.retryFailedTasks(ctx, spec, p, tasks, busy, now)
	}
	for _, t := range pr.Ready {
		if busy >= p.Slots {
			return
		}
		started, stop := d.startPlanTask(ctx, spec, p, tasks, t, now)
		if stop {
			return
		}
		if started {
			busy++
		}
	}
}

func (d *Daemon) startPlanTask(ctx context.Context, spec WatchSpec, p watch.Plan, tasks *watch.TaskLog, t watch.Task, now time.Time) (taken, stop bool) {
	if d.planTaskTried(p.Workspace, t) {
		return false, false
	}
	if reason := d.deferral(spec, now); reason != "" {
		utils.Infof("agent: plan %s: %s waits: %s\n", p.Ref(), t.Ref(), reason)
		return false, true
	}
	if !d.claimFix(p.Workspace, t.Ref()) {
		return true, false
	}
	e := t.Event()
	e.Body = planTaskBody(p, t)
	_, _ = tasks.Move(t.Ref(), "Doing", now)
	d.watchState.Fixes.StartFor(e, now)
	d.spawnFix(ctx, spec, e)
	utils.Infof("agent: plan %s: started %s - %s\n", p.Ref(), t.Ref(), t.Title)
	return true, false
}

// retryFailedTasks starts again the plan's tasks whose last run failed, up to
// the plan's cap, telling the new run why the last one stopped.
func (d *Daemon) retryFailedTasks(ctx context.Context, spec WatchSpec, p watch.Plan, tasks *watch.TaskLog, busy int, now time.Time) int {
	started := 0
	records := d.watchState.Fixes.RecentFixes(p.Workspace, 200)
	for _, id := range p.TaskIDs() {
		if busy+started >= p.Slots {
			break
		}
		t, ok := tasks.Find(fmt.Sprintf("task:%d", id))
		if !ok || t.State != "Doing" || d.fixActiveFor(p.Workspace, t.Ref()) {
			continue
		}
		why, due := retryDue(records, t.Key(), p.Retries)
		if !due || d.deferral(spec, now) != "" || !d.claimFix(p.Workspace, t.Ref()) {
			continue
		}
		e := t.Event()
		e.Body = planTaskBody(p, t) + "\n\nAn earlier run of this task failed: " + why + "\nFind out why before you repeat it. If it left a branch, build on it."
		d.watchState.Fixes.StartFor(e, now)
		d.spawnFix(ctx, spec, e)
		utils.Infof("agent: plan %s: tried %s again - %s\n", p.Ref(), t.Ref(), why)
		started++
	}
	return started
}

// retryDue says whether a task's last finished run failed and it has tries left.
func retryDue(records []watch.FixRecord, key string, retries int) (string, bool) {
	attempts := 0
	var last watch.FixRecord
	for _, f := range records {
		if f.Key != key || f.FinishedAt.IsZero() || strings.HasPrefix(f.Error, "not started") {
			continue
		}
		attempts++
		if f.FinishedAt.After(last.FinishedAt) {
			last = f
		}
	}
	if attempts == 0 || last.Error == "" || attempts > retries {
		return "", false
	}
	why := last.Error
	if last.Failure != "" {
		why = last.Failure
	}
	return why, true
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
