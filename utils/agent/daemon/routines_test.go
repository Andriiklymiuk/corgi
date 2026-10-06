package daemon

import (
	"andriiklymiuk/corgi/utils"
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
	prev := claudeCommand
	claudeCommand = func(ctx context.Context, dir string, env []string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "false")
	}
	t.Cleanup(func() { claudeCommand = prev })
	ctx := context.Background()

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
	d.routineReport(spec, e, "**3 PRs green, 1 red**\n- details…", nil, "")
	rows := watch.RecentEvents(d.Dir, 5)
	if len(rows) != 2 || rows[0].Kind != watch.KindRoutine || !strings.Contains(rows[0].Title, "digest - 3 PRs green, 1 red") || rows[0].Body != "" {
		t.Fatalf("inbox row: %+v", rows)
	}
	if !strings.Contains(rows[1].Title, "failed:") || !strings.Contains(rows[1].Title, "log: ") {
		t.Fatalf("a failed run reports too, with the log to read: %+v", rows[1])
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
	if len(runs) != 1 || runs[0].Bot != "proactive" || runs[0].Ref != "routine/suggest" || !strings.HasPrefix(runs[0].Note, "api/routes.go") {
		t.Fatalf("filed under the bot: %+v", runs)
	}
	rows := watch.RecentEvents(d.Dir, 5)
	if len(rows) != 1 || rows[0].Kind != watch.KindRoutine || !strings.Contains(rows[0].Title, "suggest - Search by tag") {
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

func TestRoutineWaitIsLoggedOncePerReason(t *testing.T) {
	d := &Daemon{}
	var buf strings.Builder
	utils.SetConsoleOverride(&buf)
	defer utils.ClearConsoleOverride()
	d.logRoutineWait("ws/digest", "digest", "day off")
	d.logRoutineWait("ws/digest", "digest", "day off")
	d.logRoutineWait("ws/digest", "digest", "quiet hours")
	if got := strings.Count(buf.String(), "waits"); got != 2 {
		t.Errorf("logged %d times, want 2 (once per reason):\n%s", got, buf.String())
	}
}

func TestARoutinePushLeadsWithTheHeadlineNotTheOutcomeLine(t *testing.T) {
	cases := map[string]string{
		"Outcome: nothing\n\n**Headline: Quiet day, 7 merged, nothing red.**\n- one": "Quiet day, 7 merged, nothing red.",
		"Outcome: nothing\n\n# Digest, 2026-10-02: quiet day, 1 merge":               "quiet day, 1 merge",
		"**3 PRs green, 1 red**\n- details…":                                         "3 PRs green, 1 red",
		"Outcome: nothing":                                                           "",
	}
	for out, want := range cases {
		if got := routineHeadline(out); got != want {
			t.Errorf("routineHeadline(%q) = %q, want %q", out, got, want)
		}
	}
}

func TestARoutineTheDaemonCutOffIsDueAgain(t *testing.T) {
	d := dynDaemon(t)
	d.loadWatchFiles()
	d.fixBusy = map[string]chan struct{}{"api": make(chan struct{}, 1)}
	d.Watches = []WatchSpec{{Workspace: "api", Dir: t.TempDir(), Action: "notify",
		Routines: []config.Routine{{Name: "digest", Kind: "digest", Schedule: "daily 08:30"}}}}
	started := make(chan struct{})
	prev := claudeCommand
	claudeCommand = func(ctx context.Context, dir string, env []string, args ...string) *exec.Cmd {
		close(started)
		return exec.CommandContext(ctx, "sleep", "10")
	}
	t.Cleanup(func() { claudeCommand = prev })
	ctx, cancel := context.WithCancel(context.Background())

	nine := time.Date(2026, 9, 14, 9, 0, 0, 0, time.Local)
	d.runRoutines(ctx, nine)
	<-started
	cancel()
	d.runs.Wait()
	if last := loadRoutineState(d.Dir).get("api/digest"); !last.IsZero() {
		t.Fatalf("a run the daemon stopped has to run again, not wait for tomorrow: %v", last)
	}
}

func TestAReadOnlyRoutineIsNotToldAboutPullRequests(t *testing.T) {
	spec := WatchSpec{Workspace: "api", Dir: t.TempDir(), Rules: watch.Rules{CI: true}}
	e, _ := RoutineEvent("api", config.Routine{Kind: "digest"}, time.Now())
	prompt := fixArgsWith(spec, e, "")[1]
	for _, never := range []string{"pull request body", "## Evidence", "review your own diff", "Do not wait for CI", "corgi agent handoff"} {
		if strings.Contains(prompt, never) {
			t.Errorf("a digest pushes nothing, so %q is noise: %s", never, prompt)
		}
	}
	if !strings.Contains(prompt, "`Outcome: nothing`") || !strings.Contains(prompt, "nothing after it") {
		t.Fatalf("it still opens with the outcome line and ends with the answer: %s", prompt)
	}
	custom, _ := RoutineEvent("api", config.Routine{Name: "tidy", Prompt: "Fix the lint warnings and open a PR."}, time.Now())
	if !strings.Contains(fixArgsWith(spec, custom, "")[1], "## Evidence") {
		t.Fatal("a routine that writes code keeps the pull request rules")
	}
}
