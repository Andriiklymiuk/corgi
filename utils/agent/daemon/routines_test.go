package daemon

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/bots"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestRoutinesRunOnTheClockAndReportToTheInbox(t *testing.T) {
	d := dynDaemon(t)
	d.loadWatchFiles()
	d.fixBusy = map[string]chan struct{}{"api": make(chan struct{}, 1)}
	spec := WatchSpec{Workspace: "api", Dir: t.TempDir(), Action: "notify",
		Routines: []config.Routine{{Name: "digest", Kind: "digest", Schedule: "daily 08:30"}, {Name: "off", Kind: "deps", Schedule: "daily 08:30", Off: true}}}
	d.Watches = []WatchSpec{spec}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	day := time.Date(2026, 9, 14, 0, 0, 0, 0, time.Local)
	d.runRoutines(ctx, day.Add(8*time.Hour))
	if len(d.watchState.Fixes.RecentFixes("api", 10)) != 0 {
		t.Fatal("not before 08:30")
	}
	d.runRoutines(ctx, day.Add(9*time.Hour))
	d.runs.Wait()
	runs := d.watchState.Fixes.RecentFixes("api", 10)
	if len(runs) != 1 || runs[0].Ref != "routine/digest" {
		t.Fatalf("the due one starts, the off one does not: %+v", runs)
	}
	d.releaseFix("api", "routine/digest")
	d.runRoutines(ctx, day.Add(10*time.Hour))
	d.runs.Wait()
	if len(d.watchState.Fixes.RecentFixes("api", 10)) != 1 {
		t.Fatal("once a day")
	}
	if last := loadRoutineState(d.Dir).get("api/digest"); !last.Equal(day.Add(9 * time.Hour)) {
		t.Fatalf("the start is remembered on disk: %v", last)
	}

	e, ok := RoutineEvent("api", config.Routine{Name: "digest", Kind: "digest"}, day)
	if !ok || !strings.Contains(e.Body, "morning digest") || e.Ref != "routine/digest" {
		t.Fatalf("event from the catalog: %+v", e)
	}
	d.routineReport(spec, e, "3 PRs green, 1 red\n- details…", nil)
	rows := watch.RecentEvents(d.Dir, 5)
	if len(rows) != 2 || rows[0].Kind != watch.KindRoutine || !strings.Contains(rows[0].Title, "digest — 3 PRs green, 1 red") || rows[0].Body != "" {
		t.Fatalf("inbox row: %+v", rows)
	}
	if !strings.Contains(rows[1].Title, "failed:") {
		t.Fatalf("a failed run reports too: %+v", rows[1])
	}
	if _, ok := RoutineEvent("api", config.Routine{Name: "x"}, day); ok {
		t.Fatal("no kind, no prompt, nothing to run")
	}
}

func TestARoutineRunsAsTheBotItNames(t *testing.T) {
	d := dynDaemon(t)
	d.loadWatchFiles()
	d.fixBusy = map[string]chan struct{}{"api": make(chan struct{}, 1)}
	var mu sync.Mutex
	var ran []string
	prev := claudeCommand
	claudeCommand = func(ctx context.Context, dir string, env []string, args ...string) *exec.Cmd {
		mu.Lock()
		defer mu.Unlock()
		ran = append(ran, strings.Join(args, " "))
		return exec.CommandContext(ctx, "echo", "Search by tag: the README promises it, the API has no route\n- api/routes.go:40")
	}
	t.Cleanup(func() { claudeCommand = prev })
	store, _ := bots.Load(bots.Path(d.Dir))
	store.Put(bots.Bot{Name: "proactive", Title: "Proactive", Workspace: "api", Soul: "Find the next thing.", Model: "opus"})
	store.Put(bots.Bot{Name: "elsewhere", Title: "Elsewhere", Workspace: "web", Soul: "Not here."})
	if err := bots.Save(bots.Path(d.Dir), store); err != nil {
		t.Fatal(err)
	}
	spec := WatchSpec{Workspace: "api", Dir: t.TempDir(), ConfigDir: t.TempDir(), Action: "notify", SkipPermissions: true,
		Routines: []config.Routine{
			{Name: "suggest", Kind: "suggest", Schedule: "weekly mon 09:30", Bot: "proactive"},
			{Name: "digest", Kind: "digest", Schedule: "weekly mon 09:30", Bot: "elsewhere"},
		}}
	d.Watches = []WatchSpec{spec}
	monday := time.Date(2026, 9, 14, 10, 0, 0, 0, time.Local)
	d.runRoutines(context.Background(), monday)
	d.runs.Wait()
	runs := d.watchState.Fixes.RecentFixes("api", 10)
	if len(runs) != 1 || runs[0].Bot != "proactive" || runs[0].Ref != "routine/suggest" || !strings.HasPrefix(runs[0].Note, "- api/routes.go") {
		t.Fatalf("filed under the bot: %+v", runs)
	}
	rows := watch.RecentEvents(d.Dir, 5)
	if len(rows) != 1 || rows[0].Kind != watch.KindRoutine || !strings.Contains(rows[0].Title, "suggest — Search by tag") {
		t.Fatalf("its report is a routine row: %+v", rows)
	}
	if !d.claimFix("api", "routine/suggest") {
		t.Fatal("the claim is given back when the bot is done")
	}
	d.releaseFix("api", "routine/suggest")

	d.runRoutines(context.Background(), monday)
	d.runs.Wait()
	mu.Lock()
	defer mu.Unlock()
	if len(ran) != 2 || !strings.Contains(ran[0], "--append-system-prompt Find the next thing.") || !strings.Contains(ran[0], "--model opus") {
		t.Fatalf("the first run is the bot's: %v", ran)
	}
	if strings.Contains(ran[1], "--append-system-prompt") {
		t.Fatalf("a bot from elsewhere does not lend its soul: %v", ran[1])
	}
}
