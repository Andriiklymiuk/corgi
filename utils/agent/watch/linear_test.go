package watch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type linearFake struct {
	srv     *httptest.Server
	now     time.Time
	calls   atomic.Int32
	viewers atomic.Int32
	queries []string
}

// newLinearFake serves a viewer and one page of issues and comments: ABC-1
// is new and mine, ABC-2 is old but freshly updated, one comment on ABC-1
// is by someone else and one is my own.
func newLinearFake(t *testing.T) *linearFake {
	t.Helper()
	f := &linearFake{now: time.Now().UTC().Truncate(time.Millisecond)}
	stamp := func(d time.Duration) string { return f.now.Add(d).Format(time.RFC3339Nano) }
	data := fmt.Sprintf(`{"data":{
  "issues":{"nodes":[
    {"id":"i1","identifier":"ABC-1","title":"Login breaks","description":"Steps to reproduce","url":"https://linear.app/acme/issue/ABC-1",
     "state":{"name":"Todo"},"labels":{"nodes":[{"name":"Bug"},{"name":"agent"}]},"assignee":{"id":"me-1","name":"Andrii"},
     "createdAt":%q,"updatedAt":%q},
    {"id":"i2","identifier":"ABC-2","title":"Old one","description":"","url":"https://linear.app/acme/issue/ABC-2",
     "state":{"name":"In Progress"},"labels":{"nodes":[]},"assignee":null,
     "createdAt":%q,"updatedAt":%q}
  ]},
  "comments":{"nodes":[
    {"id":"9","body":"Can you check staging?","createdAt":%q,"user":{"id":"u-2","name":"Bob"},"issue":{"identifier":"ABC-1","url":"https://linear.app/acme/issue/ABC-1","title":"Login breaks"}},
    {"id":"10","body":"On it","createdAt":%q,"user":{"id":"me-1","name":"Andrii"},"issue":{"identifier":"ABC-1","url":"https://linear.app/acme/issue/ABC-1","title":"Login breaks"}}
  ]}}}`, stamp(-2*time.Hour), stamp(-time.Hour), stamp(-72*time.Hour), stamp(-30*time.Minute), stamp(-20*time.Minute), stamp(-10*time.Minute))
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		if r.Header.Get("Authorization") != "lin_api_tok" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"errors":[{"message":"Authentication required"}]}`))
			return
		}
		var req struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
		}
		f.queries = append(f.queries, req.Query)
		if strings.Contains(req.Query, "viewer") {
			f.viewers.Add(1)
			_, _ = w.Write([]byte(`{"data":{"viewer":{"id":"me-1","name":"Andrii","email":"a@acme.dev"}}}`))
			return
		}
		_, _ = w.Write([]byte(data))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func eventKeys(events []Event) []string {
	var keys []string
	for _, e := range events {
		keys = append(keys, e.Key)
	}
	return keys
}

func TestLinearPoll(t *testing.T) {
	cases := []struct {
		name        string
		cursor      func(f *linearFake) Cursor
		wantKeys    []string
		wantViewers int32
		wantQuery   string
	}{
		{
			name:        "empty cursor resolves viewer and reports the last day",
			cursor:      func(*linearFake) Cursor { return Cursor{} },
			wantKeys:    []string{"linear:ABC-1", "linear:ABC-1:c9"},
			wantViewers: 1,
			wantQuery:   `team: {key: {eq: "ABC"}}`,
		},
		{
			name: "cursor with me skips the viewer call",
			cursor: func(f *linearFake) Cursor {
				return Cursor{"me": "me-1", "issues": f.now.Add(-3 * time.Hour).Format(time.RFC3339Nano), "comments": f.now.Add(-25 * time.Minute).Format(time.RFC3339Nano)}
			},
			wantKeys:    []string{"linear:ABC-1", "linear:ABC-1:c9"},
			wantViewers: 0,
		},
		{
			name: "cursor after the new issue keeps only the comment",
			cursor: func(f *linearFake) Cursor {
				return Cursor{"me": "me-1", "issues": f.now.Add(-90 * time.Minute).Format(time.RFC3339Nano)}
			},
			wantKeys:    []string{"linear:ABC-1:c9"},
			wantViewers: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newLinearFake(t)
			l := NewLinear(Secrets{Linear: "lin_api_tok"}, "ABC")
			l.URL = f.srv.URL
			cursor := tc.cursor(f)
			events, next, err := l.Poll(context.Background(), cursor)
			if err != nil {
				t.Fatal(err)
			}
			if got := eventKeys(events); strings.Join(got, ",") != strings.Join(tc.wantKeys, ",") {
				t.Fatalf("keys = %v, want %v", got, tc.wantKeys)
			}
			if f.viewers.Load() != tc.wantViewers {
				t.Errorf("viewer calls = %d, want %d", f.viewers.Load(), tc.wantViewers)
			}
			if tc.wantQuery != "" && !strings.Contains(f.queries[len(f.queries)-1], tc.wantQuery) {
				t.Errorf("query lacks %q:\n%s", tc.wantQuery, f.queries[len(f.queries)-1])
			}
			if cursor["issues"] != "" && !strings.Contains(f.queries[len(f.queries)-1], `updatedAt: {gt: "`+cursor["issues"]+`"}`) {
				t.Errorf("query does not filter on the cursor:\n%s", f.queries[len(f.queries)-1])
			}
			if next["me"] != "me-1" || l.Me != "me-1" {
				t.Errorf("me not cached: cursor=%v struct=%q", next, l.Me)
			}
			if want := f.now.Add(-30 * time.Minute).Format(time.RFC3339Nano); next["issues"] != want {
				t.Errorf("issues cursor = %q, want %q", next["issues"], want)
			}
			if want := f.now.Add(-10 * time.Minute).Format(time.RFC3339Nano); next["comments"] != want {
				t.Errorf("comments cursor = %q, want %q", next["comments"], want)
			}

			again, after, err := l.Poll(context.Background(), next)
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

func TestLinearEventFields(t *testing.T) {
	f := newLinearFake(t)
	l := &Linear{Token: "lin_api_tok", URL: f.srv.URL}
	events, _, err := l.Poll(context.Background(), Cursor{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("events: %+v", events)
	}
	issue, comment := events[0], events[1]
	if issue.Kind != KindIssueNew || issue.Source != "linear" || issue.Ref != "ABC-1" || !issue.Mine ||
		issue.State != "Todo" || issue.Assignee != "Andrii" || issue.Body != "Steps to reproduce" ||
		strings.Join(issue.Labels, ",") != "Bug,agent" || issue.URL != "https://linear.app/acme/issue/ABC-1" {
		t.Errorf("issue: %+v", issue)
	}
	if comment.Kind != KindIssueComment || !comment.Mine || comment.Author != "Bob" || comment.Body != "Can you check staging?" ||
		comment.Ref != "ABC-1" || comment.Title != "Login breaks" || comment.At != f.now.Add(-20*time.Minute) {
		t.Errorf("comment: %+v", comment)
	}
	if strings.Contains(f.queries[len(f.queries)-1], "team:") {
		t.Error("no team, no team filter")
	}
}

func TestLinearNoToken(t *testing.T) {
	f := newLinearFake(t)
	l := NewLinear(Secrets{}, "")
	l.URL = f.srv.URL
	cursor := Cursor{"issues": "2026-09-09T10:00:00Z"}
	events, next, err := l.Poll(context.Background(), cursor)
	if !errors.Is(err, ErrNoToken) || events != nil {
		t.Fatalf("got %v, %v", events, err)
	}
	if next["issues"] != cursor["issues"] || f.calls.Load() != 0 {
		t.Errorf("no token must not touch the API: cursor=%v calls=%d", next, f.calls.Load())
	}
}

func TestLinearErrors(t *testing.T) {
	f := newLinearFake(t)
	l := &Linear{Token: "wrong", URL: f.srv.URL}
	if _, _, err := l.Poll(context.Background(), Cursor{}); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("401 should surface: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errors":[{"message":"Unknown argument sort"}]}`))
	}))
	defer srv.Close()
	l = &Linear{Token: "lin_api_tok", URL: srv.URL}
	if _, _, err := l.Poll(context.Background(), Cursor{"me": "me-1"}); err == nil || !strings.Contains(err.Error(), "Unknown argument sort") {
		t.Fatalf("graphql errors should surface: %v", err)
	}
}
