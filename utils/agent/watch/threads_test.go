package watch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReviewThreadsReadsGitHubThreadsAndReviewBodies(t *testing.T) {
	var vars map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" || r.Header.Get("Authorization") != "Bearer ghp-x" {
			t.Errorf("request: %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Variables map[string]any `json:"variables"`
		}
		_ = json.Unmarshal(raw, &body)
		vars = body.Variables
		_, _ = w.Write([]byte(`{"data":{"repository":{"pullRequest":{
			"reviewThreads":{"nodes":[
				{"id":"PRRT_1","isResolved":false,"isOutdated":false,"path":"src/a.ts","line":0,"originalLine":12,
				 "comments":{"nodes":[{"databaseId":41,"author":{"login":"rev"},"createdAt":"2026-10-01T20:00:00Z","body":"the retry drops the token"}]}},
				{"id":"PRRT_2","isResolved":true,"path":"src/b.ts","line":3,"comments":{"nodes":[{"databaseId":42,"author":{"login":"rev"},"body":"ok"}]}}]},
			"reviews":{"nodes":[{"databaseId":7,"author":{"login":"rev"},"state":"CHANGES_REQUESTED","body":"two blockers"},{"databaseId":8,"author":{"login":"rev"},"state":"APPROVED","body":""}]}}}}}`))
	}))
	defer srv.Close()
	c := threadsClient{http: srv.Client(), github: srv.URL, ghToken: func() string { return "ghp-x" }}

	got, err := c.threads(context.Background(), Secrets{}, "https://github.com/acme/api/pull/42")
	if err != nil {
		t.Fatal(err)
	}
	if vars["o"] != "acme" || vars["r"] != "api" || vars["n"] != float64(42) {
		t.Fatalf("the number goes as a number, never an empty string: %v", vars)
	}
	if len(got) != 3 || got[0].Kind != "review" || got[0].State != "changes_requested" {
		t.Fatalf("a review with a body is a row, an empty approval is not: %+v", got)
	}
	if th := got[1]; th.ID != "PRRT_1" || th.Line != 12 || th.Resolved || th.Notes[0].ID != "41" || th.Notes[0].Author != "rev" {
		t.Fatalf("thread: %+v", th)
	}
	if !got[2].Resolved {
		t.Fatal("resolution state comes through")
	}
}

func TestReviewThreadsReadsGitLabDiscussions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "glpat-x" || !strings.HasSuffix(r.URL.Path, "/discussions") {
			t.Errorf("request: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[
			{"id":"d1","notes":[{"id":1,"body":"cap the retries","resolvable":true,"resolved":false,"author":{"username":"ana"},"position":{"new_path":"app/x.rb","new_line":9}},
			                    {"id":2,"body":"done","resolvable":true,"resolved":false,"author":{"username":"me"}}]},
			{"id":"d2","notes":[{"id":3,"body":"changed the description","system":true,"author":{"username":"ana"}}]},
			{"id":"d3","notes":[{"id":4,"body":"LGTM overall","resolvable":false,"author":{"username":"ana"}}]}]`))
	}))
	defer srv.Close()
	c := threadsClient{http: srv.Client(), gitlab: func(Secrets, string) (string, error) { return srv.URL + "/api/v4/projects/g%2Fp/merge_requests/5", nil }}

	got, err := c.threads(context.Background(), Secrets{GitLab: "glpat-x"}, "https://gitlab.acme.io/g/p/-/merge_requests/5")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("system notes are not conversations: %+v", got)
	}
	if th := got[0]; th.Kind != "thread" || th.Resolved || th.Path != "app/x.rb" || th.Line != 9 || len(th.Notes) != 2 || th.ID != "d1" {
		t.Fatalf("thread: %+v", th)
	}
	if got[1].Kind != "comment" {
		t.Fatalf("a note nobody can resolve is a comment: %+v", got[1])
	}
}
