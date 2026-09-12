package watch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Back to draft and reopen are the two moves the phone was missing: a review
// asked for too early, and a pull request closed by mistake.
func TestDraftAndReopenReachTheRightPlace(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.RequestURI)
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"title": "Search", "draft": false, "state": "opened"})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	link := srv.URL + "/acme/group/api/-/merge_requests/7"
	if err := DraftPR(context.Background(), Secrets{GitLab: "glpat-x"}, link); err != nil {
		t.Fatalf("draft: %v", err)
	}
	// GitLab keeps draft in the title: read it, then write it back prefixed.
	if len(got) != 2 || !strings.HasPrefix(got[0], "GET ") || !strings.HasPrefix(got[1], "PUT ") || !strings.Contains(got[1], "/api/v4/projects/acme%2Fgroup%2Fapi/merge_requests/7") {
		t.Fatalf("gitlab draft went to %q", got)
	}

	got = nil
	if err := ReopenPR(context.Background(), Secrets{GitLab: "glpat-x"}, link); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if len(got) != 1 || !strings.HasSuffix(got[0], "?state_event=reopen") {
		t.Fatalf("gitlab reopen went to %q", got)
	}

	for _, f := range []func(context.Context, Secrets, string) error{DraftPR, ReopenPR} {
		if err := f(context.Background(), Secrets{}, link); err == nil {
			t.Fatal("without a token it must say so")
		}
		if err := f(context.Background(), Secrets{GitLab: "x", GitHub: "y"}, "https://example.com/thing/1"); err == nil {
			t.Fatal("a link corgi cannot place is refused")
		}
	}
}

// A GitLab merge request already in draft is left alone: a second "Draft:"
// on the title would be the phone's doing, not the person's.
func TestDraftLeavesADraftAlone(t *testing.T) {
	var puts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			puts++
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"title": "Draft: Search", "draft": true, "state": "opened"})
	}))
	defer srv.Close()
	if err := DraftPR(context.Background(), Secrets{GitLab: "glpat-x"}, srv.URL+"/acme/api/-/merge_requests/1"); err != nil {
		t.Fatal(err)
	}
	if puts != 0 {
		t.Fatalf("an MR already in draft was written %d times", puts)
	}
}

// The state a row shows is one word for every surface, and "draft" is one
// of them now — GitHub keeps it beside state, GitLab beside the title.
func TestPullStateFoldsDraftIn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls/1"):
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "open", "draft": true, "merged": false})
		case strings.HasSuffix(r.URL.Path, "/pulls/2"):
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "open", "draft": false, "merged": false})
		case strings.HasSuffix(r.URL.Path, "/pulls/3"):
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "closed", "draft": true, "merged": true})
		case strings.HasSuffix(r.URL.Path, "/merge_requests/4"):
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "opened", "draft": true})
		case strings.HasSuffix(r.URL.Path, "/merge_requests/5"):
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "closed", "draft": true})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	g := &GitHub{Token: "t", URL: srv.URL}
	for num, want := range map[string]string{"1": "draft", "2": "open", "3": "merged"} {
		if got := g.pullState(context.Background(), map[string]string{}, "https://api.github.com/repos/acme/api/pulls/"+num); got != want {
			t.Errorf("github pull %s = %q, want %q", num, got, want)
		}
	}
	l := &GitLab{Token: "t", URL: srv.URL}
	for ref, want := range map[string]string{"acme/api!4": "draft", "acme/api!5": "closed"} {
		if got := l.RefState(context.Background(), ref); got != want {
			t.Errorf("gitlab %s = %q, want %q", ref, got, want)
		}
	}
}
