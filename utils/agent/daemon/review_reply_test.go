package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/watch"
)

type fakeTeller struct {
	outcome watch.ReviewOutcome
	asked   []string
}

func (f *fakeTeller) Name() string { return "github" }
func (f *fakeTeller) Poll(context.Context, watch.Cursor) ([]watch.Event, watch.Cursor, error) {
	return nil, nil, nil
}
func (f *fakeTeller) MyReviewSince(_ context.Context, ref string, _ time.Time) (watch.ReviewOutcome, bool) {
	f.asked = append(f.asked, ref)
	return f.outcome, true
}

func reviewPost() watch.Event {
	return watch.Event{
		Key: "slack:C0RE:1726000400.000100", Source: "slack", Kind: watch.KindReviewRequested,
		Ref: "slack-1726000400", Title: "@teammate in #code-review: ABC-1505 fix", Body: "ABC-1505 fix: https://github.com/acme/web/pull/456",
		Author: "@teammate", State: "#code-review", Links: []string{"https://github.com/acme/web/pull/456"},
		URL: "https://acme.slack.com/archives/C0RE/p1726000400000100", Mine: true, At: time.Now(),
	}
}

func runReviewPost(t *testing.T, outcome watch.ReviewOutcome) (*chatSaid, *[]string) {
	t.Helper()
	d := testDaemon(t)
	d.Notify = func(string, string) {}
	said := &chatSaid{}
	d.Chat = said.fn()
	ran := fakeClaude(t)
	teller := &fakeTeller{outcome: outcome}
	d.Watches = []WatchSpec{{
		Workspace: "acme", Dir: t.TempDir(), ConfigDir: t.TempDir(), Action: "fix", SkipPermissions: true,
		Rules: watch.Rules{Enabled: true, Reviews: true}, Sources: []watch.Source{teller},
		Chat: &config.SlackWatch{Trust: []string{"@teammate"}},
	}}
	d.startWatches(context.Background())
	d.handleWatchEvent(context.Background(), reviewPost())
	d.runs.Wait()
	return said, ran
}

func TestAReviewPostReplyIsWhatWasPostedAndTheEmojiMatches(t *testing.T) {
	said, ran := runReviewPost(t, watch.ReviewOutcome{Comments: 2})
	said.mu.Lock()
	defer said.mu.Unlock()
	if len(said.posts) != 1 || said.posts[0] != "acme/web#456 — comments added (2)" {
		t.Fatalf("the thread hears what landed on the pull request, not the forge's review state: %v", said.posts)
	}
	if len(said.emoji) != 2 || said.emoji[0] != "eyes" || said.emoji[1] != "speech_balloon" {
		t.Fatalf("eyes while it runs, 💬 when comments were added: %v", said.emoji)
	}
	if len(*ran) != 1 || !strings.Contains((*ran)[0], "/corgi:review https://github.com/acme/web/pull/456") || strings.Contains((*ran)[0], "<<<") {
		t.Fatalf("a review post runs the review skill plainly, the same as a person typing it, no chat fence: %q", (*ran)[0])
	}
}

func TestAnApprovedReviewPostGetsTheCheck(t *testing.T) {
	said, _ := runReviewPost(t, watch.ReviewOutcome{Approved: true})
	said.mu.Lock()
	defer said.mu.Unlock()
	if len(said.posts) != 1 || said.posts[0] != "acme/web#456 — approved ✅" {
		t.Fatalf("posts = %v", said.posts)
	}
	if len(said.emoji) != 2 || said.emoji[1] != "white_check_mark" {
		t.Fatalf("✅ on the post when approved: %v", said.emoji)
	}
}
