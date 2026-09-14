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
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/actions/runs"):
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
