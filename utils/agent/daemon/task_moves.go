package daemon

import (
	"context"
	"slices"
	"sort"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/peers"
	"andriiklymiuk/corgi/utils/agent/watch"
)

const (
	// A closed lid for a coffee is no reason to move work; a tracker hands over
	// sooner because its events wait for the next machine anyway.
	taskTakeOverAfter = 10 * time.Minute
	// Copies older than this describe a machine gone for days: someone has
	// decided what to do with those tasks by now.
	taskCopiesExpire = 3 * 24 * time.Hour
	// A machine that just woke has not heard yet whether a peer took its tasks.
	peersHeardWithin = 150 * time.Second
	tasksSentAtOnce  = 50
)

// syncTasksWith gives a peer a copy of what waits here, and learns which of
// this machine's tasks it holds.
func (d *Daemon) syncTasksWith(ctx context.Context, p peers.Peer) {
	d.syncTasksVia(p.Name, func(in, out any) error {
		return peers.NewClient(p).Call(ctx, "/launch/peers/tasks", in, out)
	})
}

func (d *Daemon) syncTasksVia(peer string, call func(in, out any) error) {
	tasksMu.Lock()
	wanted := watch.LoadTasks(d.Dir).Wanted()
	tasksMu.Unlock()
	if len(wanted) > tasksSentAtOnce {
		wanted = wanted[:tasksSentAtOnce]
	}
	var out struct {
		Taken []watch.TakenTask `json:"taken"`
	}
	in := watch.TaskCopies{Fixes: d.workspacesThatFix(), Tasks: wanted}
	if err := call(in, &out); err != nil {
		return
	}
	d.markTasksTaken(peer, out.Taken, time.Now())
}

func (d *Daemon) markTasksTaken(peer string, taken []watch.TakenTask, now time.Time) {
	if len(taken) == 0 {
		return
	}
	tasksMu.Lock()
	defer tasksMu.Unlock()
	tasks := watch.LoadTasks(d.Dir)
	for _, t := range taken {
		if moved, ok := tasks.MoveAway(t.ID, t.CreatedAt, peer+" as "+t.Ref, now); ok {
			utils.Infof("agent: %s moved to %s while this machine was away\n", moved.Ref(), peer)
		}
	}
}

func (d *Daemon) workspacesThatFix() []string {
	out := []string{}
	for _, spec := range d.Watches {
		if spec.Action == "fix" {
			out = append(out, spec.Workspace)
		}
	}
	return out
}

// heardPeersLately holds tasks back until a machine that just woke has heard
// its peers: one of them may have taken what waits here. Alone, it never waits.
func (d *Daemon) heardPeersLately(now time.Time) bool {
	store, err := peers.Load(peers.Path(d.Dir))
	if err != nil || len(store.Peers) == 0 {
		return true
	}
	return now.Sub(time.UnixMilli(d.peersHeardAt.Load())) < peersHeardWithin
}

// takeOverTasks brings a quiet peer's waiting tasks here, when this machine
// has the workspace and is the one to do it. The peer learns on its return.
func (d *Daemon) takeOverTasks(now time.Time) {
	store, err := peers.Load(peers.Path(d.Dir))
	if err != nil || len(store.Peers) == 0 {
		return
	}
	tasks := watch.LoadTasks(d.Dir)
	for _, p := range store.Peers {
		if p.SeenAt.IsZero() || now.Sub(p.SeenAt) < taskTakeOverAfter {
			continue
		}
		copies, ok := watch.LoadTaskCopies(d.Dir, p.Name)
		if !ok || now.Sub(copies.At) > taskCopiesExpire {
			continue
		}
		for _, t := range copies.Tasks {
			if !t.WantsRun() || tasks.HasOrigin(p.Name, t.ID) || !d.shouldTakeTask(store, t.Workspace, now) {
				continue
			}
			mine, err := tasks.AddCopy(t, p.Name, now)
			if err != nil {
				utils.Infof("agent: could not take %s from %s: %v\n", t.Ref(), p.Name, err)
				continue
			}
			line := "took " + t.Ref() + " from " + p.Name + " as " + mine.Ref() + ": it has been quiet for " + now.Sub(p.SeenAt).Round(time.Minute).String()
			utils.Infof("agent: %s\n", line)
			go d.notifyAttention("corgi agent", line, t.Workspace)
		}
	}
}

// shouldTakeTask picks one machine among those that can run the workspace:
// the first by name, so two never start the same task.
func (d *Daemon) shouldTakeTask(store *peers.Store, workspace string, now time.Time) bool {
	spec, ok := d.watchSpecFor(workspace)
	if !ok || spec.Action != "fix" {
		return false
	}
	var log *watch.FixLog
	if d.watchState != nil {
		log = d.watchState.Fixes
	}
	me := strings.ToLower(peers.Me())
	var able []string
	if laptopUnwell(spec, log, now) == "" {
		able = append(able, me)
	}
	for _, q := range store.Peers {
		if q.Alive(now) && q.Unwell == "" && d.peerFixes(q.Name, workspace) {
			able = append(able, strings.ToLower(q.Name))
		}
	}
	sort.Strings(able)
	return len(able) > 0 && able[0] == me
}

func (d *Daemon) peerFixes(peer, workspace string) bool {
	copies, ok := watch.LoadTaskCopies(d.Dir, peer)
	return ok && slices.Contains(copies.Fixes, workspace)
}
