package daemon

import (
	"context"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/watch"
)

var tasksMu sync.Mutex

func (d *Daemon) advanceWork(ctx context.Context) {
	d.advancePlans(ctx)
	d.advanceTasks(ctx)
}

// advanceTasks starts what the board asks for: a task added to run, and a task a
// person commented on since its last run. Only where the workspace runs fixes.
func (d *Daemon) advanceTasks(ctx context.Context) {
	if d.watchState == nil || len(d.Watches) == 0 {
		return
	}
	tasksMu.Lock()
	defer tasksMu.Unlock()
	now := time.Now()
	if !d.heardPeersLately(now) {
		return
	}
	d.takeOverTasks(now)
	tasks := watch.LoadTasks(d.Dir)
	for _, t := range tasks.Tasks {
		if !t.WantsRun() {
			continue
		}
		spec, ok := d.watchSpecFor(t.Workspace)
		if !ok || spec.Action != "fix" || d.fixActiveFor(t.Workspace, t.Ref()) {
			continue
		}
		if d.fixesActiveIn(t.Workspace) >= taskSlots(spec) {
			continue
		}
		if reason := d.deferral(spec, now); reason != "" {
			if d.taskWaitSaid == nil {
				d.taskWaitSaid = map[string]string{}
			}
			if d.taskWaitSaid[t.Key()] != reason {
				d.taskWaitSaid[t.Key()] = reason
				utils.Infof("agent: %s waits: %s\n", t.Ref(), reason)
			}
			continue
		}
		delete(d.taskWaitSaid, t.Key())
		if !d.claimFix(t.Workspace, t.Ref()) {
			continue
		}
		e := t.Event()
		_, _ = tasks.Started(t.Ref(), now)
		d.watchState.Fixes.StartFor(e, now)
		d.spawnFix(ctx, spec, e)
		utils.Infof("agent: started %s - %s\n", t.Ref(), t.Title)
	}
}

// One checkout takes one run; worktrees take a few.
func taskSlots(spec WatchSpec) int {
	switch {
	case !spec.Isolate:
		return 1
	case spec.Slots > 0:
		return spec.Slots
	}
	return 3
}

func (d *Daemon) fixesActiveIn(workspace string) int {
	d.attentionMu.Lock()
	defer d.attentionMu.Unlock()
	n := 0
	for key, on := range d.fixActive {
		if on && strings.HasPrefix(key, workspace+"/") {
			n++
		}
	}
	return n
}

// replyOnTask puts what a run came to on the task's thread.
func (d *Daemon) replyOnTask(e watch.Event, text string) {
	if e.Kind != watch.KindTask || strings.TrimSpace(text) == "" {
		return
	}
	tasksMu.Lock()
	defer tasksMu.Unlock()
	_, _ = watch.LoadTasks(d.Dir).Comment(e.Ref, watch.TaskAgent, text, time.Now())
}

// sweepBoard closes what is over: rows left for a person and tasks whose pull
// requests are merged or closed, and tasks whose run ended.
func (d *Daemon) sweepBoard(now time.Time) {
	log := d.fixLog()
	pulls := watch.LoadPullLog(d.Dir).All()
	if n := log.SweepLeft(pulls, now); n > 0 {
		utils.Infof("agent: closed %d row(s) left for you: their pull requests are over\n", n)
	}
	inPlan := map[int]bool{}
	for _, p := range watch.LoadPlans(d.Dir).All() {
		for _, id := range p.TaskIDs() {
			inPlan[id] = true
		}
	}
	tasksMu.Lock()
	defer tasksMu.Unlock()
	moved := watch.LoadTasks(d.Dir).Settle(log.Records(), pulls, func(t watch.Task) bool {
		return inPlan[t.ID] || d.fixActiveFor(t.Workspace, t.Ref())
	}, now)
	for _, line := range moved {
		utils.Infof("agent: %s\n", line)
	}
}
