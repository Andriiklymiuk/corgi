package cmd

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestARedBuildIsRerunOnTheForgeItCameFrom(t *testing.T) {
	var hits []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.Method+" "+r.URL.Path)
		switch {
		case strings.HasSuffix(r.URL.Path, "/pipelines") && r.Method == http.MethodGet:
			io.WriteString(w, `[{"id":5,"status":"failed","updated_at":"2026-11-09T11:30:00Z"}]`)
		case strings.HasSuffix(r.URL.Path, "/retry"):
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	s := watch.Secrets{GitLab: "gl", GitLabURL: srv.URL}
	since := time.Date(2026, 11, 9, 11, 0, 0, 0, time.UTC)
	r, err := rerunRedBuildOn(context.Background(), dir, s, "acme/web!3", since)
	if err != nil || r.RunID != 5 || r.Forge != "gitlab" {
		t.Fatalf("a merge request goes to GitLab: %+v %v", r, err)
	}
	if _, err := rerunRedBuildOn(context.Background(), dir, s, "acme/web!3", since); err == nil || !strings.Contains(err.Error(), "rerun already") {
		t.Fatalf("one retry per pipeline: %v", err)
	}
	if _, err := rerunRedBuildOn(context.Background(), dir, watch.Secrets{}, "acme/api#7", since); err != watch.ErrNoToken {
		t.Fatalf("a pull request goes to GitHub, which needs its own token: %v", err)
	}
}
