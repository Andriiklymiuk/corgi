package daemon

import (
	"strings"
	"testing"
	"time"
)

func threadDaemon(retractable bool) (*Daemon, chan string, chan string) {
	posted, retracted := make(chan string, 8), make(chan string, 8)
	d := &Daemon{}
	d.NotifyAway = func(_, body, _ string) func() {
		posted <- body
		if !retractable {
			return nil
		}
		return func() { retracted <- body }
	}
	return d, posted, retracted
}

func nextPost(t *testing.T, ch chan string) string {
	t.Helper()
	select {
	case got := <-ch:
		return got
	case <-time.After(2 * time.Second):
		t.Fatal("nothing arrived")
		return ""
	}
}

func TestSecondPostAboutOnePullRequestReplacesTheFirst(t *testing.T) {
	d, posted, retracted := threadDaemon(true)
	link := "https://github.com/acme/api/pull/59"
	d.ring(attention{title: "t", body: "Sam reviewed acme/api#59: looks fine", workspace: "ws", link: link})
	d.ring(attention{title: "t", body: "fixed acme/api#59 - merge it", workspace: "ws", link: link})

	if first := nextPost(t, posted); first != "Sam reviewed acme/api#59: looks fine" {
		t.Fatalf("first post = %q", first)
	}
	second := nextPost(t, posted)
	if !strings.HasPrefix(second, "Sam reviewed acme/api#59") || !strings.HasSuffix(second, "fixed acme/api#59 - merge it") {
		t.Fatalf("second post does not tell the whole story: %q", second)
	}
	if gone := nextPost(t, retracted); gone != "Sam reviewed acme/api#59: looks fine" {
		t.Fatalf("retracted = %q", gone)
	}
}

func TestPostsAboutDifferentPullRequestsStayApart(t *testing.T) {
	d, posted, retracted := threadDaemon(true)
	d.ring(attention{title: "t", body: "one", workspace: "ws", link: "https://github.com/acme/api/pull/1"})
	d.ring(attention{title: "t", body: "two", workspace: "ws", link: "https://github.com/acme/api/pull/2"})
	if a, b := nextPost(t, posted), nextPost(t, posted); a != "one" || b != "two" {
		t.Fatalf("posts = %q, %q", a, b)
	}
	select {
	case gone := <-retracted:
		t.Fatalf("retracted %q", gone)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestPostThatCannotBeTakenBackIsNotRepeated(t *testing.T) {
	d, posted, _ := threadDaemon(false)
	link := "https://gitlab.com/acme/api/-/merge_requests/3"
	d.ring(attention{title: "t", body: "one", workspace: "ws", link: link})
	d.ring(attention{title: "t", body: "two", workspace: "ws", link: link})
	if a, b := nextPost(t, posted), nextPost(t, posted); a != "one" || b != "two" {
		t.Fatalf("posts = %q, %q", a, b)
	}
}

func TestSessionAndLinklessPostsAreNotThreaded(t *testing.T) {
	for _, a := range []attention{
		{workspace: "ws", link: "https://x/y", session: "s1"},
		{workspace: "ws", link: ""},
		{workspace: "ws", link: "file:///tmp/log"},
		{workspace: "ws", link: "https://x/y", run: "r"},
	} {
		if id := postThreadID(a); id != "" {
			t.Fatalf("%+v got thread %q", a, id)
		}
	}
}

func TestOldThreadIsForgotten(t *testing.T) {
	var p postThreads
	start := time.Now()
	p.keep("id", "one", func() {}, start)
	story, earlier := p.story("id", "two", start.Add(postThreadWindow+time.Minute))
	if story != "two" || earlier != nil {
		t.Fatalf("story = %q, earlier set = %v", story, earlier != nil)
	}
}
