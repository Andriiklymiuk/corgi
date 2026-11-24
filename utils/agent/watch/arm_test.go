package watch

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func armServer(t *testing.T, answer func(r *http.Request, body string) string) (Forge, *[]string) {
	t.Helper()
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(body))
		_, _ = io.WriteString(w, answer(r, string(body)))
	}))
	t.Cleanup(srv.Close)
	return Forge{HTTP: srv.Client(), GitHubAPI: srv.URL, Secrets: Secrets{GitHub: "tok", GitLab: "tok", GitLabURL: srv.URL}}, &calls
}

func TestAutoMergeIsArmedOnMyOpenPullRequest(t *testing.T) {
	pr := `{"node_id":"PR_1","state":"open","user":{"login":"me"}}`
	f, calls := armServer(t, func(r *http.Request, body string) string {
		switch {
		case strings.HasSuffix(r.URL.Path, "/user"):
			return `{"login":"me"}`
		case strings.HasSuffix(r.URL.Path, "/graphql") && strings.Contains(body, `"MERGE"`):
			return `{"errors":[{"message":"Merge method merge commits are not allowed on this repository"}]}`
		case strings.HasSuffix(r.URL.Path, "/graphql"):
			return `{"data":{}}`
		}
		return pr
	})
	got, err := f.ArmMerge(context.Background(), "https://github.com/acme/api/pull/4")
	if err != nil || got != ArmedNow {
		t.Fatalf("arm = %q %v", got, err)
	}
	last := (*calls)[len(*calls)-1]
	if !strings.Contains(last, "enablePullRequestAutoMerge") || !strings.Contains(last, `"SQUASH"`) || !strings.Contains(last, "PR_1") {
		t.Fatalf("last call = %s", last)
	}
	for name, body := range map[string]string{
		"a draft":        `{"node_id":"PR_1","state":"open","draft":true,"user":{"login":"me"}}`,
		"someone else's": `{"node_id":"PR_1","state":"open","user":{"login":"sam"}}`,
		"a merged one":   `{"node_id":"PR_1","state":"closed","merged":true,"user":{"login":"me"}}`,
	} {
		pr = body
		before := len(*calls)
		if got, err := f.ArmMerge(context.Background(), "https://github.com/acme/api/pull/4"); err == nil {
			t.Fatalf("%s was armed: %q", name, got)
		}
		for _, c := range (*calls)[before:] {
			if strings.Contains(c, "graphql") {
				t.Fatalf("%s reached the mutation", name)
			}
		}
	}
	pr = `{"node_id":"PR_1","state":"open","user":{"login":"me"},"auto_merge":{"merge_method":"squash"}}`
	if got, err := f.ArmMerge(context.Background(), "https://github.com/acme/api/pull/4"); err != nil || got != ArmedAlready {
		t.Fatalf("already = %q %v", got, err)
	}
}

func TestAGitLabMergeRequestIsArmedOnlyWhileItsPipelineRuns(t *testing.T) {
	mr := `{"state":"opened","sha":"abc","author":{"username":"me"},"head_pipeline":{"status":"running"}}`
	f, calls := armServer(t, func(r *http.Request, _ string) string {
		if strings.HasSuffix(r.URL.Path, "/user") {
			return `{"username":"me"}`
		}
		return mr
	})
	link := f.Secrets.GitLabURL + "/acme/api/-/merge_requests/7"
	got, err := f.ArmMerge(context.Background(), link)
	last := (*calls)[len(*calls)-1]
	if err != nil || got != ArmedNow || !strings.HasPrefix(last, "PUT ") || !strings.Contains(last, `"merge_when_pipeline_succeeds":true`) || !strings.Contains(last, `"sha":"abc"`) {
		t.Fatalf("arm = %q %v, last %s", got, err, last)
	}
	for name, body := range map[string]string{
		"a finished pipeline": `{"state":"opened","author":{"username":"me"},"head_pipeline":{"status":"success"}}`,
		"no pipeline":         `{"state":"opened","author":{"username":"me"}}`,
		"a draft":             `{"state":"opened","draft":true,"author":{"username":"me"},"head_pipeline":{"status":"running"}}`,
		"someone else's":      `{"state":"opened","author":{"username":"sam"},"head_pipeline":{"status":"running"}}`,
	} {
		mr = body
		before := len(*calls)
		if got, err := f.ArmMerge(context.Background(), link); err == nil {
			t.Fatalf("%s was armed: %q", name, got)
		}
		for _, c := range (*calls)[before:] {
			if strings.HasPrefix(c, "PUT ") {
				t.Fatalf("%s reached the merge call", name)
			}
		}
	}
}
