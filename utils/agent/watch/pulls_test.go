package watch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

// Between "in review" and "ready to merge" sit two facts the forge knows:
// do the checks pass, and did someone approve. Read once a round, kept in
// a file every surface reads, so the phone, the menu bar and the editor
// can all say "ready to merge" instead of "a pull request exists".

func TestPullRefFromALink(t *testing.T) {
	cases := map[string]string{
		"https://github.com/acme/api/pull/7":                   "acme/api#7",
		"https://github.com/acme/api/pull/7#issuecomment-1":    "acme/api#7",
		"https://gitlab.com/group/sub/proj/-/merge_requests/9": "group/sub/proj!9",
		"https://linear.app/acme/issue/APP-1":                  "",
	}
	for link, want := range cases {
		if got := PullRef(link); got != want {
			t.Errorf("PullRef(%s) = %q, want %q", link, got, want)
		}
	}
}

func TestChecksVerdict(t *testing.T) {
	if v := checksVerdict(nil, 0); v != "none" {
		t.Fatalf("no checks: %s", v)
	}
	if v := checksVerdict([]string{"success", "skipped"}, 0); v != "passing" {
		t.Fatalf("green: %s", v)
	}
	if v := checksVerdict([]string{"success", "failure"}, 0); v != "failing" {
		t.Fatalf("one red: %s", v)
	}
	if v := checksVerdict([]string{"success"}, 1); v != "pending" {
		t.Fatalf("one still running: %s", v)
	}
	if v := checksVerdict([]string{"skipped"}, 0); v != "none" {
		t.Fatalf("only skipped: %s", v)
	}
}

func TestReadyToMergeNeedsGreenChecksAndAnApproval(t *testing.T) {
	ready := PullStatus{State: "open", Checks: "passing", Review: "approved"}
	if !ready.Ready() || ready.Line() != "ready to merge · checks ✓ · approved" {
		t.Fatalf("ready: %v %q", ready.Ready(), ready.Line())
	}
	for _, p := range []PullStatus{
		{State: "open", Checks: "failing", Review: "approved"},
		{State: "open", Checks: "pending", Review: "approved"},
		{State: "open", Checks: "passing", Review: "changes"},
		{State: "open", Checks: "passing", Review: "pending"},
		{State: "draft", Checks: "passing", Review: "approved"},
		{State: "merged", Checks: "passing", Review: "approved"},
	} {
		if p.Ready() {
			t.Errorf("%+v should not be ready", p)
		}
	}
	if l := (PullStatus{State: "open", Checks: "failing", Review: "changes"}).Line(); l != "checks ✗ · changes requested" {
		t.Fatalf("line: %q", l)
	}
	// No checks configured at all is not a reason to hold a merge.
	if !(PullStatus{State: "open", Checks: "none", Review: "approved"}).Ready() {
		t.Fatal("no checks + approved should be ready")
	}
}

func TestGitHubPullStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/api/pulls/7":
			_, _ = w.Write([]byte(`{"state":"open","merged":false,"draft":false,"head":{"sha":"abc"},"requested_reviewers":[]}`))
		case "/repos/acme/api/pulls/7/reviews":
			_, _ = w.Write([]byte(`[{"state":"CHANGES_REQUESTED","user":{"login":"dan"}},{"state":"COMMENTED","user":{"login":"sam"}},{"state":"APPROVED","user":{"login":"dan"}}]`))
		case "/repos/acme/api/commits/abc/check-runs":
			_, _ = w.Write([]byte(`{"check_runs":[{"status":"completed","conclusion":"success"},{"status":"completed","conclusion":"skipped"}]}`))
		case "/repos/acme/api/commits/abc/status":
			_, _ = w.Write([]byte(`{"state":"success","statuses":[{"state":"success"}]}`))
		case "/repos/acme/api/pulls/8":
			_, _ = w.Write([]byte(`{"state":"open","merged":false,"draft":false,"head":{"sha":"def"},"requested_reviewers":[{"login":"sam"}]}`))
		case "/repos/acme/api/pulls/8/reviews":
			_, _ = w.Write([]byte(`[]`))
		case "/repos/acme/api/commits/def/check-runs":
			_, _ = w.Write([]byte(`{"check_runs":[{"status":"in_progress","conclusion":null},{"status":"completed","conclusion":"failure"}]}`))
		case "/repos/acme/api/commits/def/status":
			_, _ = w.Write([]byte(`{"state":"pending","statuses":[]}`))
		case "/repos/acme/api/pulls/9":
			_, _ = w.Write([]byte(`{"state":"closed","merged":true,"draft":false,"head":{"sha":"x"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	g := &GitHub{Token: "tok", URL: srv.URL}

	// dan asked for changes, then approved: the last word counts.
	st, ok := g.PullStatus(context.Background(), "acme/api#7")
	if !ok || !st.Ready() {
		t.Fatalf("7: %+v %v", st, ok)
	}
	// A failing run wins over one still running; a requested reviewer who
	// has not spoken is a review pending.
	st, ok = g.PullStatus(context.Background(), "acme/api#8")
	if !ok || st.Checks != "failing" || st.Review != "pending" {
		t.Fatalf("8: %+v", st)
	}
	st, ok = g.PullStatus(context.Background(), "acme/api#9")
	if !ok || st.State != "merged" || st.Checks != "" {
		t.Fatalf("9: %+v", st)
	}
	if _, ok := g.PullStatus(context.Background(), "acme/api!7"); ok {
		t.Fatal("a GitLab ref is not GitHub's to answer")
	}
}

func TestGitLabPullStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/projects/acme%2Fapi/merge_requests/7", "/api/v4/projects/acme/api/merge_requests/7":
			_, _ = w.Write([]byte(`{"state":"opened","draft":false,"head_pipeline":{"status":"success"}}`))
		case "/api/v4/projects/acme%2Fapi/merge_requests/7/approvals", "/api/v4/projects/acme/api/merge_requests/7/approvals":
			_, _ = w.Write([]byte(`{"approved":true,"approved_by":[{"user":{"username":"dan"}}]}`))
		case "/api/v4/projects/acme%2Fapi/merge_requests/8", "/api/v4/projects/acme/api/merge_requests/8":
			_, _ = w.Write([]byte(`{"state":"opened","draft":true,"head_pipeline":{"status":"running"}}`))
		case "/api/v4/projects/acme%2Fapi/merge_requests/8/approvals", "/api/v4/projects/acme/api/merge_requests/8/approvals":
			_, _ = w.Write([]byte(`{"approved":false,"approved_by":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	g := &GitLab{Token: "tok", URL: srv.URL}
	st, ok := g.PullStatus(context.Background(), "acme/api!7")
	if !ok || !st.Ready() {
		t.Fatalf("7: %+v %v", st, ok)
	}
	st, ok = g.PullStatus(context.Background(), "acme/api!8")
	if !ok || st.State != "draft" || st.Checks != "pending" || st.Review != "pending" {
		t.Fatalf("8: %+v", st)
	}
}

func TestPullLogKeepsWhatWasReadAndAnswersByLink(t *testing.T) {
	dir := t.TempDir()
	l := LoadPullLog(dir)
	if err := l.Set("acme/api#7", PullStatus{State: "open", Checks: "passing", Review: "approved", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	again := LoadPullLog(dir)
	if st, ok := again.Get("https://github.com/acme/api/pull/7"); !ok || !st.Ready() {
		t.Fatalf("by link: %+v %v", st, ok)
	}
	if _, ok := again.Get("acme/api#8"); ok {
		t.Fatal("unknown ref answered")
	}
	if filepath.Base(filepath.Dir(again.path)) != "watch" {
		t.Fatalf("path: %s", again.path)
	}
}
