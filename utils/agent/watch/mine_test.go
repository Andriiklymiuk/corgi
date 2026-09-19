package watch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJiraMineListsMyOpenTickets(t *testing.T) {
	var jql string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/search/jql" {
			http.NotFound(w, r)
			return
		}
		jql = r.URL.Query().Get("jql")
		_, _ = w.Write([]byte(`{"issues":[
  {"key":"ABC-5","fields":{"summary":"Waiting story","labels":["bug"],"status":{"name":"Todo"},"assignee":{"accountId":"me-1"},"created":"2026-09-01T10:00:00.000+0000","updated":"2026-09-02T10:00:00.000+0000","description":null,
   "issuetype":{"subtask":false},"subtasks":[{"key":"ABC-6","fields":{"status":{"name":"Todo"},"assignee":{"accountId":"me-1"}}}]}},
  {"key":"ABC-6","fields":{"summary":"Its subtask","labels":[],"status":{"name":"Todo"},"assignee":{"accountId":"me-1"},"created":"2026-09-01T11:00:00.000+0000","updated":"2026-09-02T11:00:00.000+0000","description":null,
   "issuetype":{"subtask":true},"parent":{"key":"ABC-5","fields":{"summary":"Waiting story"}}}}
]}`))
	}))
	t.Cleanup(srv.Close)
	j := &Jira{URL: srv.URL, Email: "a@acme.dev", Token: "jira-tok", Project: "ABC", Me: "me-1"}
	events, err := j.Mine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(jql, "assignee = currentUser()") || !strings.Contains(jql, "statusCategory != Done") || !strings.Contains(jql, `project = "ABC"`) {
		t.Fatalf("jql %q", jql)
	}
	if len(events) != 2 || events[0].Key != "jira:ABC-5" || events[0].Kind != KindIssueNew || !events[0].Mine || events[0].State != "Todo" {
		t.Fatalf("%+v", events)
	}
	if strings.Join(events[0].Subtasks, ",") != "ABC-6" || events[1].Parent != "ABC-5" || events[1].Labels != nil && len(events[1].Labels) != 0 {
		t.Fatalf("%+v", events)
	}
}

func TestLinearMineListsMyOpenIssues(t *testing.T) {
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Query string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		query = req.Query
		_, _ = w.Write([]byte(`{"data":{"issues":{"nodes":[
   {"id":"i5","identifier":"ABC-5","title":"Waiting story","description":"","url":"https://linear.app/acme/issue/ABC-5","state":{"name":"To implement"},
    "labels":{"nodes":[]},"assignee":{"id":"me-1","name":"Andrii"},"creator":{"id":"u-2","name":"Bob"},"createdAt":"2026-09-01T10:00:00.000Z","updatedAt":"2026-09-02T10:00:00.000Z",
    "children":{"nodes":[]},"history":{"nodes":[]}}
  ]}}}`))
	}))
	t.Cleanup(srv.Close)
	l := &Linear{URL: srv.URL, Token: "lin_api_tok", Me: "me-1", Team: "ABC"}
	events, err := l.Mine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(query, "isMe") || !strings.Contains(query, "completed") || !strings.Contains(query, `team: {key: {eq: "ABC"}}`) {
		t.Fatalf("query:\n%s", query)
	}
	if len(events) != 1 || events[0].Key != "linear:ABC-5" || !events[0].Mine || events[0].State != "To implement" {
		t.Fatalf("%+v", events)
	}
}
