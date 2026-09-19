package watch

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// An old ticket that a person assigns to me, or moves into a column, is work
// from that moment: the changelog says so even though `created` is old.
func TestJiraOldTicketBecomesMine(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	stamp := func(d time.Duration) string { return now.Add(d).Format(jiraStamp) }
	search := fmt.Sprintf(`{"issues":[
  {"key":"ABC-7","fields":{"summary":"Old story","labels":[],"status":{"name":"READY TO DEV"},
   "assignee":{"accountId":"me-1","displayName":"Andrii"},"creator":{"accountId":"u-2","displayName":"Bob"},"created":%q,"updated":%q,"description":null,
   "issuetype":{"name":"Story","subtask":false},
   "subtasks":[{"key":"ABC-8","fields":{"summary":"Fix the label","status":{"name":"To Do"},"assignee":{"accountId":"me-1"}}},
               {"key":"ABC-9","fields":{"summary":"Done already","status":{"name":"Done"},"assignee":{"accountId":"me-1"}}}]},
   "changelog":{"histories":[
     {"id":"901","created":%q,"author":{"accountId":"u-3","displayName":"Pat"},"items":[{"field":"assignee","to":"me-1","toString":"Andrii"}]}
   ]}},
  {"key":"ABC-8","fields":{"summary":"Fix the label","labels":[],"status":{"name":"To Do"},
   "assignee":{"accountId":"me-1","displayName":"Andrii"},"creator":{"accountId":"u-3","displayName":"Pat"},"created":%q,"updated":%q,"description":null,
   "issuetype":{"name":"Sub-task","subtask":true},"parent":{"key":"ABC-7","fields":{"summary":"Old story"}}}},
  {"key":"ABC-3","fields":{"summary":"Moved by me","labels":[],"status":{"name":"In Review"},
   "assignee":{"accountId":"me-1","displayName":"Andrii"},"created":%q,"updated":%q,"description":null},
   "changelog":{"histories":[
     {"id":"902","created":%q,"author":{"accountId":"me-1","displayName":"Andrii"},"items":[{"field":"status","toString":"In Review"}]}
   ]}},
  {"key":"ABC-4","fields":{"summary":"Nothing changed but a field","labels":[],"status":{"name":"In Progress"},
   "assignee":{"accountId":"me-1","displayName":"Andrii"},"created":%q,"updated":%q,"description":null},
   "changelog":{"histories":[
     {"id":"903","created":%q,"author":{"accountId":"u-3","displayName":"Pat"},"items":[{"field":"priority","toString":"High"}]}
   ]}}
]}`, stamp(-72*time.Hour), stamp(-5*time.Minute), stamp(-5*time.Minute),
		stamp(-4*time.Minute), stamp(-4*time.Minute),
		stamp(-72*time.Hour), stamp(-6*time.Minute), stamp(-6*time.Minute),
		stamp(-72*time.Hour), stamp(-7*time.Minute), stamp(-7*time.Minute))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/search/jql":
			if !strings.Contains(r.URL.Query().Get("expand"), "changelog") {
				t.Errorf("the search must expand the changelog: %s", r.URL.RawQuery)
			}
			for _, f := range []string{"parent", "subtasks", "issuetype"} {
				if !strings.Contains(r.URL.Query().Get("fields"), f) {
					t.Errorf("the search must ask for %s: %s", f, r.URL.RawQuery)
				}
			}
			_, _ = w.Write([]byte(search))
		default:
			_, _ = w.Write([]byte(`{"comments":[]}`))
		}
	}))
	t.Cleanup(srv.Close)

	j := &Jira{URL: srv.URL, Email: "a@acme.dev", Token: "jira-tok", Project: "ABC", Me: "me-1"}
	cursor := Cursor{"me": "me-1", "issues": now.Add(-10 * time.Minute).Format(time.RFC3339Nano), "comments": now.Format(time.RFC3339Nano)}
	events, _, err := j.Poll(context.Background(), cursor)
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]Event{}
	for _, e := range events {
		byKey[e.Key] = e
	}
	old, ok := byKey["jira:ABC-7:h901"]
	if !ok {
		t.Fatalf("the assignment is an event of its own: %v", eventKeys(events))
	}
	if old.Kind != KindIssueNew || !old.Mine || old.Ref != "ABC-7" || old.State != "READY TO DEV" {
		t.Fatalf("event %+v", old)
	}
	if got := strings.Join(old.Subtasks, ","); got != "ABC-8" {
		t.Fatalf("only the open subtasks of mine count: %q", got)
	}
	sub, ok := byKey["jira:ABC-8"]
	if !ok {
		t.Fatalf("a new subtask is a new issue: %v", eventKeys(events))
	}
	if sub.Parent != "ABC-7" || sub.ParentTitle != "Old story" {
		t.Fatalf("a subtask names its parent: %+v", sub)
	}
	if _, ok := byKey["jira:ABC-3:h902"]; ok {
		t.Fatal("a move I made myself is not news")
	}
	if _, ok := byKey["jira:ABC-4:h903"]; ok {
		t.Fatal("a field edit is not an assignment or a move")
	}
}
