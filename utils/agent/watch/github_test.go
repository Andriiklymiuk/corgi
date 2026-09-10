package watch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const githubNotifications = `[
  {"id":"n1","reason":"review_requested","updated_at":"2026-09-09T10:00:00Z",
   "subject":{"title":"Add retries","url":"https://api.github.com/repos/acme/api/pulls/12","type":"PullRequest"},
   "repository":{"full_name":"acme/api"}},
  {"id":"n2","reason":"mention","updated_at":"2026-09-09T09:00:00Z",
   "subject":{"title":"Fix login","url":"https://api.github.com/repos/acme/web/pulls/7","type":"PullRequest"},
   "repository":{"full_name":"acme/web"}},
  {"id":"n3","reason":"mention","updated_at":"2026-09-09T08:00:00Z",
   "subject":{"title":"Bug: crash","url":"https://api.github.com/repos/acme/api/issues/3","type":"Issue"},
   "repository":{"full_name":"acme/api"}},
  {"id":"n4","reason":"subscribed","updated_at":"2026-09-09T07:00:00Z",
   "subject":{"title":"Noise","url":"https://api.github.com/repos/acme/api/pulls/5","type":"PullRequest"},
   "repository":{"full_name":"acme/api"}}
]`

type githubFake struct {
	srv      *httptest.Server
	requests atomic.Int32
	users    atomic.Int32
}

func newGitHubFake(t *testing.T) *githubFake {
	t.Helper()
	f := &githubFake{}
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
			_, _ = w.Write([]byte(githubNotifications))
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
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2: %+v", len(events), events)
	}
	review, comment := events[0], events[1]
	// review_requested is someone asking me to review THEIR pull request.
	if review.Kind != KindReviewRequested || review.Mine || review.Ref != "acme/api#12" ||
		review.Key != "github:acme/api#12:n1:2026-09-09T10:00:00Z" ||
		review.URL != "https://github.com/acme/api/pull/12" || review.Title != "Add retries" ||
		review.Source != "github" || review.At.IsZero() {
		t.Errorf("review event = %+v", review)
	}
	if comment.Kind != KindPRComment || !comment.Mine || comment.Ref != "acme/web#7" {
		t.Errorf("comment event = %+v", comment)
	}
	if cursor["me"] != "andrii" || cursor["lastModified"] != "Wed, 09 Sep 2026 10:00:00 GMT" || cursor["pollInterval"] != "60" {
		t.Errorf("cursor = %v", cursor)
	}
	if g.Me != "andrii" || f.users.Load() != 1 || f.requests.Load() != 2 {
		t.Errorf("me = %q, /user calls = %d, requests = %d", g.Me, f.users.Load(), f.requests.Load())
	}

	// Second round: If-Modified-Since → 304, one request, same cursor, no /user.
	events, next, err := g.Poll(context.Background(), cursor)
	if err != nil {
		t.Fatal(err)
	}
	if events != nil || f.requests.Load() != 3 || f.users.Load() != 1 {
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
