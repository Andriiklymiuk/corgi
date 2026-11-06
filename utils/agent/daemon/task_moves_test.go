package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/peers"
	"andriiklymiuk/corgi/utils/agent/watch"
)

func withPeer(t *testing.T, d *Daemon, p peers.Peer) {
	t.Helper()
	if err := peers.Update(peers.Path(d.Dir), func(s *peers.Store) { s.Peers = append(s.Peers, p) }); err != nil {
		t.Fatal(err)
	}
}

// fixingDaemon is a machine that can run fixes: its agent is installed.
func fixingDaemon(t *testing.T) *Daemon {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	d := testDaemon(t)
	d.Watches = []WatchSpec{{Workspace: "w", Action: "fix", AgentDir: d.Dir}}
	return d
}

func waitingCopy(t *testing.T, d *Daemon, peer string, at time.Time) watch.Task {
	t.Helper()
	there := watch.LoadTasks(t.TempDir())
	task, _ := there.Add("fix it", "body", "w", "you", at)
	task, _ = there.AskRun(task.Ref(), at)
	if err := watch.SaveTaskCopies(d.Dir, peer, watch.TaskCopies{Tasks: []watch.Task{task}, At: at}); err != nil {
		t.Fatal(err)
	}
	return task
}

func TestAQuietPeersWaitingTaskComesHereOnce(t *testing.T) {
	d := fixingDaemon(t)
	now := time.Now()
	withPeer(t, d, peers.Peer{Name: "laptop", SeenAt: now.Add(-time.Hour)})
	there := waitingCopy(t, d, "laptop", now.Add(-time.Hour))
	d.takeOverTasks(now)
	d.takeOverTasks(now)
	tasks := watch.LoadTasks(d.Dir).Tasks
	if len(tasks) != 1 {
		t.Fatalf("%d tasks", len(tasks))
	}
	if !tasks[0].WantsRun() || tasks[0].Origin == nil || tasks[0].Origin.ID != there.ID || tasks[0].Origin.Peer != "laptop" {
		t.Fatalf("%+v", tasks[0])
	}
}

func TestATaskStaysWhereItIsUnlessThePeerHasBeenQuietAWhile(t *testing.T) {
	now := time.Now()
	for name, seen := range map[string]time.Time{"alive": now, "a short nap": now.Add(-5 * time.Minute), "never heard": {}} {
		d := fixingDaemon(t)
		withPeer(t, d, peers.Peer{Name: "laptop", SeenAt: seen})
		waitingCopy(t, d, "laptop", now)
		d.takeOverTasks(now)
		if n := len(watch.LoadTasks(d.Dir).Tasks); n != 0 {
			t.Errorf("%s: took %d", name, n)
		}
	}
}

func TestOldCopiesAreLeftAlone(t *testing.T) {
	d := fixingDaemon(t)
	now := time.Now()
	withPeer(t, d, peers.Peer{Name: "laptop", SeenAt: now.Add(-5 * 24 * time.Hour)})
	waitingCopy(t, d, "laptop", now.Add(-5*24*time.Hour))
	d.takeOverTasks(now)
	if n := len(watch.LoadTasks(d.Dir).Tasks); n != 0 {
		t.Fatalf("took %d", n)
	}
}

func TestATaskForAWorkspaceThatDoesNotRunHereStays(t *testing.T) {
	d := testDaemon(t)
	d.Watches = []WatchSpec{{Workspace: "w", Action: "notify", AgentDir: d.Dir}}
	now := time.Now()
	withPeer(t, d, peers.Peer{Name: "laptop", SeenAt: now.Add(-time.Hour)})
	waitingCopy(t, d, "laptop", now)
	d.takeOverTasks(now)
	if n := len(watch.LoadTasks(d.Dir).Tasks); n != 0 {
		t.Fatalf("took %d", n)
	}
}

func TestOnlyOneMachineTakesATask(t *testing.T) {
	d := fixingDaemon(t)
	now := time.Now()
	me := strings.ToLower(peers.Me())
	store := &peers.Store{Peers: []peers.Peer{{Name: "0-first", SeenAt: now}}}
	if err := watch.SaveTaskCopies(d.Dir, "0-first", watch.TaskCopies{Fixes: []string{"w"}, At: now}); err != nil {
		t.Fatal(err)
	}
	if me > "0-first" && d.shouldTakeTask(store, "w", now) {
		t.Fatal("the first by name takes it")
	}
	store.Peers[0].Unwell = "limit"
	if !d.shouldTakeTask(store, "w", now) {
		t.Fatal("a peer that cannot work does not count")
	}
	store.Peers[0].Unwell = ""
	store.Peers[0].SeenAt = now.Add(-time.Hour)
	if !d.shouldTakeTask(store, "w", now) {
		t.Fatal("a quiet peer does not count")
	}
	if err := watch.SaveTaskCopies(d.Dir, "0-first", watch.TaskCopies{Fixes: []string{"other"}, At: now}); err != nil {
		t.Fatal(err)
	}
	store.Peers[0].SeenAt = now
	if !d.shouldTakeTask(store, "w", now) {
		t.Fatal("a peer without the workspace does not count")
	}
}

func TestAMachineThatWokeWaitsToHearItsPeersBeforeStartingTasks(t *testing.T) {
	d := fixingDaemon(t)
	now := time.Now()
	if !d.heardPeersLately(now) {
		t.Fatal("alone it never waits")
	}
	withPeer(t, d, peers.Peer{Name: "laptop", SeenAt: now.Add(-time.Hour)})
	if d.heardPeersLately(now) {
		t.Fatal("no round of pulses yet")
	}
	d.peersHeardAt.Store(now.UnixMilli())
	if !d.heardPeersLately(now.Add(time.Minute)) {
		t.Fatal("heard a minute ago")
	}
	if d.heardPeersLately(now.Add(10 * time.Minute)) {
		t.Fatal("the lid was closed since")
	}
}

func TestATaskAPeerTookStopsHereOnReturn(t *testing.T) {
	d := fixingDaemon(t)
	now := time.Now()
	tasks := watch.LoadTasks(d.Dir)
	a, _ := tasks.Add("fix it", "", "w", "you", now)
	a, _ = tasks.AskRun(a.Ref(), now)
	d.markTasksTaken("build-1", []watch.TakenTask{{ID: a.ID, CreatedAt: a.CreatedAt, Ref: "TASK-9"}}, now)
	got, _ := watch.LoadTasks(d.Dir).Find(a.Ref())
	if got.WantsRun() || got.State != "Canceled" || got.MovedTo != "build-1 as TASK-9" {
		t.Fatalf("%+v", got)
	}
}

func TestTheBoardSweepClosesRowsAndTasksWithoutAPlanTask(t *testing.T) {
	d := fixingDaemon(t)
	now := time.Now()
	link := "https://github.com/a/b/pull/7"
	log := watch.LoadFixLog(d.Dir)
	tasks := watch.LoadTasks(d.Dir)
	task, _ := tasks.Add("fix it", "", "w", "you", now.Add(-time.Hour))
	task, _ = tasks.Started(task.Ref(), now.Add(-time.Hour))
	log.StartFor(task.Event(), now.Add(-time.Hour))
	log.Finish(task.Key(), []string{link}, "", "", now.Add(-time.Hour))
	log.SetLeft(task.Key(), watch.LeftFor("## Left for you\n- Decide something.\n"))
	if err := watch.LoadPullLog(d.Dir).Set("a/b#7", watch.PullStatus{State: "merged", At: now}); err != nil {
		t.Fatal(err)
	}
	d.sweepBoard(now)
	if records := watch.LoadFixLog(d.Dir).Records(); records[len(records)-1].HasOpenLeft() {
		t.Fatal("the row closes")
	}
	got, _ := watch.LoadTasks(d.Dir).Find(task.Ref())
	if got.State != "Done" {
		t.Fatalf("%+v", got)
	}
}

type askAboutPull struct {
	asked atomic.Int32
	state string
}

func (a *askAboutPull) Name() string { return "fake" }
func (a *askAboutPull) Poll(_ context.Context, c watch.Cursor) ([]watch.Event, watch.Cursor, error) {
	return nil, c, nil
}
func (a *askAboutPull) PullStatus(context.Context, string) (watch.PullStatus, bool) {
	a.asked.Add(1)
	if a.state == "" {
		return watch.PullStatus{}, false
	}
	return watch.PullStatus{State: a.state, Checks: "passing", At: time.Now()}, true
}

func TestAnOpenPullRequestNobodyMentionsIsStillAskedAboutAndOldNewsStaysQuiet(t *testing.T) {
	d := fixingDaemon(t)
	src := &askAboutPull{state: "merged"}
	spec := d.Watches[0]
	spec.Sources = []watch.Source{src}
	now := time.Now()
	pulls := watch.LoadPullLog(d.Dir)
	_ = pulls.Set("a/b#9", watch.PullStatus{State: "open", Workspace: "w", Link: "https://github.com/a/b/pull/9", At: now.Add(-20 * 24 * time.Hour)})
	d.refreshPulls(context.Background(), spec)
	got, _ := watch.LoadPullLog(d.Dir).Get("a/b#9")
	if got.State != "merged" || src.asked.Load() != 1 {
		t.Fatalf("%+v asked %d", got, src.asked.Load())
	}
	d.refreshPulls(context.Background(), spec)
	if src.asked.Load() != 1 {
		t.Fatal("merged stays merged, no need to ask")
	}
}

func TestAPullRequestThatCannotAnswerDoesNotHoldUpTheRest(t *testing.T) {
	d := fixingDaemon(t)
	src := &askAboutPull{}
	spec := d.Watches[0]
	spec.Sources = []watch.Source{src}
	_ = watch.LoadPullLog(d.Dir).Set("a/b#9", watch.PullStatus{State: "open", Workspace: "w", At: time.Now().Add(-48 * time.Hour)})
	d.refreshPulls(context.Background(), spec)
	d.refreshPulls(context.Background(), spec)
	if src.asked.Load() != 1 {
		t.Fatalf("asked %d", src.asked.Load())
	}
}

func TestSyncTellsAPeerWhatWaitsAndHearsWhatItTook(t *testing.T) {
	d := fixingDaemon(t)
	now := time.Now()
	tasks := watch.LoadTasks(d.Dir)
	a, _ := tasks.Add("fix it", "", "w", "you", now)
	a, _ = tasks.AskRun(a.Ref(), now)
	var got watch.TaskCopies
	d.syncTasksVia("build-1", func(in, out any) error {
		got = in.(watch.TaskCopies)
		data, _ := json.Marshal(map[string]any{"taken": []watch.TakenTask{{ID: a.ID, CreatedAt: a.CreatedAt, Ref: "TASK-5"}}})
		return json.Unmarshal(data, out)
	})
	if len(got.Tasks) != 1 || got.Tasks[0].ID != a.ID || got.Fixes[0] != "w" {
		t.Fatalf("%+v", got)
	}
	back, _ := watch.LoadTasks(d.Dir).Find(a.Ref())
	if back.MovedTo != "build-1 as TASK-5" {
		t.Fatalf("%+v", back)
	}
}
