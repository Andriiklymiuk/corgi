package watch

import (
	"context"
	"testing"
	"time"
)

func TestRememberingAPostAgainKeepsWhenFixedWasSaid(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	post := ChannelPost{Workspace: "acme", Channel: "C0RE", TS: "1726000400.000100", Refs: []string{"acme/api#5"}, At: now}
	if err := RememberChannelPost(dir, post, now); err != nil {
		t.Fatal(err)
	}
	if err := MarkChannelPostFixed(dir, post, now); err != nil {
		t.Fatal(err)
	}
	post.Refs = append(post.Refs, "acme/web#9")
	if err := RememberChannelPost(dir, post, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	got := ChannelPostsNaming(dir, "acme", []string{"acme/web#9"})
	if len(got) != 1 || !got[0].FixedAt.Equal(now) {
		t.Fatalf("the post gains a pull request and keeps its fixed mark: %+v", got)
	}
}

func TestMyOwnReviewPostReachesOwnAndNeverTheSink(t *testing.T) {
	sunk, own := 0, 0
	w := &Watch{Workspace: "acme", Rules: Rules{Enabled: true, Reviews: true}, State: LoadState(t.TempDir()),
		Sink: func(context.Context, Event) { sunk++ },
		Own:  func(Event) { own++ }}
	mine := Event{Key: "slack:C0RE:1726000400.000100", Source: "slack", Kind: KindReviewRequested, Self: true,
		Links: []string{"https://github.com/acme/api/pull/5"}}
	if w.Handle(context.Background(), mine) || sunk != 0 {
		t.Fatal("my own post is not a review for me")
	}
	if own != 1 {
		t.Fatalf("my own post is still shown to Own, so its thread can be remembered: %d", own)
	}
	theirs := mine
	theirs.Key, theirs.Self = "slack:C0RE:1726000500.000100", false
	if !w.Handle(context.Background(), theirs) || own != 1 || sunk != 1 {
		t.Fatalf("a colleague's post goes to the sink only: own=%d sunk=%d", own, sunk)
	}
}
