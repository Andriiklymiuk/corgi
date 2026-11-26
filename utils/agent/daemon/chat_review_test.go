package daemon

import (
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/watch"
)

func channelReviewSpec() WatchSpec {
	return WatchSpec{
		Workspace: "acme", Action: "fix", Repos: []string{"acme/api", "acme/web"},
		Chat: &config.SlackWatch{ReviewChannels: []string{"#code-review"}, Review: &config.SlackReview{SkipRepos: []string{"acme/infra"}}},
	}
}

func channelPost(links ...string) watch.Event {
	return watch.Event{Key: "slack:C0RE:1726000400.000100", Source: "slack", Kind: watch.KindReviewRequested, Ref: "slack-1726000400", Author: "@stranger", Links: links}
}

func TestAReviewPostOfWatchedReposNeedsNoTrust(t *testing.T) {
	spec := channelReviewSpec()
	if why := chatRunRefusal(spec, channelPost("https://github.com/acme/api/pull/5")); why != "" {
		t.Fatalf("a post that names only watched repos is reviewed: %q", why)
	}
	if why := chatRunRefusal(spec, channelPost("https://github.com/other/repo/pull/5")); why == "" {
		t.Fatal("a repo nobody watches still needs a trusted author")
	}
	old := channelPost("https://github.com/acme/api/pull/5")
	old.At = time.Now().Add(-time.Hour)
	spec.Chat.Review.Since = time.Now()
	if why := chatRunRefusal(spec, old); !strings.Contains(why, "before") {
		t.Fatalf("a post from before the switch is left alone: %q", why)
	}
	spec.Chat.Review = nil
	if why := chatRunRefusal(spec, channelPost("https://github.com/acme/api/pull/5")); why == "" {
		t.Fatal("without the review block nothing changes: trust decides")
	}
}

func TestSkippedReposAreLeftToTheirOwner(t *testing.T) {
	spec := channelReviewSpec()
	mixed := withoutSkippedRepos(spec, channelPost("https://github.com/acme/api/pull/5", "https://github.com/acme/infra/pull/9"))
	if len(mixed.Links) != 1 || len(mixed.Skipped) != 1 || mixed.Skipped[0] != "acme/infra" {
		t.Fatalf("the skipped repo leaves the links: %+v", mixed)
	}
	only := withoutSkippedRepos(spec, channelPost("https://github.com/acme/infra/pull/9"))
	if why := chatRunRefusal(spec, only); !strings.Contains(why, "owner") {
		t.Fatalf("a post of skipped repos alone starts nothing: %q", why)
	}
}

func TestMyOwnReviewPostIsRememberedByItsPullRequests(t *testing.T) {
	d := testDaemon(t)
	d.watchState = watch.LoadState(d.Dir)
	spec := channelReviewSpec()
	mine := channelPost("https://github.com/acme/api/pull/5", "https://github.com/acme/web/pull/9")
	mine.Self = true

	d.rememberOwnReviewPost(spec, mine)
	d.rememberOwnReviewPost(spec, channelPost("https://github.com/acme/api/pull/7"))

	got := watch.ChannelPostsNaming(d.Dir, "acme", []string{"acme/web#9"})
	if len(got) != 1 || got[0].Channel != "C0RE" || got[0].TS != "1726000400.000100" {
		t.Fatalf("my post is the thread Fixed goes into: %+v", got)
	}
	if theirs := watch.ChannelPostsNaming(d.Dir, "acme", []string{"acme/api#7"}); len(theirs) != 0 {
		t.Fatalf("a colleague's post is not mine to answer Fixed in: %+v", theirs)
	}
}
