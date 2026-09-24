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

var gitlabTodos = `[
  {"id":10,"action_name":"marked","target_type":"MergeRequest","target_url":"https://gitlab.com/acme/api/-/merge_requests/9",
   "body":"later","created_at":"2026-09-09T10:00:00Z","project":{"path_with_namespace":"acme/api"},
   "target":{"iid":9,"title":"Later"},"author":{"username":"me"}},
  {"id":9,"action_name":"review_requested","target_type":"MergeRequest","target_url":"https://gitlab.com/acme/api/-/merge_requests/12",
   "body":"please review","created_at":"2026-09-09T09:00:00Z","project":{"path_with_namespace":"acme/api"},
   "target":{"iid":12,"title":"Add retries"},"author":{"username":"bob"}},
  {"id":7,"action_name":"mentioned","target_type":"MergeRequest","target_url":"https://gitlab.com/acme/web/-/merge_requests/7",
   "body":"` + strings.Repeat("x", 250) + `","created_at":"2026-09-09T08:00:00Z","project":{"path_with_namespace":"acme/web"},
   "target":{"iid":7,"title":"Fix login"},"author":{"username":"ann"}},
  {"id":6,"action_name":"mentioned","target_type":"MergeRequest","target_url":"https://gitlab.com/acme/web/-/merge_requests/7",
   "body":"Pipeline passed","created_at":"2026-09-09T07:30:00Z","project":{"path_with_namespace":"acme/web"},
   "target":{"iid":7,"title":"Fix login"},"author":{"username":"project_42_bot_9f","bot":true}},
  {"id":5,"action_name":"mentioned","target_type":"MergeRequest","target_url":"https://gitlab.com/acme/web/-/merge_requests/7",
   "body":"rebased","created_at":"2026-09-09T07:20:00Z","project":{"path_with_namespace":"acme/web"},
   "target":{"iid":7,"title":"Fix login"},"author":{"username":"me"}},
  {"id":4,"action_name":"mentioned","target_type":"Issue","target_url":"https://gitlab.com/acme/api/-/issues/3",
   "body":"issue","created_at":"2026-09-09T07:00:00Z","project":{"path_with_namespace":"acme/api"},
   "target":{"iid":3,"title":"Bug"},"author":{"username":"ann"}}
]`

type gitlabFake struct {
	srv      *httptest.Server
	requests atomic.Int32
}

func newGitLabFake(t *testing.T) *gitlabFake {
	t.Helper()
	f := &gitlabFake{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		if r.Header.Get("PRIVATE-TOKEN") != "tok" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"401 Unauthorized"}`))
			return
		}
		if r.URL.Path == "/api/v4/user" {
			_, _ = w.Write([]byte(`{"id":7,"username":"me"}`))
			return
		}
		if r.URL.Path == "/api/v4/merge_requests" {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		if r.URL.Path != "/api/v4/todos" || r.URL.Query().Get("state") != "pending" {
			t.Errorf("unexpected request %s?%s", r.URL.Path, r.URL.RawQuery)
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(gitlabTodos))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func TestGitLabPoll(t *testing.T) {
	f := newGitLabFake(t)
	g := NewGitLab(Secrets{GitLab: "tok", GitLabURL: f.srv.URL})
	g.Me = "me"

	events, cursor, err := g.Poll(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || !events[2].Bot || events[2].Author != "project_42_bot_9f" {
		t.Fatalf("events = %d, want 3 with the bot's tagged: %+v", len(events), events)
	}
	review, comment := events[0], events[1]
	if review.Key != "gitlab:todo:9" || review.Kind != KindReviewRequested || review.Ref != "acme/api!12" ||
		review.Title != "Add retries" || review.Author != "bob" || review.Mine || review.Source != "gitlab" ||
		review.URL != "https://gitlab.com/acme/api/-/merge_requests/12" || review.At.IsZero() {
		t.Errorf("review event = %+v", review)
	}
	if comment.Key != "gitlab:todo:7" || comment.Kind != KindPRComment || comment.Ref != "acme/web!7" || len(comment.Body) != 200 {
		t.Errorf("comment event = %+v", comment)
	}
	if cursor["lastId"] != "10" {
		t.Errorf("cursor = %v, want lastId 10", cursor)
	}

	events, next, err := g.Poll(context.Background(), cursor)
	if err != nil {
		t.Fatal(err)
	}
	if events != nil || next["lastId"] != "10" {
		t.Errorf("second round: events = %v, cursor = %v", events, next)
	}

	events, _, err = g.Poll(context.Background(), Cursor{"lastId": "7"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Key != "gitlab:todo:9" {
		t.Errorf("events after id 7 = %+v", events)
	}
}

func TestGitLabLearnsWhoIAm(t *testing.T) {
	f := newGitLabFake(t)
	g := NewGitLab(Secrets{GitLab: "tok", GitLabURL: f.srv.URL})
	events, cursor, err := g.Poll(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if cursor["me"] != "me" {
		t.Fatalf("cursor = %v, want me", cursor)
	}
	for _, e := range events {
		if e.Author == "me" {
			t.Fatalf("my own todo came through: %+v", e)
		}
	}
	if len(events) != 3 {
		t.Fatalf("events = %d, want 3", len(events))
	}
	before := f.requests.Load()
	g2 := NewGitLab(Secrets{GitLab: "tok", GitLabURL: f.srv.URL})
	if _, _, err := g2.Poll(context.Background(), cursor); err != nil {
		t.Fatal(err)
	}
	if f.requests.Load()-before != 2 || g2.Me != "me" {
		t.Fatalf("requests = %d, me = %q", f.requests.Load()-before, g2.Me)
	}
}

func TestGitLabNoToken(t *testing.T) {
	f := newGitLabFake(t)
	g := NewGitLab(Secrets{GitLabURL: f.srv.URL})
	_, _, err := g.Poll(context.Background(), nil)
	if !errors.Is(err, ErrNoToken) {
		t.Fatalf("err = %v, want ErrNoToken", err)
	}
	if f.requests.Load() != 0 {
		t.Errorf("request sent without a token")
	}
	if NewGitLab(Secrets{}).URL != "" || g.Name() != "gitlab" {
		t.Errorf("defaults: url %q name %q", NewGitLab(Secrets{}).URL, g.Name())
	}
}

func TestGitLabUnauthorized(t *testing.T) {
	f := newGitLabFake(t)
	g := &GitLab{URL: f.srv.URL, Token: "wrong"}
	_, cursor, err := g.Poll(context.Background(), Cursor{"lastId": "3"})
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") || !strings.Contains(err.Error(), "Unauthorized") {
		t.Fatalf("err = %v", err)
	}
	if cursor["lastId"] != "3" {
		t.Errorf("cursor lost on error: %v", cursor)
	}
}

func TestGitLabNotesOnMyMergeRequests(t *testing.T) {
	var sawSince string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/user":
			_, _ = w.Write([]byte(`{"id":7,"username":"me"}`))
		case "/api/v4/todos":
			_, _ = w.Write([]byte(`[{"id":3,"action_name":"build_failed","target_type":"MergeRequest",
			  "target_url":"https://gitlab.com/acme/api/-/merge_requests/5","body":"pipeline","created_at":"2026-09-22T10:00:00Z",
			  "project":{"path_with_namespace":"acme/api"},"target":{"iid":5,"title":"Retries"},"author":{"username":"me"}}]`))
		case "/api/v4/merge_requests":
			if r.URL.Query().Get("scope") != "created_by_me" || r.URL.Query().Get("state") != "opened" {
				t.Errorf("merge requests query = %s", r.URL.RawQuery)
			}
			sawSince = r.URL.Query().Get("updated_after")
			_, _ = w.Write([]byte(`[{"iid":5,"project_id":42,"title":"Retries","web_url":"https://gitlab.com/acme/api/-/merge_requests/5",
			  "state":"opened","updated_at":"2026-09-22T10:05:00.500Z","references":{"full":"acme/api!5"}}]`))
		case "/api/v4/projects/42/merge_requests/5/notes":
			_, _ = w.Write([]byte(`[
			  {"id":90,"body":"CI passed","system":false,"created_at":"2026-09-22T10:05:00Z","author":{"username":"project_42_bot_1","bot":true}},
			  {"id":89,"body":"thanks","system":false,"created_at":"2026-09-22T10:04:00Z","author":{"username":"me"}},
			  {"id":88,"body":"added 1 commit","system":true,"created_at":"2026-09-22T10:03:00Z","author":{"username":"me"}},
			  {"id":87,"body":"rename this to fetchWithRetry","system":false,"created_at":"2026-09-22T10:02:00Z","author":{"username":"ann"}},
			  {"id":86,"body":"and this one","system":false,"created_at":"2026-09-22T10:01:00Z","author":{"username":"ann"}},
			  {"id":80,"body":"old","system":false,"created_at":"2026-09-22T09:00:00Z","author":{"username":"ann"}}]`))
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	g := &GitLab{URL: srv.URL, Token: "tok", Me: "me"}

	events, cursor, err := g.Poll(context.Background(), Cursor{"lastId": "1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != KindCIFailed || !events[0].Mine || events[0].Ref != "acme/api!5" || events[0].Body != "" {
		t.Fatalf("first round = %+v, want only the red build (notes start from here)", events)
	}
	if cursor["notesSince"] != "2026-09-22T10:05:00.5Z" {
		t.Fatalf("notesSince = %q", cursor["notesSince"])
	}

	cursor["notesSince"] = "2026-09-22T10:00:00Z"
	events, next, err := g.Poll(context.Background(), cursor)
	if err != nil {
		t.Fatal(err)
	}
	if sawSince != "2026-09-22T10:00:00Z" {
		t.Errorf("updated_after = %q", sawSince)
	}
	if len(events) != 1 {
		t.Fatalf("events = %+v, want the newest reviewer note alone", events)
	}
	e := events[0]
	if e.Key != "gitlab:note:87" || e.Kind != KindPRComment || !e.Mine || e.Author != "ann" || e.Ref != "acme/api!5" ||
		e.URL != "https://gitlab.com/acme/api/-/merge_requests/5" || e.Body != "rename this to fetchWithRetry" || e.State != "opened" {
		t.Errorf("note event = %+v", e)
	}
	if next["notesSince"] != "2026-09-22T10:05:00.5Z" {
		t.Errorf("next notesSince = %q", next["notesSince"])
	}
}

func TestGitLabRefFromURL(t *testing.T) {
	if got := gitlabRefFromURL("https://gitlab.com/acme/group/api/-/merge_requests/7", 7); got != "acme/group/api!7" {
		t.Errorf("ref = %q", got)
	}
}

func TestGitLabScopesCanWrite(t *testing.T) {
	cases := []struct {
		scopes []string
		want   bool
	}{
		{[]string{"api"}, true},
		{[]string{"read_api", "read_user"}, false},
		{nil, true},
	}
	for _, c := range cases {
		if got := GitLabScopesCanWrite(c.scopes); got != c.want {
			t.Errorf("GitLabScopesCanWrite(%v) = %v, want %v", c.scopes, got, c.want)
		}
	}
}

func TestTokenScopesReadsTheSelfEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/personal_access_tokens/self" || r.Header.Get("PRIVATE-TOKEN") != "glpat-x" {
			http.Error(w, "nope", 404)
			return
		}
		_, _ = w.Write([]byte(`{"scopes":["read_api"]}`))
	}))
	defer srv.Close()
	g := &GitLab{URL: srv.URL, Token: "glpat-x"}
	scopes, err := g.TokenScopes(context.Background())
	if err != nil || len(scopes) != 1 || scopes[0] != "read_api" {
		t.Fatalf("scopes %v, err %v", scopes, err)
	}
}

func TestGitLabAnsweredSinceNeedsANoteAndAPushAfterIt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/merge_requests/5/notes"):
			_, _ = w.Write([]byte(`[
			  {"id":89,"body":"done, see 3f2a","system":false,"created_at":"2026-09-22T10:04:00Z","author":{"username":"me"}},
			  {"id":88,"body":"added 1 commit","system":true,"created_at":"2026-09-22T10:03:00Z","author":{"username":"me"}},
			  {"id":87,"body":"rename this to fetchWithRetry","system":false,"created_at":"2026-09-22T10:02:00Z","author":{"username":"ann"}}]`))
		case strings.HasSuffix(r.URL.Path, "/merge_requests/5"):
			_, _ = w.Write([]byte(`{"iid":5,"state":"opened","sha":"3f2a"}`))
		case strings.HasSuffix(r.URL.Path, "/repository/commits/3f2a"):
			_, _ = w.Write([]byte(`{"id":"3f2a","committed_date":"2026-09-22T10:03:30Z"}`))
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	g := &GitLab{URL: srv.URL, Token: "tok", Me: "me"}

	note := time.Date(2026, 9, 22, 10, 2, 0, 0, time.UTC)
	if why := g.AnsweredSince(context.Background(), "acme/api!5", note); why == "" {
		t.Error("my note at 10:04 and the push at 10:03:30 answer ann's note at 10:02")
	}
	if why := g.AnsweredSince(context.Background(), "acme/api!5", note.Add(105*time.Second)); why != "" {
		t.Errorf("my note at 10:04 is after 10:03:45 but the push is not, got %q", why)
	}
	if why := g.AnsweredSince(context.Background(), "acme/api!5", note.Add(3*time.Minute)); why != "" {
		t.Errorf("nothing of mine after 10:05, got %q", why)
	}
	if why := (&GitLab{URL: srv.URL, Token: "tok"}).AnsweredSince(context.Background(), "acme/api!5", note); why != "" {
		t.Errorf("not knowing who I am, nothing can be mine, got %q", why)
	}
}
