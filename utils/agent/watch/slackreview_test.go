package watch

import (
	"context"
	"testing"
	"time"
)

const reviewPostTS = "1726000400.000100"

func reviewFollower(f *slackFake) *Slack {
	s := newTestSlack(f, SlackWatchConfig{ReviewChannels: []string{"#code-review"}, Recheck: []string{"fixed"}, ApprovedWord: "Approved"})
	s.now = func() time.Time { return slackTSTime(reviewPostTS).Add(time.Hour) }
	return s
}

func reviewCursor() Cursor {
	return Cursor{"me": "UME", "team": "acme", "hist:C0RE": "1726000450.000100", "chan:#code-review": "C0RE", "rt:C0RE:" + reviewPostTS: reviewPostTS}
}

func TestAColleaguesPostIsFollowedForASecondLook(t *testing.T) {
	f := newSlackFake(t)
	f.history["C0RE"] = `{"ok":true,"messages":[
		{"type":"message","user":"UTM","ts":"1726000400.000100","text":"api: https://github.com/acme/api/pull/519"}
	],"has_more":false}`
	s := reviewFollower(f)
	_, next, err := s.Poll(context.Background(), Cursor{"me": "UME", "team": "acme", "hist:C0RE": "1726000300.000100", "chan:#code-review": "C0RE"})
	if err != nil {
		t.Fatal(err)
	}
	if next["rt:C0RE:"+reviewPostTS] == "" {
		t.Fatalf("the post is followed: %v", next)
	}
}

func TestFixedAfterMyCommentsAsksForASecondLook(t *testing.T) {
	f := newSlackFake(t)
	f.history["C0RE"] = `{"ok":true,"messages":[],"has_more":false}`
	f.replies = `{"ok":true,"messages":[
		{"type":"message","user":"UTM","ts":"1726000400.000100","text":"api: https://github.com/acme/api/pull/519"},
		{"type":"message","user":"UME","ts":"1726000500.000100","thread_ts":"1726000400.000100","text":"Some comments"},
		{"type":"message","user":"UTM","ts":"1726000600.000100","thread_ts":"1726000400.000100","text":"Fixed"}
	]}`
	s := reviewFollower(f)
	events, next, err := s.Poll(context.Background(), reviewCursor())
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != KindReviewRequested || len(events[0].Links) != 1 {
		t.Fatalf("one second look at the pull request of the post: %+v", events)
	}
	again, _, _ := s.Poll(context.Background(), next)
	if len(again) != 0 {
		t.Fatalf("the same reply asks once: %+v", again)
	}
}

func TestNoSecondLookBeforeIAnsweredOrAfterIApproved(t *testing.T) {
	for name, thread := range map[string]string{
		"not answered yet": `{"ok":true,"messages":[
			{"type":"message","user":"UTM","ts":"1726000400.000100","text":"api: https://github.com/acme/api/pull/519"},
			{"type":"message","user":"UTM","ts":"1726000600.000100","thread_ts":"1726000400.000100","text":"fixed a typo"}]}`,
		"approved": `{"ok":true,"messages":[
			{"type":"message","user":"UTM","ts":"1726000400.000100","text":"api: https://github.com/acme/api/pull/519"},
			{"type":"message","user":"UME","ts":"1726000500.000100","thread_ts":"1726000400.000100","text":"Approved"},
			{"type":"message","user":"UTM","ts":"1726000600.000100","thread_ts":"1726000400.000100","text":"fixed one more thing"}]}`,
	} {
		f := newSlackFake(t)
		f.history["C0RE"] = `{"ok":true,"messages":[],"has_more":false}`
		f.replies = thread
		events, next, err := reviewFollower(f).Poll(context.Background(), reviewCursor())
		if err != nil || len(events) != 0 {
			t.Fatalf("%s: no run, got %+v %v", name, events, err)
		}
		if name == "approved" && next["rt:C0RE:"+reviewPostTS] != "" {
			t.Fatalf("an approved post is no longer followed: %v", next)
		}
	}
}

func TestSaysWordMatchesWholeWords(t *testing.T) {
	if !saysWord("fixed all", []string{"fixed"}) || !saysWord("Done, pushed", []string{"done"}) {
		t.Fatal("a plain word matches")
	}
	if saysWord("prefixed it", []string{"fixed"}) || saysWord("anything", []string{""}) {
		t.Fatal("a part of a longer word, or an empty word, does not")
	}
}

func TestAwaitsMeOnlyWhenTheLastWordIsTheirs(t *testing.T) {
	f := newSlackFake(t)
	f.replies = `{"ok":true,"messages":[
		{"type":"message","user":"UME","ts":"1726000400.000100","text":"api: https://github.com/acme/api/pull/519"},
		{"type":"message","user":"UTM","ts":"1726000600.000100","thread_ts":"1726000400.000100","text":"One comment"}]}`
	s := reviewFollower(f)
	s.me = "UME"
	if !s.AwaitsMe(context.Background(), "C0RE", reviewPostTS) {
		t.Fatal("their comment waits for my answer")
	}
	f.replies = `{"ok":true,"messages":[
		{"type":"message","user":"UME","ts":"1726000400.000100","text":"api: https://github.com/acme/api/pull/519"},
		{"type":"message","user":"UTM","ts":"1726000600.000100","thread_ts":"1726000400.000100","text":"Approved"}]}`
	if s.AwaitsMe(context.Background(), "C0RE", reviewPostTS) {
		t.Fatal("an approval waits for nothing")
	}
}
