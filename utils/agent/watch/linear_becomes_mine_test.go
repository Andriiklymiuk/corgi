package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLinearOldIssueBecomesMine(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	stamp := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339Nano) }
	data := fmt.Sprintf(`{"data":{
  "issues":{"nodes":[
    {"id":"i7","identifier":"ABC-7","title":"Old story","description":"","url":"https://linear.app/acme/issue/ABC-7",
     "state":{"name":"To implement"},"labels":{"nodes":[]},"assignee":{"id":"me-1","name":"Andrii"},"creator":{"id":"u-2","name":"Bob"},
     "createdAt":%q,"updatedAt":%q,
     "children":{"nodes":[{"identifier":"ABC-8","state":{"name":"Todo"},"assignee":{"id":"me-1"}},{"identifier":"ABC-9","state":{"name":"Done"},"assignee":{"id":"me-1"}}]},
     "history":{"nodes":[{"id":"h1","createdAt":%q,"actor":{"id":"u-3"},"toAssignee":{"id":"me-1"},"toState":null}]}},
    {"id":"i8","identifier":"ABC-8","title":"Fix the label","description":"","url":"https://linear.app/acme/issue/ABC-8",
     "state":{"name":"Todo"},"labels":{"nodes":[]},"assignee":{"id":"me-1","name":"Andrii"},"creator":{"id":"u-3","name":"Pat"},
     "createdAt":%q,"updatedAt":%q,"parent":{"identifier":"ABC-7","title":"Old story"},"children":{"nodes":[]},"history":{"nodes":[]}},
    {"id":"i3","identifier":"ABC-3","title":"Moved by a bot","description":"","url":"https://linear.app/acme/issue/ABC-3",
     "state":{"name":"In Progress"},"labels":{"nodes":[]},"assignee":{"id":"me-1","name":"Andrii"},
     "createdAt":%q,"updatedAt":%q,"children":{"nodes":[]},
     "history":{"nodes":[{"id":"h2","createdAt":%q,"actor":null,"toAssignee":null,"toState":{"name":"In Progress"}}]}},
    {"id":"i4","identifier":"ABC-4","title":"Moved by me","description":"","url":"https://linear.app/acme/issue/ABC-4",
     "state":{"name":"In Review"},"labels":{"nodes":[]},"assignee":{"id":"me-1","name":"Andrii"},
     "createdAt":%q,"updatedAt":%q,"children":{"nodes":[]},
     "history":{"nodes":[{"id":"h3","createdAt":%q,"actor":{"id":"me-1"},"toAssignee":null,"toState":{"name":"In Review"}}]}}
  ]},
  "comments":{"nodes":[]}}}`,
		stamp(-72*time.Hour), stamp(-5*time.Minute), stamp(-5*time.Minute),
		stamp(-4*time.Minute), stamp(-4*time.Minute),
		stamp(-72*time.Hour), stamp(-6*time.Minute), stamp(-6*time.Minute),
		stamp(-72*time.Hour), stamp(-7*time.Minute), stamp(-7*time.Minute))
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Query string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		query = req.Query
		_, _ = w.Write([]byte(data))
	}))
	t.Cleanup(srv.Close)

	l := &Linear{URL: srv.URL, Token: "lin_api_tok", Me: "me-1"}
	cursor := Cursor{"me": "me-1", "issues": now.Add(-10 * time.Minute).Format(time.RFC3339Nano), "comments": now.Format(time.RFC3339Nano)}
	events, _, err := l.Poll(context.Background(), cursor)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"history(", "toAssignee", "toState", "parent {", "children"} {
		if !strings.Contains(query, want) {
			t.Errorf("the query asks for %s:\n%s", want, query)
		}
	}
	byKey := map[string]Event{}
	for _, e := range events {
		byKey[e.Key] = e
	}
	old, ok := byKey["linear:ABC-7:h1"]
	if !ok {
		t.Fatalf("the hand-off is an event: %v", eventKeys(events))
	}
	if old.Kind != KindIssueNew || !old.Mine || strings.Join(old.Subtasks, ",") != "ABC-8" {
		t.Fatalf("event %+v", old)
	}
	if sub := byKey["linear:ABC-8"]; sub.Parent != "ABC-7" || sub.ParentTitle != "Old story" {
		t.Fatalf("a sub-issue names its parent: %+v", sub)
	}
	if _, ok := byKey["linear:ABC-3:h2"]; ok {
		t.Fatal("an automation's move is not a hand-off")
	}
	if _, ok := byKey["linear:ABC-4:h3"]; ok {
		t.Fatal("my own move is not news")
	}
}
