package watch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const jiraStamp = "2006-01-02T15:04:05.000-0700"

type jiraFake struct {
	srv      *httptest.Server
	now      time.Time
	calls    atomic.Int32
	myself   atomic.Int32
	comments atomic.Int32
	jql      string
}

// newJiraFake serves myself, a search with a new issue assigned to me
// (ABC-1), an old updated one assigned to someone else (ABC-2), and the
// comments on ABC-1: one by a colleague, one by me.
func newJiraFake(t *testing.T) *jiraFake {
	t.Helper()
	f := &jiraFake{now: time.Now().UTC().Truncate(time.Millisecond)}
	stamp := func(d time.Duration) string { return f.now.Add(d).Format(jiraStamp) }
	search := fmt.Sprintf(`{"issues":[
  {"key":"ABC-1","fields":{"summary":"Login breaks","labels":["bug","agent"],"status":{"name":"To Do"},
   "assignee":{"accountId":"me-1","displayName":"Andrii"},"created":%q,"updated":%q,
   "description":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Steps to "},{"type":"text","text":"reproduce"}]}]}}},
  {"key":"ABC-2","fields":{"summary":"Old one","labels":[],"status":{"name":"In Progress"},
   "assignee":{"accountId":"u-2","displayName":"Bob"},"created":%q,"updated":%q,"description":null}}
]}`, stamp(-2*time.Hour), stamp(-time.Hour), stamp(-72*time.Hour), stamp(-30*time.Minute))
	comments := fmt.Sprintf(`{"comments":[
  {"id":"501","author":{"accountId":"me-1","displayName":"Andrii"},"created":%q,
   "body":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"On it"}]}]}},
  {"id":"500","author":{"accountId":"u-2","displayName":"Bob"},"created":%q,
   "body":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Can you check staging?"}]}]}}
]}`, stamp(-10*time.Minute), stamp(-20*time.Minute))
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		if email, token, ok := r.BasicAuth(); !ok || email != "a@acme.dev" || token != "jira-tok" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"errorMessages":["Unauthorized"]}`))
			return
		}
		switch r.URL.Path {
		case "/rest/api/3/myself":
			f.myself.Add(1)
			_, _ = w.Write([]byte(`{"accountId":"me-1","displayName":"Andrii","timeZone":"UTC"}`))
		case "/rest/api/3/search":
			f.jql = r.URL.Query().Get("jql")
			if r.URL.Query().Get("maxResults") != "50" || !strings.Contains(r.URL.Query().Get("fields"), "assignee") {
				t.Errorf("search params: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(search))
		case "/rest/api/3/issue/ABC-1/comment":
			f.comments.Add(1)
			if r.URL.Query().Get("orderBy") != "-created" {
				t.Errorf("comment params: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(comments))
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func TestJiraPoll(t *testing.T) {
	cases := []struct {
		name       string
		project    string
		cursor     func(f *jiraFake) Cursor
		wantKeys   []string
		wantMyself int32
		wantJQL    string
	}{
		{
			name:       "empty cursor resolves me and reports the last day",
			project:    "ABC",
			cursor:     func(*jiraFake) Cursor { return Cursor{} },
			wantKeys:   []string{"jira:ABC-1", "jira:ABC-1:c500"},
			wantMyself: 1,
			wantJQL:    `project = "ABC" AND updated >= "`,
		},
		{
			name:    "cursor with me skips myself and dates the JQL from the cursor",
			project: "",
			cursor: func(f *jiraFake) Cursor {
				return Cursor{"me": "me-1", "issues": f.now.Add(-3 * time.Hour).Format(time.RFC3339Nano), "comments": f.now.Add(-25 * time.Minute).Format(time.RFC3339Nano)}
			},
			wantKeys:   []string{"jira:ABC-1", "jira:ABC-1:c500"},
			wantMyself: 0,
			wantJQL:    `updated >= "` + time.Now().Add(-3*time.Hour).UTC().Format("2006-01-02 15:04") + `" ORDER BY updated ASC`,
		},
		{
			name:    "cursor after the new issue keeps only the comment",
			project: "ABC",
			cursor: func(f *jiraFake) Cursor {
				return Cursor{"me": "me-1", "issues": f.now.Add(-90 * time.Minute).Format(time.RFC3339Nano)}
			},
			wantKeys:   []string{"jira:ABC-1:c500"},
			wantMyself: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newJiraFake(t)
			j := NewJira(Secrets{JiraURL: f.srv.URL + "/", JiraEmail: "a@acme.dev", JiraToken: "jira-tok"}, tc.project)
			events, next, err := j.Poll(context.Background(), tc.cursor(f))
			if err != nil {
				t.Fatal(err)
			}
			if got := eventKeys(events); strings.Join(got, ",") != strings.Join(tc.wantKeys, ",") {
				t.Fatalf("keys = %v, want %v", got, tc.wantKeys)
			}
			if f.myself.Load() != tc.wantMyself {
				t.Errorf("myself calls = %d, want %d", f.myself.Load(), tc.wantMyself)
			}
			if tc.wantJQL != "" && !strings.Contains(f.jql, tc.wantJQL) {
				t.Errorf("jql = %q, want it to contain %q", f.jql, tc.wantJQL)
			}
			if tc.project == "" && strings.Contains(f.jql, "project") {
				t.Errorf("no project, no project clause: %q", f.jql)
			}
			if next["me"] != "me-1" || j.Me != "me-1" {
				t.Errorf("me not cached: cursor=%v struct=%q", next, j.Me)
			}
			if want := f.now.Add(-30 * time.Minute).Format(time.RFC3339Nano); next["issues"] != want {
				t.Errorf("issues cursor = %q, want %q", next["issues"], want)
			}
			if want := f.now.Add(-10 * time.Minute).Format(time.RFC3339Nano); next["comments"] != want {
				t.Errorf("comments cursor = %q, want %q", next["comments"], want)
			}

			again, after, err := j.Poll(context.Background(), next)
			if err != nil {
				t.Fatal(err)
			}
			if len(again) != 0 {
				t.Errorf("second poll emitted %v", eventKeys(again))
			}
			if after["issues"] != next["issues"] || after["comments"] != next["comments"] {
				t.Errorf("cursor moved with nothing new: %v -> %v", next, after)
			}
		})
	}
}

func TestJiraEventFields(t *testing.T) {
	f := newJiraFake(t)
	j := &Jira{URL: f.srv.URL, Email: "a@acme.dev", Token: "jira-tok", Project: "ABC"}
	events, _, err := j.Poll(context.Background(), Cursor{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("events: %+v", events)
	}
	issue, comment := events[0], events[1]
	if issue.Kind != KindIssueNew || issue.Source != "jira" || issue.Ref != "ABC-1" || !issue.Mine ||
		issue.State != "To Do" || issue.Assignee != "Andrii" || issue.Body != "Steps to  reproduce" ||
		strings.Join(issue.Labels, ",") != "bug,agent" || issue.URL != f.srv.URL+"/browse/ABC-1" {
		t.Errorf("issue: %+v", issue)
	}
	if comment.Kind != KindIssueComment || !comment.Mine || comment.Author != "Bob" || comment.Body != "Can you check staging?" ||
		comment.Ref != "ABC-1" || comment.Title != "Login breaks" || !comment.At.Equal(f.now.Add(-20*time.Minute)) {
		t.Errorf("comment: %+v", comment)
	}
	if f.comments.Load() != 1 {
		t.Errorf("comments fetched %d times, want once (ABC-2 is not mine)", f.comments.Load())
	}
}

func TestJiraNoToken(t *testing.T) {
	f := newJiraFake(t)
	for _, s := range []Secrets{{}, {JiraURL: f.srv.URL}, {JiraURL: f.srv.URL, JiraEmail: "a@acme.dev"}, {JiraEmail: "a@acme.dev", JiraToken: "jira-tok"}} {
		cursor := Cursor{"issues": "2026-09-09T10:00:00Z"}
		events, next, err := NewJira(s, "ABC").Poll(context.Background(), cursor)
		if !errors.Is(err, ErrNoToken) || events != nil || next["issues"] != cursor["issues"] {
			t.Errorf("%+v: got %v, %v, %v", s, events, next, err)
		}
	}
	if f.calls.Load() != 0 {
		t.Errorf("no token must not touch the API: %d calls", f.calls.Load())
	}
}

func TestJiraUnauthorized(t *testing.T) {
	f := newJiraFake(t)
	j := &Jira{URL: f.srv.URL, Email: "a@acme.dev", Token: "wrong"}
	_, next, err := j.Poll(context.Background(), Cursor{"issues": "2026-09-09T10:00:00Z"})
	if err == nil || !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "myself") {
		t.Fatalf("401 should surface with the path: %v", err)
	}
	if next["issues"] != "2026-09-09T10:00:00Z" {
		t.Errorf("cursor must survive a failed poll: %v", next)
	}
}
