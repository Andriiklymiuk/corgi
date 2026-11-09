package watch

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRerunFindsTheNewestFailedRunAndRerunsItsJobs(t *testing.T) {
	var posted []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer gh" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/user":
			io.WriteString(w, `{"login":"andrii"}`)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/actions/runs"):
			if r.URL.Query().Get("actor") != "andrii" {
				t.Errorf("a rerun lists only my runs, got actor %q", r.URL.Query().Get("actor"))
			}
			io.WriteString(w, `{"workflow_runs":[
			  {"id":11,"name":"release","html_url":"https://github.com/acme/api/actions/runs/11","updated_at":"2026-09-14T10:00:00Z"},
			  {"id":12,"name":"ci","html_url":"https://github.com/acme/api/actions/runs/12","updated_at":"2026-09-14T11:30:00Z"},
			  {"id":9,"name":"old","html_url":"https://github.com/acme/api/actions/runs/9","updated_at":"2026-09-13T11:30:00Z"}]}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/rerun-failed-jobs"):
			posted = append(posted, r.URL.Path)
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	prev := GitHubAPI
	GitHubAPI = srv.URL
	t.Cleanup(func() { GitHubAPI = prev })

	s := Secrets{GitHub: "gh"}
	since := time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC)
	run, err := NewestFailedRun(context.Background(), s, "acme/api", since)
	if err != nil || run.ID != 12 || run.Name != "ci" {
		t.Fatalf("the newest red since 11:00 is 12: %+v %v", run, err)
	}
	if _, err := NewestFailedRun(context.Background(), s, "acme/api", time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("nothing red since noon")
	}
	if err := RerunFailedJobs(context.Background(), s, "acme/api", run.ID); err != nil {
		t.Fatal(err)
	}
	if len(posted) != 1 || posted[0] != "/repos/acme/api/actions/runs/12/rerun-failed-jobs" {
		t.Fatalf("posted %v", posted)
	}
	if _, err := NewestFailedRun(context.Background(), Secrets{}, "acme/api", since); err != ErrNoToken {
		t.Fatalf("no token: %v", err)
	}
}

func TestRerunLogRemembersRuns(t *testing.T) {
	dir := t.TempDir()
	l := LoadReruns(dir)
	if l.Seen(12) {
		t.Fatal("nothing yet")
	}
	if err := l.Set(Rerun{RunID: 12, URL: "u", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if !LoadReruns(dir).Seen(12) {
		t.Fatal("a rerun is remembered across loads")
	}
}

func TestSetPullBodyWritesTheDescription(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/repos/acme/api/pulls/7" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		b, _ := io.ReadAll(r.Body)
		got = string(b)
	}))
	t.Cleanup(srv.Close)
	prev := GitHubAPI
	GitHubAPI = srv.URL
	t.Cleanup(func() { GitHubAPI = prev })
	if err := SetPullBody(context.Background(), Secrets{GitHub: "gh"}, "https://github.com/acme/api/pull/7", "## What\n\nx"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `"body":"## What\n\nx"`) {
		t.Fatalf("payload %s", got)
	}
	if err := SetPullBody(context.Background(), Secrets{}, "https://github.com/acme/api/pull/7", "x"); err != ErrNoToken {
		t.Fatalf("no token: %v", err)
	}
}

func TestAGitLabRedPipelineIsRetriedOnceWhenItIsTheNewest(t *testing.T) {
	var retried []string
	status := "failed"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "gl" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.EscapedPath() == "/api/v4/projects/acme%2Fweb%2Fapp/merge_requests/124/pipelines":
			io.WriteString(w, `[{"id":77,"status":"`+status+`","web_url":"https://gl/acme/web/app/-/pipelines/77","updated_at":"2026-11-09T11:30:00Z"},
			  {"id":70,"status":"failed","web_url":"https://gl/p/70","updated_at":"2026-11-09T09:00:00Z"}]`)
		case r.Method == http.MethodPost && r.URL.EscapedPath() == "/api/v4/projects/acme%2Fweb%2Fapp/pipelines/77/retry":
			retried = append(retried, r.URL.Path)
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	s := Secrets{GitLab: "gl", GitLabURL: srv.URL}
	since := time.Date(2026, 11, 9, 11, 0, 0, 0, time.UTC)
	run, err := NewestFailedPipeline(context.Background(), s, "acme/web/app!124", since)
	if err != nil || run.ID != 77 {
		t.Fatalf("the newest pipeline, red since 11:00, is 77: %+v %v", run, err)
	}
	if err := RetryPipeline(context.Background(), s, "acme/web/app!124", run.ID); err != nil || len(retried) != 1 {
		t.Fatalf("retried: %v %v", retried, err)
	}
	if _, err := NewestFailedPipeline(context.Background(), s, "acme/web/app!124", time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("nothing red since noon")
	}
	status = "running"
	if _, err := NewestFailedPipeline(context.Background(), s, "acme/web/app!124", since); err == nil || !strings.Contains(err.Error(), "running") {
		t.Fatalf("an older red pipeline is never retried while a newer one runs: %v", err)
	}
	if _, err := NewestFailedPipeline(context.Background(), Secrets{}, "acme/web/app!124", since); err != ErrNoToken {
		t.Fatalf("no token: %v", err)
	}
	if _, err := NewestFailedPipeline(context.Background(), s, "acme/web/app#124", since); err == nil {
		t.Fatal("a GitHub ref is not a merge request")
	}

	l := LoadReruns(t.TempDir())
	_ = l.Set(Rerun{RunID: 77, Forge: "gitlab", At: time.Now()})
	_ = l.Set(Rerun{RunID: 12, At: time.Now()})
	if !l.SeenOn("gitlab", 77) || l.Seen(77) || !l.Seen(12) || l.SeenOn("gitlab", 12) {
		t.Fatal("a GitLab pipeline and a GitHub run with the same number are two different things")
	}
}
