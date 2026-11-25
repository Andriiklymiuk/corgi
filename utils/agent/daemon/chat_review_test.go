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
