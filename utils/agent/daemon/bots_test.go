package daemon

import (
	"context"
	"strings"
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
