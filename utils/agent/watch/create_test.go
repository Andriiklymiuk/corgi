package watch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJiraCreatesAnEpicAndTasksUnderIt(t *testing.T) {
	var bodies []map[string]any
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		if u, p, ok := r.BasicAuth(); !ok || u != "me@acme.com" || p != "tok" {
			w.WriteHeader(401)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		switch r.URL.Path {
		case "/rest/api/3/issue":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "10", "key": "SHOP-" + string(rune('0'+len(bodies)))})
		case "/wiki/rest/api/content":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "99", "_links": map[string]string{"base": "https://acme.atlassian.net/wiki", "webui": "/spaces/ENG/pages/99"}})
		case "/wiki/rest/api/space":
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]string{{"key": "ENG", "name": "Engineering"}}})
		default:
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"errorMessages":["nope"]}`))
		}
	}))
	defer srv.Close()
	s := Secrets{JiraURL: srv.URL + "/", JiraEmail: "me@acme.com", JiraToken: "tok"}
	j := CreatorFor(s, "jira", "SHOP").(*Jira)
	ctx := context.Background()

	epic, err := j.Create(ctx, NewIssue{Title: "BrewShift", Body: "shifts", Epic: true})
	if err != nil || epic.Ref != "SHOP-1" || epic.URL != srv.URL+"/browse/SHOP-1" {
		t.Fatalf("epic: %+v %v", epic, err)
	}
	task, err := j.Create(ctx, NewIssue{Title: "Week grid", Body: "drag shifts", Parent: epic.Ref})
	if err != nil || task.Ref != "SHOP-2" {
		t.Fatalf("task: %+v %v", task, err)
	}
	f0 := bodies[0]["fields"].(map[string]any)
	f1 := bodies[1]["fields"].(map[string]any)
	if f0["issuetype"].(map[string]any)["name"] != "Epic" || f0["parent"] != nil || f0["description"] == nil {
		t.Fatalf("epic fields: %v", f0)
	}
	if f1["issuetype"].(map[string]any)["name"] != "Task" || f1["parent"].(map[string]any)["key"] != "SHOP-1" || f1["project"].(map[string]any)["key"] != "SHOP" {
		t.Fatalf("task fields: %v", f1)
	}
	if _, err := (&Jira{URL: srv.URL, Email: "me@acme.com", Token: "tok"}).Create(ctx, NewIssue{Title: "x"}); err == nil || !strings.Contains(err.Error(), "project") {
		t.Fatalf("no project: %v", err)
	}

	page, err := j.CreatePage(ctx, "ENG", "BrewShift", "<h1>BrewShift</h1>")
	if err != nil || page.URL != "https://acme.atlassian.net/wiki/spaces/ENG/pages/99" || page.Ref != "page 99" {
		t.Fatalf("page: %+v %v", page, err)
	}
	last := bodies[len(bodies)-1]
	if last["type"] != "page" || last["space"].(map[string]any)["key"] != "ENG" {
		t.Fatalf("page body: %v", last)
	}
	if _, err := j.CreatePage(ctx, " ", "x", "y"); err == nil {
		t.Fatal("a space is needed")
	}
	spaces, err := j.ConfluenceSpaces(ctx)
	if err != nil || len(spaces) != 1 || spaces[0].ID != "ENG" || spaces[0].Name != "Engineering" {
		t.Fatalf("spaces: %+v %v", spaces, err)
	}
	if j.SiteURL() != srv.URL {
		t.Fatalf("site: %s", j.SiteURL())
	}

	// A refusal carries the status and the body.
	wrong := &Jira{URL: srv.URL, Email: "someone", Token: "else", Project: "SHOP"}
	if _, err := wrong.Create(ctx, NewIssue{Title: "x"}); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("bad credentials: %v", err)
	}
	if err := j.sendJSON(ctx, http.MethodPost, "/nowhere", map[string]any{}, nil); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("a missing path: %v", err)
	}
}

func TestLinearCreatesUnderTheTeamAndParent(t *testing.T) {
	var queries []string
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "lin_tok" {
			w.WriteHeader(401)
			return
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		queries = append(queries, body["query"])
		switch {
		case strings.Contains(body["query"], `key: {eq: "NOPE"}`):
			_, _ = w.Write([]byte(`{"data":{"teams":{"nodes":[]}}}`))
		case strings.Contains(body["query"], "teams("):
			_, _ = w.Write([]byte(`{"data":{"teams":{"nodes":[{"id":"team-1"}]}}}`))
		case strings.Contains(body["query"], `title: "refused"`):
			_, _ = w.Write([]byte(`{"data":{"issueCreate":{"success":false}}}`))
		default:
			n++
			_, _ = w.Write([]byte(`{"data":{"issueCreate":{"success":true,"issue":{"id":"id-` + string(rune('0'+n)) + `","identifier":"ABC-` + string(rune('0'+n)) + `","url":"https://linear.app/x/issue/ABC-` + string(rune('0'+n)) + `"}}}}`))
		}
	}))
	defer srv.Close()
	l := CreatorFor(Secrets{Linear: "lin_tok"}, "linear", "ABC").(*Linear)
	l.URL = srv.URL
	ctx := context.Background()
	parent, err := l.Create(ctx, NewIssue{Title: "BrewShift", Body: "shifts", Epic: true})
	if err != nil || parent.ID != "id-1" || parent.Ref != "ABC-1" {
		t.Fatalf("parent: %+v %v", parent, err)
	}
	child, err := l.Create(ctx, NewIssue{Title: "Week grid", Parent: parent.ID})
	if err != nil || child.Ref != "ABC-2" || !strings.HasPrefix(child.URL, "https://linear.app/") {
		t.Fatalf("child: %+v %v", child, err)
	}
	joined := strings.Join(queries, "\n")
	if !strings.Contains(joined, `teamId: "team-1"`) || !strings.Contains(joined, `parentId: "id-1"`) || !strings.Contains(joined, `description: "shifts"`) {
		t.Fatalf("queries: %s", joined)
	}
	if _, err := l.Create(ctx, NewIssue{Title: "refused"}); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("a refusal: %v", err)
	}
	none := &Linear{Token: "lin_tok", Team: "NOPE", URL: srv.URL}
	if _, err := none.Create(ctx, NewIssue{Title: "x"}); err == nil || !strings.Contains(err.Error(), "NOPE") {
		t.Fatalf("unknown team: %v", err)
	}
	if _, err := (&Linear{Token: "lin_tok", URL: srv.URL}).teamID(ctx); err == nil {
		t.Fatal("a team is needed")
	}
}

func TestCreatorForFollowsTheCredentials(t *testing.T) {
	if CreatorFor(Secrets{}, "jira", "X") != nil || CreatorFor(Secrets{JiraToken: "t"}, "jira", "X") != nil {
		t.Fatal("jira needs a URL and a token")
	}
	if CreatorFor(Secrets{}, "linear", "X") != nil || CreatorFor(Secrets{Linear: "l"}, "github", "X") != nil {
		t.Fatal("linear needs a token; other trackers cannot create")
	}
	if _, ok := CreatorFor(Secrets{Linear: "l"}, " Linear ", "X").(*Linear); !ok {
		t.Fatal("the name is trimmed and lowercased")
	}
	if _, ok := CreatorFor(Secrets{JiraURL: "https://x", JiraToken: "t"}, "jira", "X").(*Jira); !ok {
		t.Fatal("jira with credentials")
	}
}
