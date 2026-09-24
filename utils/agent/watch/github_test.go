package watch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const githubNotifications = `[
  {"id":"n1","reason":"review_requested","updated_at":"2026-09-09T10:00:00Z",
   "subject":{"title":"Add retries","url":"https://api.github.com/repos/acme/api/pulls/12","type":"PullRequest",
              "latest_comment_url":"https://api.github.com/repos/acme/api/pulls/12"},
   "repository":{"full_name":"acme/api"}},
  {"id":"n2","reason":"mention","updated_at":"2026-09-09T09:00:00Z",
   "subject":{"title":"Fix login","url":"https://api.github.com/repos/acme/web/pulls/7","type":"PullRequest",
              "latest_comment_url":"https://api.github.com/repos/acme/web/issues/comments/99"},
   "repository":{"full_name":"acme/web"}},
  {"id":"n3","reason":"mention","updated_at":"2026-09-09T08:00:00Z",
   "subject":{"title":"Bug: crash","url":"https://api.github.com/repos/acme/api/issues/3","type":"Issue"},
   "repository":{"full_name":"acme/api"}},
  {"id":"n5","reason":"author","updated_at":"2026-09-09T06:30:00Z",
   "subject":{"title":"Android icon","url":"https://api.github.com/repos/acme/app/pulls/254","type":"PullRequest",
              "latest_comment_url":"https://api.github.com/repos/acme/app/issues/comments/500"},
   "repository":{"full_name":"acme/app"}},
  {"id":"n6","reason":"author","updated_at":"2026-09-09T06:00:00Z",
   "subject":{"title":"Android icon","url":"https://api.github.com/repos/acme/app/pulls/254","type":"PullRequest",
              "latest_comment_url":"https://api.github.com/repos/acme/app/issues/comments/501"},
   "repository":{"full_name":"acme/app"}},
  {"id":"n4","reason":"subscribed","updated_at":"2026-09-09T07:00:00Z",
   "subject":{"title":"Noise","url":"https://api.github.com/repos/acme/api/pulls/5","type":"PullRequest"},
   "repository":{"full_name":"acme/api"}}
]`

type githubFake struct {
	srv           *httptest.Server
	requests      atomic.Int32
	users         atomic.Int32
	notifications string
}

func newGitHubFake(t *testing.T) *githubFake {
	t.Helper()
	f := &githubFake{notifications: githubNotifications}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
			return
		}
		switch r.URL.Path {
		case "/user":
			f.users.Add(1)
			_, _ = w.Write([]byte(`{"login":"andrii"}`))
		case "/notifications":
			if r.URL.Query().Get("participating") != "true" {
				t.Errorf("participating missing: %s", r.URL.RawQuery)
			}
			if r.Header.Get("If-Modified-Since") != "" {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("Last-Modified", "Wed, 09 Sep 2026 10:00:00 GMT")
			w.Header().Set("X-Poll-Interval", "60")
			_, _ = w.Write([]byte(f.notifications))
		case "/repos/acme/web/issues/comments/99":
			_, _ = w.Write([]byte(`{"body":"can you add a test for the empty case?","user":{"login":"maria","type":"User"}}`))
		case "/repos/acme/app/issues/comments/500":
			_, _ = w.Write([]byte(`{"body":"ABC-1 App icon\nReview in Linear","user":{"login":"linear-code[bot]","type":"Bot"}}`))
		case "/repos/acme/app/issues/comments/501":
			_, _ = w.Write([]byte(`{"body":"rebased","user":{"login":"andrii","type":"User"}}`))
		case "/repos/acme/api/pulls/9/reviews/77":
			_, _ = w.Write([]byte(`{"body":"looks good, one nit inline","state":"COMMENTED","user":{"login":"maria","type":"User"}}`))
		case "/repos/acme/api/pulls/29/reviews":
			_, _ = w.Write([]byte(`[
  {"id":3001,"state":"COMMENTED","body":"## Review\n\nI1 the label step is only reached on your own PR","submitted_at":"2026-09-24T15:31:01Z","user":{"login":"maria","type":"User"}},
  {"id":3002,"state":"COMMENTED","body":"","submitted_at":"2026-09-24T15:31:02Z","user":{"login":"maria","type":"User"}},
  {"id":3003,"state":"COMMENTED","body":"Fixed in 5307e4b.","submitted_at":"2026-09-24T16:00:27Z","user":{"login":"andrii","type":"User"}}
]`))
		case "/repos/acme/api/pulls/29":
			_, _ = w.Write([]byte(`{"state":"open","head":{"sha":"abc123"}}`))
		case "/repos/acme/api/commits/abc123":
			_, _ = w.Write([]byte(`{"commit":{"committer":{"date":"2026-09-24T16:05:00Z"}}}`))
		case "/repos/acme/api/pulls/29/comments":
			_, _ = w.Write([]byte(`[
  {"id":7001,"body":"I2 the step is only reached on your own PR","created_at":"2026-09-24T15:31:02Z","user":{"login":"maria"}},
  {"id":7002,"body":"Fixed in 5307e4b.","created_at":"2026-09-24T16:00:27Z","user":{"login":"andrii"}}
]`))
		case "/repos/acme/api/issues/29/comments":
			_, _ = w.Write([]byte(`[]`))
		case "/repos/acme/api/pulls/30/reviews":
			_, _ = w.Write([]byte(`[
  {"id":4001,"state":"APPROVED","body":"ship it","submitted_at":"2026-09-20T09:00:00Z","user":{"login":"maria","type":"User"}}
]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func TestGitHubPoll(t *testing.T) {
	f := newGitHubFake(t)
	g := &GitHub{Token: "tok", URL: f.srv.URL}

	events, cursor, err := g.Poll(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || !events[2].Bot || events[2].Author == "" {
		t.Fatalf("events = %d, want 3 with the bot's tagged: %+v", len(events), events)
	}
	if (Rules{Enabled: true, PRs: true}).Match(events[2]) || !(Rules{Enabled: true, PRs: true, Bots: true}).Match(events[2]) {
		t.Fatal("a bot's comment counts only with --bots")
	}
	review, comment := events[0], events[1]
	if review.Kind != KindReviewRequested || review.Mine || review.Ref != "acme/api#12" ||
		review.Key != "github:acme/api#12:n1:2026-09-09T10:00:00Z" ||
		review.URL != "https://github.com/acme/api/pull/12" || review.Title != "Add retries" ||
		review.Source != "github" || review.At.IsZero() {
		t.Errorf("review event = %+v", review)
	}
	if comment.Kind != KindPRComment || !comment.Mine || comment.Ref != "acme/web#7" ||
		comment.Author != "maria" || comment.Body != "can you add a test for the empty case?" {
		t.Errorf("comment event = %+v", comment)
	}
	if review.Author != "" || review.Body != "" {
		t.Errorf("a review request points at the pull request itself, not a comment: %+v", review)
	}
	if cursor["me"] != "andrii" || cursor["lastModified"] != "Wed, 09 Sep 2026 10:00:00 GMT" || cursor["pollInterval"] != "60" {
		t.Errorf("cursor = %v", cursor)
	}
	if g.Me != "andrii" || f.users.Load() != 1 || f.requests.Load() != 8 {
		t.Errorf("me = %q, /user calls = %d, requests = %d", g.Me, f.users.Load(), f.requests.Load())
	}

	events, next, err := g.Poll(context.Background(), cursor)
	if err != nil {
		t.Fatal(err)
	}
	if events != nil || f.requests.Load() != 9 || f.users.Load() != 1 {
		t.Errorf("304 round: events = %v, requests = %d, /user calls = %d", events, f.requests.Load(), f.users.Load())
	}
	if next["lastModified"] != cursor["lastModified"] || next["me"] != "andrii" {
		t.Errorf("cursor changed on 304: %v", next)
	}
}

func TestGitHubRepoFilter(t *testing.T) {
	f := newGitHubFake(t)
	g := &GitHub{Token: "tok", URL: f.srv.URL, Repos: []string{"Acme/API"}}
	events, _, err := g.Poll(context.Background(), Cursor{"me": "andrii"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Ref != "acme/api#12" {
		t.Errorf("events = %+v", events)
	}
	if f.users.Load() != 0 {
		t.Errorf("/user fetched although cursor had me")
	}
}

func TestGitHubNoToken(t *testing.T) {
	f := newGitHubFake(t)
	old := githubAuthToken
	githubAuthToken = func() string { return "" }
	t.Cleanup(func() { githubAuthToken = old })

	g := NewGitHub(Secrets{}, nil)
	g.URL = f.srv.URL
	_, _, err := g.Poll(context.Background(), nil)
	if !errors.Is(err, ErrNoToken) {
		t.Fatalf("err = %v, want ErrNoToken", err)
	}
	if f.requests.Load() != 0 {
		t.Errorf("request sent without a token")
	}

	githubAuthToken = func() string { return "from-gh\n" }
	if got := NewGitHub(Secrets{}, nil).Token; got != "from-gh" {
		t.Errorf("gh auth token fallback = %q", got)
	}
	if got := NewGitHub(Secrets{GitHub: "saved"}, nil).Token; got != "saved" {
		t.Errorf("saved token = %q", got)
	}
}

func TestGitHubUnauthorized(t *testing.T) {
	f := newGitHubFake(t)
	g := &GitHub{Token: "wrong", URL: f.srv.URL}
	_, cursor, err := g.Poll(context.Background(), Cursor{"me": "andrii"})
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") || !strings.Contains(err.Error(), "Bad credentials") {
		t.Fatalf("err = %v", err)
	}
	if cursor["me"] != "andrii" {
		t.Errorf("cursor lost on error: %v", cursor)
	}
}

const githubAuthorActivity = `[
  {"id":"p1","reason":"author","updated_at":"2026-09-14T07:06:14Z",
   "subject":{"title":"Tracking consent","url":"https://api.github.com/repos/acme/app/pulls/258","type":"PullRequest",
              "latest_comment_url":null},
   "repository":{"full_name":"acme/app"}},
  {"id":"p2","reason":"author","updated_at":"2026-09-14T06:43:40Z",
   "subject":{"title":"Tracking consent","url":"https://api.github.com/repos/acme/app/pulls/258","type":"PullRequest",
              "latest_comment_url":"https://api.github.com/repos/acme/app/pulls/258"},
   "repository":{"full_name":"acme/app"}},
  {"id":"p3","reason":"comment","updated_at":"2026-09-14T06:00:00Z",
   "subject":{"title":"Retry queue","url":"https://api.github.com/repos/acme/api/pulls/9","type":"PullRequest",
              "latest_comment_url":"https://api.github.com/repos/acme/api/pulls/9/reviews/77"},
   "repository":{"full_name":"acme/api"}}
]`

func TestGitHubPollSkipsAuthorActivityWithoutAComment(t *testing.T) {
	f := newGitHubFake(t)
	f.notifications = githubAuthorActivity
	g := &GitHub{Token: "tok", URL: f.srv.URL}

	events, _, err := g.Poll(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want only the review with words in it: %+v", len(events), events)
	}
	if e := events[0]; e.Ref != "acme/api#9" || e.Author != "maria" || e.Body != "looks good, one nit inline" {
		t.Errorf("review event = %+v", e)
	}
}

const githubReviewIsTheSummary = `[
  {"id":"q1","reason":"author","updated_at":"2026-09-24T15:31:01Z",
   "subject":{"title":"Mobile compatibility (0.16.0)","url":"https://api.github.com/repos/acme/api/pulls/29","type":"PullRequest",
              "latest_comment_url":"https://api.github.com/repos/acme/api/pulls/29"},
   "repository":{"full_name":"acme/api"}},
  {"id":"q2","reason":"author","updated_at":"2026-09-24T18:00:00Z",
   "subject":{"title":"Retry queue","url":"https://api.github.com/repos/acme/api/pulls/30","type":"PullRequest",
              "latest_comment_url":"https://api.github.com/repos/acme/api/pulls/30"},
   "repository":{"full_name":"acme/api"}}
]`

// A review submitted with its summary in the body, and no comment after it, is
// a thread whose latest_comment_url is the pull request itself. It is still the
// review to work on - keyed the way the webhook keys it, so every poll while the
// thread stays unread is the same event, not a run each. An old review under a
// thread raised by something else (a push, a merge) is not raised again.
func TestGitHubPollReadsTheReviewBehindAnAuthorThread(t *testing.T) {
	f := newGitHubFake(t)
	f.notifications = githubReviewIsTheSummary
	g := &GitHub{Token: "tok", URL: f.srv.URL}

	events, _, err := g.Poll(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want the one review with words in it: %+v", len(events), events)
	}
	e := events[0]
	if e.Kind != KindPRReview || e.Key != "github:acme/api#29:r3001" {
		t.Errorf("kind/key = %s %s, want pr.review github:acme/api#29:r3001", e.Kind, e.Key)
	}
	if e.Author != "maria" || !e.Mine || e.Bot {
		t.Errorf("review event = %+v", e)
	}
	if e.Body == "" || e.Body[:9] != "## Review" {
		t.Errorf("body = %q, want the review summary", e.Body)
	}

	again, _, err := g.Poll(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 1 || again[0].Key != e.Key {
		t.Errorf("second poll = %+v, want the same key so the daemon's seen set holds it", again)
	}
}

// Feedback answered by hand - a reply and a push after it - is not a run.
// A reply alone is a promise; a push alone could be anything.
func TestGitHubAnsweredSinceNeedsAReplyAndAPushAfterIt(t *testing.T) {
	f := newGitHubFake(t)
	g := &GitHub{Token: "tok", URL: f.srv.URL}

	review := time.Date(2026, 9, 24, 15, 31, 1, 0, time.UTC)
	if why := g.AnsweredSince(context.Background(), "acme/api#29", review); why == "" {
		t.Error("a reply at 16:00 and a push at 16:05 answer a review at 15:31")
	}
	if why := g.AnsweredSince(context.Background(), "acme/api#29", review.Add(30*time.Minute)); why != "" {
		t.Errorf("a review at 16:01 has a push after it but no reply, got %q", why)
	}
	if why := g.AnsweredSince(context.Background(), "acme/api#29", review.Add(35*time.Minute)); why != "" {
		t.Errorf("nothing after 16:06, got %q", why)
	}
	if why := g.AnsweredSince(context.Background(), "acme/api#30", review); why != "" {
		t.Errorf("a pull request with no comments of mine, got %q", why)
	}
	if why := g.AnsweredSince(context.Background(), "acme/api#29", time.Time{}); why != "" {
		t.Errorf("no moment to compare against, got %q", why)
	}
}
