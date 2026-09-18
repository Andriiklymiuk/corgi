package watch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClosingAndMergingReachTheRightPlace(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.RequestURI+" "+r.Header.Get("PRIVATE-TOKEN")+r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	link := srv.URL + "/acme/group/api/-/merge_requests/7"
	if err := ClosePR(context.Background(), Secrets{GitLab: "glpat-x", GitLabURL: srv.URL}, link); err != nil {
		t.Fatalf("close: %v", err)
	}
	if len(got) != 1 || !strings.Contains(got[0], "/api/v4/projects/acme%2Fgroup%2Fapi/merge_requests/7") {
		t.Fatalf("gitlab close went to %q", got)
	}
	if !strings.Contains(got[0], "glpat-x") || !strings.HasPrefix(got[0], "PUT ") {
		t.Fatalf("gitlab close = %q", got[0])
	}

	got = nil
	if err := MergePR(context.Background(), Secrets{GitLab: "glpat-x", GitLabURL: srv.URL}, link); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if len(got) != 1 || !strings.HasSuffix(strings.Fields(got[0])[1], "/merge") {
		t.Fatalf("gitlab merge went to %q", got)
	}

	if err := ClosePR(context.Background(), Secrets{}, link); err == nil {
		t.Fatal("closing without a token must say so")
	}
	for _, bad := range []string{"https://example.com/thing/1", "not a url", ""} {
		if err := ClosePR(context.Background(), Secrets{GitLab: "x", GitHub: "y"}, bad); err == nil {
			t.Errorf("%q is not a pull request corgi knows how to close", bad)
		}
	}
	if err := ClosePR(context.Background(), Secrets{GitHub: "y"}, "https://github.com/acme/api/issues/3"); err == nil {
		t.Fatal("an issue is not a pull request")
	}
}
