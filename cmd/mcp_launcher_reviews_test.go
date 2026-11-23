package cmd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/watch"
	"andriiklymiuk/corgi/utils/agent/workspace"
)

type fixedAsker struct{ status watch.PullStatus }

func (a fixedAsker) PullStatus(context.Context, string) (watch.PullStatus, bool) {
	a.status.At = time.Now()
	return a.status, a.status.State != ""
}

func reviewsHome(t *testing.T, status watch.PullStatus) *[]string {
	t.Helper()
	ticketHome(t)
	var posts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			body, _ := io.ReadAll(r.Body)
			posts = append(posts, r.URL.Path+" "+string(body))
			_, _ = io.WriteString(w, `{"merged":true}`)
			return
		}
		q := r.URL.Query().Get("q")
		switch {
		case strings.HasPrefix(r.URL.Path, "/search/issues") && strings.Contains(q, "review-requested"):
			_, _ = io.WriteString(w, `{"items":[{"title":"Theirs","html_url":"https://github.com/acme/api/pull/3","updated_at":"2026-11-21T10:00:00Z","user":{"login":"sam"},"pull_request":{}}]}`)
		case strings.HasPrefix(r.URL.Path, "/search/issues") && strings.Contains(q, "is:open"):
			_, _ = io.WriteString(w, `{"items":[{"title":"Mine","html_url":"https://github.com/acme/api/pull/1","updated_at":"2026-11-22T10:00:00Z","user":{"login":"me"},"pull_request":{}}]}`)
		case strings.HasPrefix(r.URL.Path, "/search/issues"):
			_, _ = io.WriteString(w, `{"items":[]}`)
		case strings.HasSuffix(r.URL.Path, "/files"):
			_, _ = io.WriteString(w, `[{"filename":"src/retry.ts","status":"modified","additions":1,"deletions":0,"patch":"@@ -1,1 +1,2 @@\n keep\n+new\n"}]`)
		default:
			_, _ = io.WriteString(w, `{"title":"Mine","state":"open","user":{"login":"me"},"head":{"ref":"fix/retries","sha":"abc"},"base":{"ref":"main"}}`)
		}
	}))
	t.Cleanup(srv.Close)
	oldForge, oldAsker := forgeFor, pullAskerFor
	forgeFor = func(string, string) watch.Forge {
		return watch.Forge{HTTP: srv.Client(), GitHubAPI: srv.URL, Secrets: watch.Secrets{GitHub: "tok"}}
	}
	pullAskerFor = func(string, string, string) watch.PullAsker { return fixedAsker{status} }
	reviewCache = &reviewStore{status: map[string]watch.PullStatus{}, changes: map[string]reviewChange{}}
	t.Cleanup(func() { forgeFor, pullAskerFor = oldForge, oldAsker })
	return &posts
}

func reviewsCall(t *testing.T, method, target, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if strings.HasPrefix(target, "/launch/reviews/change") {
		launchReviewsChangeHandler(rec, req)
	} else {
		launchReviewsHandler(rec, req)
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func TestTheReviewsListSaysWhatIsReadyAndWhatWaitsForMe(t *testing.T) {
	reviewsHome(t, watch.PullStatus{State: "open", Checks: "passing", Review: "approved"})
	rec, out := reviewsCall(t, http.MethodGet, "/launch/reviews", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d %s", rec.Code, rec.Body)
	}
	rows := out["reviews"].([]any)
	if len(rows) != 2 {
		t.Fatalf("rows = %v", rows)
	}
	groups := map[string]any{}
	for _, r := range rows {
		row := r.(map[string]any)
		groups[row["ref"].(string)] = row["group"]
	}
	if groups["acme/api#1"] != watch.GroupReady || groups["acme/api#3"] != watch.GroupToReview {
		t.Fatalf("groups = %v", groups)
	}
	if again, _ := reviewsCall(t, http.MethodGet, "/launch/reviews", ""); again.Code != http.StatusOK {
		t.Fatalf("the cached list = %d", again.Code)
	}
	if rec, _ := reviewsCall(t, http.MethodDelete, "/launch/reviews", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE = %d", rec.Code)
	}
}

func TestAChangeComesWithItsStatus(t *testing.T) {
	reviewsHome(t, watch.PullStatus{State: "open", Checks: "failing"})
	rec, out := reviewsCall(t, http.MethodGet, "/launch/reviews/change?link=https://github.com/acme/api/pull/1", "")
	if rec.Code != http.StatusOK || out["checks"] != "failing" || out["mine"] != true || out["eyes"].(float64) != 1 {
		t.Fatalf("change = %d %v", rec.Code, out)
	}
	if rec, _ := reviewsCall(t, http.MethodGet, "/launch/reviews/change?link=nope", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("a bad link = %d", rec.Code)
	}
	if rec, _ := reviewsCall(t, http.MethodPost, "/launch/reviews/change", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST = %d", rec.Code)
	}
	if viewerMay(http.MethodGet, "/launch/reviews/change") {
		t.Errorf("a read-only guest may read code")
	}
}

func TestAReviewFromThePhoneIsSentOnce(t *testing.T) {
	posts := reviewsHome(t, watch.PullStatus{State: "open"})
	body := `{"link":"https://github.com/acme/api/pull/3","do":"send","verdict":"request","notes":[{"path":"src/retry.ts","line":2,"body":"cap this"}]}`
	rec, out := reviewsCall(t, http.MethodPost, "/launch/reviews", body)
	if rec.Code != http.StatusOK || out["done"] != "changes requested" {
		t.Fatalf("send = %d %v", rec.Code, out)
	}
	if len(*posts) != 1 || !strings.Contains((*posts)[0], "/repos/acme/api/pulls/3/reviews") || !strings.Contains((*posts)[0], "cap this") {
		t.Fatalf("posts = %v", *posts)
	}
}

func TestMyOwnPullCannotBeApprovedAndSomeoneElsesCannotBeMerged(t *testing.T) {
	posts := reviewsHome(t, watch.PullStatus{State: "open"})
	cases := []struct {
		body string
		code int
	}{
		{`{"link":"https://github.com/acme/api/pull/1","do":"send","verdict":"approve"}`, http.StatusBadRequest},
		{`{"link":"https://github.com/acme/api/pull/3","do":"merge"}`, http.StatusBadRequest},
		{`{"link":"https://github.com/acme/api/pull/3","do":"fix","body":"x"}`, http.StatusBadRequest},
		{`{"link":"https://github.com/acme/api/pull/1","do":"fix"}`, http.StatusBadRequest},
		{`{"link":"https://github.com/acme/api/pull/1","do":"fix","body":"cap it"}`, http.StatusNotFound},
		{`{"link":"https://github.com/acme/api/pull/1","do":"dance"}`, http.StatusBadRequest},
		{`{"link":"https://github.com/acme/other/pull/9","do":"merge"}`, http.StatusNotFound},
		{`not json`, http.StatusBadRequest},
	}
	for _, c := range cases {
		if rec, _ := reviewsCall(t, http.MethodPost, "/launch/reviews", c.body); rec.Code != c.code {
			t.Errorf("%s = %d, want %d: %s", c.body, rec.Code, c.code, rec.Body)
		}
	}
	if len(*posts) != 0 {
		t.Fatalf("something was sent: %v", *posts)
	}
}

func TestSeatsPickTheWorkspaceThatListsTheRepo(t *testing.T) {
	seats := []reviewSeat{{id: "all"}, {id: "api", repos: []string{"Acme/API"}}}
	if got, ok := seatFor(seats, "acme/api"); got != "api" || !ok {
		t.Errorf("seat = %q", got)
	}
	if got, ok := seatFor(seats, "acme/web"); got != "all" || !ok {
		t.Errorf("seat = %q", got)
	}
	if got, ok := seatFor([]reviewSeat{{id: "api", repos: []string{"acme/api"}}}, "acme/web"); ok {
		t.Errorf("a repo no workspace has got the seat %q", got)
	}
}

func TestAWorkspaceListsTheReposOfItsStack(t *testing.T) {
	root := t.TempDir()
	repo := func(dir, remote string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", remote}} {
			cmd := exec.Command("git", args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v %s", args, err, out)
			}
		}
	}
	repo(root, "git@github.com:Acme/stack.git")
	repo(filepath.Join(root, "api"), "https://github.com/acme/api.git")
	repo(filepath.Join(root, "web"), "https://gitlab.example.com/group/sub/web.git")
	compose := "services:\n  api:\n    path: ./api\n  web:\n    path: ./web\n  worker:\n    path: ./api/worker\n  none: {}\n"
	if err := os.MkdirAll(filepath.Join(root, "api", "worker"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "corgi-compose.yml"), []byte(compose), 0o600); err != nil {
		t.Fatal(err)
	}
	got := workspaceProjects(workspace.Workspace{ID: "stack", AbsPath: root})
	if strings.Join(got, ",") != "acme/stack,acme/api,group/sub/web" {
		t.Fatalf("projects = %v", got)
	}
	if got := workspaceProjects(workspace.Workspace{AbsPath: t.TempDir()}); len(got) != 0 {
		t.Fatalf("a folder that is no checkout lists %v", got)
	}
}

func TestReviewsReadAsText(t *testing.T) {
	rows := []watch.ReviewRow{
		{Ref: "acme/api#3", Title: "Theirs", Group: watch.GroupToReview, Checks: "passing", Review: "pending"},
		{Ref: "acme/api#1", Title: "Mine", Group: watch.GroupReady, Checks: "none", Session: "api"},
	}
	got := reviewRowsText(rows)
	if !strings.Contains(got, "Waiting for your review\n  acme/api#3") || !strings.Contains(got, "passing · pending") || strings.Contains(got, "none") {
		t.Fatalf("rows = %q", got)
	}
	if strings.Index(got, "Waiting") > strings.Index(got, "Ready to merge") {
		t.Errorf("what waits for me is not first")
	}
	if !strings.Contains(reviewRowsText(nil), "nobody waits") {
		t.Errorf("an empty list says nothing")
	}
	change := reviewChange{Checks: "failing", Change: watch.Change{Ref: "acme/api#1", Title: "Mine", Total: 90, Eyes: 12,
		Files: []watch.ChangeFile{
			{Path: "db/001.sql", Adds: 9, Eyes: true, Why: "changes the database", Hunks: []watch.ChangeHunk{{Header: "up", Flags: []string{"deletes data"}}}},
			{Path: "bun.lock", Adds: 60},
		},
		Skipped: []watch.ChangeSkip{{Kind: watch.KindLock, Files: 1, Lines: 60}}}}
	text := reviewChangeText(change)
	for _, want := range []string{"12 of 90 changed lines need a person", "failing", "db/001.sql  +9 -0  (changes the database)", "! deletes data in up", "folded away: lock 60"} {
		if !strings.Contains(text, want) {
			t.Errorf("text misses %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "bun.lock") {
		t.Errorf("a folded file is listed")
	}
}
