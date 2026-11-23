package daemon

import (
	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/push"
	"context"
	"errors"
	"os"
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

func TestAReadOnlyRoutineMayRunItsReadsWithoutTheBypass(t *testing.T) {
	spec := WatchSpec{Workspace: "api", Dir: t.TempDir()}
	retro, _ := RoutineEvent("api", config.Routine{Kind: "retro"}, time.Now())
	p := fixPrint(spec, retro, "")
	if len(p.AllowedTools) == 0 || p.SkipPermissions {
		t.Fatalf("a retro gets its read commands, not the bypass: %+v", p.AllowedTools)
	}
	joined := strings.Join(p.AllowedTools, " ")
	for _, want := range []string{"Bash(corgi agent history:*)", "Bash(corgi agent task add:*)", "Bash(git log:*)"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %s", want)
		}
	}
	for _, never := range []string{"git push", "git commit", "gh pr merge", "gh api", "Bash(corgi:*)", "Bash(git:*)"} {
		if strings.Contains(joined, never) {
			t.Errorf("a read-only routine may not %s", never)
		}
	}
	custom, _ := RoutineEvent("api", config.Routine{Name: "tidy", Prompt: "Fix the lint warnings and open a PR."}, time.Now())
	if len(fixPrint(spec, custom, "").AllowedTools) != 0 {
		t.Fatal("a routine that writes code gets no list: it needs the workspace's own permission choice")
	}
	spec.SkipPermissions = true
	if p := fixPrint(spec, retro, ""); len(p.AllowedTools) != 0 || !p.SkipPermissions {
		t.Fatal("with the bypass on, the list is not needed")
	}
}

func TestAPushNamesTheRoutineOnce(t *testing.T) {
	cases := map[string]string{
		"Retro: 96 runs in 7 days, 3 tasks filed.": "96 runs in 7 days, 3 tasks filed.",
		"retro - 96 runs":                          "96 runs",
		"Retrospective of 96 runs":                 "Retrospective of 96 runs",
		"Retro:":                                   "Retro:",
		"Three tasks filed.":                       "Three tasks filed.",
	}
	for in, want := range cases {
		if got := withoutName(in, "retro"); got != want {
			t.Errorf("withoutName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestARoutineReportOpensItsRunNotAnEmptyChat(t *testing.T) {
	d := dynDaemon(t)
	d.loadWatchFiles()
	pushed := make(chan push.Message, 4)
	d.Push = func(m push.Message) { pushed <- m }
	var opened []string
	var mu sync.Mutex
	d.NotifyWithLink = func(_, _, link string) {
		mu.Lock()
		opened = append(opened, link)
		mu.Unlock()
	}
	d.LinkFor = func(string) string { return "https://claude.ai/code?environment=env_x" }
	d.RunLink = func(key string) string { return "https://corgi.example/run/" + key }
	spec := WatchSpec{Workspace: "corgi"}
	began := time.Date(2026, 11, 16, 10, 4, 0, 0, time.UTC)
	e, _ := RoutineEvent("corgi", config.Routine{Name: "retro", Kind: "retro"}, began)
	tasks := watch.LoadTasks(d.Dir)
	_, _ = tasks.Add("Retro: an older one", "", "corgi", "cli", began.Add(-time.Hour))
	_, _ = tasks.Add("Retro: a build nobody asked for", "", "corgi", "cli", began.Add(time.Minute))
	_, _ = tasks.Add("Somebody else's", "", "web", "cli", began.Add(time.Minute))
	_, _ = tasks.Add("Added by hand meanwhile", "", "corgi", "cli", began.Add(time.Minute))
	d.routineReport(spec, e, "Outcome: nothing\nretro: 20 runs, 1 task filed\nTASK-2 is filed, TASK-1 and TASK-3 were there", nil, "/tmp/runs/routine_corgi_retro.log")
	m := <-pushed
	if m.Data["run"] != e.Key || m.Data["url"] != "https://corgi.example/run/"+e.Key {
		t.Fatalf("push data: %v", m.Data)
	}
	if m.Body != "retro: 20 runs, 1 task filed\nTASK-2 a build nobody asked for" {
		t.Fatalf("the post names what was filed, and only that: %q", m.Body)
	}
	d.routineReport(spec, e, "", errors.New("exit 1"), "/tmp/runs/routine_corgi_retro.log")
	if m := <-pushed; strings.Contains(m.Body, "/tmp/") || m.Data["url"] == "" {
		t.Fatalf("a link to the run, not a path on the Mac: %q %v", m.Body, m.Data)
	}
	d.routineReport(spec, e, "1 PR opened", nil, "/tmp/x.log", "https://github.com/acme/api/pull/9")
	if m := <-pushed; m.Data["url"] != "https://github.com/acme/api/pull/9" || m.Data["run"] != e.Key {
		t.Fatalf("a pull request wins: %v", m.Data)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, l := range opened {
		if strings.Contains(l, "environment=") {
			t.Fatalf("a headless run never opens an empty chat: %v", opened)
		}
	}
}

func TestARoutineReadsOnlyItsOwnAccountAndFolder(t *testing.T) {
	e := watch.Event{Kind: watch.KindRoutine, Ref: "routine/retro"}
	got := routineScope(WatchSpec{Workspace: "onboarding", Dir: "/Users/me/work/IM/onboarding", ConfigDir: "/Users/me/.claude-skp"}, e)
	for _, want := range []string{"/Users/me/work/IM/onboarding", "/Users/me/.claude-skp", "corgi agent helper mine-sessions --root /Users/me/.claude-skp/projects --project /Users/me/work/IM/onboarding --days 7", "never another account's"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	plain := routineScope(WatchSpec{Workspace: "corgi", Dir: "/Users/me/corgi"}, e)
	if !strings.Contains(plain, "/.claude/projects") {
		t.Fatalf("no configDir means the default account: %q", plain)
	}
	if routineScope(WatchSpec{Workspace: "corgi", Dir: "/x"}, watch.Event{Kind: watch.KindIssueNew}) != "" {
		t.Fatal("only routines get the scope line")
	}
}

func TestEveryRunCommitsAsTheWorkspace(t *testing.T) {
	prev := gitIdentity
	defer func() { gitIdentity = prev }()
	gitIdentity = func(dir string) (string, string) {
		if dir == "/w/acme" {
			return "Andrii", "gradli@example.com"
		}
		return "", ""
	}
	got := identityNote(WatchSpec{Dir: "/w/acme"})
	if !strings.Contains(got, "Andrii <gradli@example.com>") || !strings.Contains(got, "outside the workspace") {
		t.Fatalf("%q", got)
	}
	if identityNote(WatchSpec{Dir: "/w/unknown"}) != "" {
		t.Fatal("no identity, no line")
	}
}

func TestAFreshCloneCommitsAsTheWorkspaceThroughTheEnvironment(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	git := func(dir string, env []string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null"), env...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	ws := t.TempDir()
	git(ws, nil, "init", "-q")
	git(ws, nil, "config", "user.name", "Work Self")
	git(ws, nil, "config", "user.email", "work@example.com")
	env := identityEnv(ws)
	if len(env) != 4 {
		t.Fatalf("env %v", env)
	}
	scratch := t.TempDir()
	git(scratch, nil, "init", "-q")
	git(scratch, nil, "config", "user.email", "someone-else@example.com")
	git(scratch, env, "commit", "-q", "--allow-empty", "-m", "fix")
	if got := git(scratch, nil, "log", "-1", "--format=%ae %ce %an"); got != "work@example.com work@example.com Work Self" {
		t.Fatalf("committed as %q", got)
	}
	if identityEnv("") != nil {
		t.Fatal("no workspace folder sets nothing")
	}
}
