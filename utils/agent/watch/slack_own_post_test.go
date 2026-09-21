package watch

import (
	"context"
	"strings"
	"testing"
)

func TestMyOwnReviewPostIsNotAReviewForMe(t *testing.T) {
	f := newSlackFake(t)
	f.history["C0RE"] = `{"ok":true,"messages":[
		{"type":"message","user":"UME","ts":"1726000400.000100",
		 "text":"[HUM-1500] Premium frequency\napi: https://github.com/acme/api/pull/519"}
	],"has_more":false}`
	s := newTestSlack(f, SlackWatchConfig{ReviewChannels: []string{"#code-review"}})

	events, _, err := s.Poll(context.Background(), Cursor{"me": "UME", "team": "acme", "hist:C0RE": "1726000300.000100", "chan:#code-review": "C0RE"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != KindReviewRequested || !events[0].Self {
		t.Fatalf("the post is a review request that I wrote: %+v", events)
	}
	why := Rules{Enabled: true, Reviews: true}.Why(events[0])
	if !strings.Contains(why, "my own") {
		t.Fatalf("my own pull requests are not mine to review, got %q", why)
	}
}

func TestAColleaguesReplyOnMyReviewPostIsFeedbackNotAReview(t *testing.T) {
	f := newSlackFake(t)
	f.history["C0RE"] = `{"ok":true,"messages":[
		{"type":"message","user":"UTM","ts":"1726000500.000100","thread_ts":"1726000400.000100",
		 "text":"approved, one nit: rename the flag"}
	],"has_more":false}`
	f.replies = `{"ok":true,"messages":[
		{"type":"message","user":"UME","ts":"1726000400.000100",
		 "text":"[HUM-1500] Premium frequency\napi: https://github.com/acme/api/pull/519"}
	]}`
	s := newTestSlack(f, SlackWatchConfig{ReviewChannels: []string{"#code-review"}})

	events, _, err := s.Poll(context.Background(), Cursor{"me": "UME", "team": "acme", "hist:C0RE": "1726000300.000100", "chan:#code-review": "C0RE"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("one reply, one event: %+v", events)
	}
	e := events[0]
	if e.Kind != KindChatMention || !e.Mine || e.Self {
		t.Fatalf("a reply on my post is a word for me, not a review to do: kind=%s mine=%v self=%v", e.Kind, e.Mine, e.Self)
	}
	if len(e.Links) != 1 || e.Links[0] != "https://github.com/acme/api/pull/519" {
		t.Fatalf("the pull request of the post rides along so it can be handed to the session on it: %v", e.Links)
	}
	if why := (Rules{Enabled: true, Reviews: true, Mentions: true}).Why(e); why != "" {
		t.Fatalf("it rings: %q", why)
	}
}

func TestAChatWordOnMyPullRequestHandsOverToTheSessionOnIt(t *testing.T) {
	e := Event{Kind: KindChatMention, Source: "slack", Ref: "slack-1", Author: "@utm", Body: "approved, one nit: rename the flag",
		URL: "https://acme.slack.com/archives/C0RE/p1726000500000100?thread_ts=1726000400.000100", Links: []string{"https://github.com/acme/api/pull/519"}, Mine: true}
	if got := PullLinkOf(e); got != "https://github.com/acme/api/pull/519" {
		t.Fatalf("the pull request in the post is the branch to hand it to, got %q", got)
	}
	line := HandoverLine(e)
	if !strings.Contains(line, "@utm") || !strings.Contains(line, "rename the flag") || !strings.Contains(line, "pull/519") {
		t.Fatalf("the line carries who, what and where: %q", line)
	}
	if HandoverLine(Event{Kind: KindChatMention, Body: "hi"}) != "" {
		t.Fatal("a mention with no pull request has no branch to go to")
	}
}
