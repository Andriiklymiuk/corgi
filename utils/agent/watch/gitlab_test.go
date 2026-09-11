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

	// Todo 6 is a project bot, todo 5 is my own comment: neither is a person
	// waiting on me, and both still move the cursor.
	events, cursor, err := g.Poll(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2: %+v", len(events), events)
	}
	review, comment := events[0], events[1]
	// A review request is on someone else's merge request, so it is not mine;
	// every other todo is.
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

	// Same feed again: everything is at or below lastId.
	events, next, err := g.Poll(context.Background(), cursor)
	if err != nil {
		t.Fatal(err)
	}
	if events != nil || next["lastId"] != "10" {
		t.Errorf("second round: events = %v, cursor = %v", events, next)
	}

	// A cursor in the middle of the feed keeps only the newer todos.
	events, _, err = g.Poll(context.Background(), Cursor{"lastId": "7"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Key != "gitlab:todo:9" {
		t.Errorf("events after id 7 = %+v", events)
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
