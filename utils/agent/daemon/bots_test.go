package daemon

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/bots"
	"andriiklymiuk/corgi/utils/agent/watch"
)

// A bot with `on` runs by itself when its kind arrives in its workspace:
// as its own persona (the soul on the command line), filed under its name
// in the fix log with what it said — beside, not instead of, the
// workspace's own notify. A bot without `on` never runs on its own.
func TestABotRunsOnItsOwnKinds(t *testing.T) {
	d := testDaemon(t)
	notes := make(chan string, 8)
	d.Notify = func(_, body string) { notes <- body }
	ran := fakeClaude(t)
	store, _ := bots.Load(bots.Path(d.Dir))
	store.Put(bots.Bot{Name: "reviewer", Title: "Code Reviewer", Workspace: "acme", Soul: "Be brief, never merge.", Model: "sonnet", On: []string{"pr.review"}})
	store.Put(bots.Bot{Name: "chief", Title: "Chief", Workspace: "acme", Soul: "Answer questions."})
	if err := bots.Save(bots.Path(d.Dir), store); err != nil {
		t.Fatal(err)
	}
	d.Watches = []WatchSpec{{Workspace: "acme", Dir: t.TempDir(), ConfigDir: t.TempDir(), Repos: []string{"acme/api"}, Rules: watch.Rules{Enabled: true, PRs: true, Reviews: true}, Action: "notify", SkipPermissions: true}}
	d.startWatches(context.Background())

	e := watch.Event{Key: "github:acme/api#7:r1", Source: "github", Kind: watch.KindPRReview, Ref: "acme/api#7", Title: "Add retries", Body: "Looks wrong on line 40", URL: "https://github.com/acme/api/pull/7", Mine: true, At: time.Now()}
	d.handleWatchEvent(context.Background(), e)

	collectNotes(t, notes, "Code Reviewer on acme/api#7")
	d.runs.Wait()
	if len(*ran) != 1 {
		t.Fatalf("one run, the reviewer's: %v", *ran)
	}
	line := (*ran)[0]
	if !strings.Contains(line, "--append-system-prompt Be brief, never merge.") || !strings.Contains(line, "--model sonnet") || !strings.Contains(line, "You are Code Reviewer") {
		t.Fatalf("the run is the bot's: %s", line)
	}
	var mine []watch.FixRecord
	for _, r := range d.watchState.Fixes.RecentFixes("", 10) {
		if r.Bot == "reviewer" {
			mine = append(mine, r)
		}
	}
	if len(mine) != 1 || mine[0].Ref != "acme/api#7" || !mine[0].Done() || mine[0].Key != "bot:reviewer:github:acme/api#7:r1" {
		t.Fatalf("filed under the bot: %+v", mine)
	}
	if !strings.Contains(mine[0].Outcome(), "opened 1 PR") && mine[0].Note == "" {
		t.Fatalf("the run's word is kept: %+v", mine[0])
	}
}

// A bot whose run fails gets one more try a rung up the ladder — sonnet
// after haiku, opus after sonnet or the default — and the record says so;
// an opus bot has nowhere to go and fails once.
func TestAFailedBotRunRetriesOneModelUp(t *testing.T) {
	d := testDaemon(t)
	notes := make(chan string, 8)
	d.Notify = func(_, body string) { notes <- body }
	var mu sync.Mutex
	var ran []string
	prev := claudeCommand
	claudeCommand = func(ctx context.Context, dir string, env []string, args ...string) *exec.Cmd {
		mu.Lock()
		defer mu.Unlock()
		ran = append(ran, strings.Join(args, " "))
		// The reviewer's first attempt falls over; its retry on opus says
		// its line. Big fails on its own model.
		if line := strings.Join(args, " "); strings.Contains(line, "Be brief.") && strings.Contains(line, "--model opus") {
			return exec.CommandContext(ctx, "echo", "two findings, no merge")
		}
		return exec.CommandContext(ctx, "false")
	}
	t.Cleanup(func() { claudeCommand = prev })
	store, _ := bots.Load(bots.Path(d.Dir))
	store.Put(bots.Bot{Name: "reviewer", Title: "Code Reviewer", Workspace: "acme", Soul: "Be brief.", Model: "sonnet", On: []string{"pr.review"}})
	store.Put(bots.Bot{Name: "big", Title: "Big", Workspace: "acme", Soul: "Be big.", Model: "opus", On: []string{"pr.review"}})
	if err := bots.Save(bots.Path(d.Dir), store); err != nil {
		t.Fatal(err)
	}
	d.Watches = []WatchSpec{{Workspace: "acme", Dir: t.TempDir(), ConfigDir: t.TempDir(), Repos: []string{"acme/api"}, Rules: watch.Rules{Enabled: true, PRs: true, Reviews: true}, Action: "notify", SkipPermissions: true}}
	d.startWatches(context.Background())
	e := watch.Event{Key: "github:acme/api#7:r1", Source: "github", Kind: watch.KindPRReview, Ref: "acme/api#7", Title: "Add retries", URL: "https://github.com/acme/api/pull/7", Mine: true, At: time.Now()}
	d.handleWatchEvent(context.Background(), e)
	collectNotes(t, notes, "Code Reviewer on acme/api#7 — two findings, no merge", "Big on acme/api#7 failed")
	d.runs.Wait()
	mu.Lock()
	defer mu.Unlock()
	sonnet, opus := 0, 0
	for _, r := range ran {
		if strings.Contains(r, "--model sonnet") {
			sonnet++
		}
		if strings.Contains(r, "--model opus") {
			opus++
		}
	}
	// reviewer: sonnet then opus; big: opus once (its own model), no retry.
	if sonnet != 1 || opus != 2 {
		t.Fatalf("ladder: %v", ran)
	}
	var byBot = map[string]watch.FixRecord{}
	for _, r := range d.watchState.Fixes.RecentFixes("", 10) {
		byBot[r.Bot] = r
	}
	if r := byBot["reviewer"]; r.Retry != "opus" || r.Error != "" || r.Note != "two findings, no merge" {
		t.Fatalf("the retry is on the record: %+v", r)
	}
	if r := byBot["big"]; r.Retry != "" || r.Error == "" {
		t.Fatalf("opus fails once: %+v", r)
	}
}
