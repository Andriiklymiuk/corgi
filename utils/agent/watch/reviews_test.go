package watch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type forgeCall struct {
	method, path, query, body, auth string
}

func fakeForge(t *testing.T, answers map[string]string) (*httptest.Server, *[]forgeCall) {
	t.Helper()
	var calls []forgeCall
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		calls = append(calls, forgeCall{r.Method, r.URL.Path, r.URL.RawQuery, string(body), r.Header.Get("Authorization") + r.Header.Get("PRIVATE-TOKEN")})
		mu.Unlock()
		for key, answer := range answers {
			method, rest, _ := strings.Cut(key, " ")
			path, query, _ := strings.Cut(rest, "?")
			if method == r.Method && path == r.URL.Path && strings.Contains(r.URL.RawQuery, query) {
				if answer == "500" {
					http.Error(w, "boom", http.StatusInternalServerError)
					return
				}
				_, _ = io.WriteString(w, answer)
				return
			}
		}
		_, _ = io.WriteString(w, "{}")
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestGitHubReviewsAreListedOncePerPull(t *testing.T) {
	item := func(n, title, login string, draft bool, merged string) string {
		mergedAt := "null"
		if merged != "" {
			mergedAt = `"` + merged + `"`
		}
		return `{"title":"` + title + `","html_url":"https://github.com/acme/api/pull/` + n + `","draft":` + map[bool]string{true: "true", false: "false"}[draft] +
			`,"updated_at":"2026-11-2` + n + `T10:00:00Z","user":{"login":"` + login + `"},"pull_request":{"merged_at":` + mergedAt + `}}`
	}
	srv, calls := fakeForge(t, map[string]string{
		"GET /search/issues?author%3A%40me+archived": `{"items":[` + item("1", "Mine", "me", false, "") + `,` + item("2", "Draft", "me", true, "") + `]}`,
		"GET /search/issues?review-requested":        `{"items":[` + item("3", "Theirs", "sam", false, "") + `,` + item("1", "Mine", "me", false, "") + `]}`,
	})
	f := Forge{HTTP: srv.Client(), GitHubAPI: srv.URL, Secrets: Secrets{GitHub: "tok"}}
	rows, err := f.ListReviews(context.Background(), time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC))
	if err != nil || len(rows) != 3 {
		t.Fatalf("rows = %d err = %v", len(rows), err)
	}
	groups := map[string]string{}
	for _, r := range rows {
		groups[r.Ref] = r.Group
	}
	want := map[string]string{"acme/api#1": GroupOpen, "acme/api#2": GroupDraft, "acme/api#3": GroupToReview}
	for ref, group := range want {
		if groups[ref] != group {
			t.Errorf("%s is in %q, want %q", ref, groups[ref], group)
		}
	}
	if rows[0].Ref != "acme/api#3" || rows[0].Repo != "acme/api" {
		t.Errorf("newest first: %+v", rows[0])
	}
	for _, c := range *calls {
		if strings.Contains(c.query, "merged") {
			t.Errorf("merged pull requests are asked for: %s", c.query)
		}
		if c.auth != "Bearer tok" {
			t.Errorf("a call went out without the token: %+v", c)
		}
	}
	if len(*calls) != 2 {
		t.Errorf("calls = %+v", *calls)
	}
}

func TestAReadyPullOfMineIsPlacedInReady(t *testing.T) {
	row := ReviewRow{Mine: true, State: "open"}
	row.Place(PullStatus{State: "open", Checks: "passing", Review: "approved"}, time.Now())
	if row.Group != GroupReady || row.Checks != "passing" {
		t.Fatalf("row = %+v", row)
	}
	now := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	old := ReviewRow{Mine: true, State: "open", Updated: now.AddDate(0, -3, 0)}
	old.Place(PullStatus{State: "open", Checks: "passing", Review: "approved"}, now)
	if old.Group != GroupOld {
		t.Errorf("a pull request nobody touched for three months is %q", old.Group)
	}
	answered := ReviewRow{Answered: true}
	answered.Place(PullStatus{}, now)
	if answered.Group != GroupAnswered {
		t.Errorf("one I approved still waits for me: %q", answered.Group)
	}
}

func TestAListThatFailsSaysWhy(t *testing.T) {
	srv, _ := fakeForge(t, map[string]string{"GET /search/issues?": "500"})
	f := Forge{HTTP: srv.Client(), GitHubAPI: srv.URL, Secrets: Secrets{GitHub: "tok"}}
	if _, err := f.ListReviews(context.Background(), time.Now()); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("err = %v", err)
	}
}

func TestGitLabReviewsAreListed(t *testing.T) {
	var srv *httptest.Server
	mr := func(n, title, state string, draft bool) string {
		return `{"title":"` + title + `","web_url":"` + srv.URL + `/group/proj/-/merge_requests/` + n + `","state":"` + state + `","draft":` +
			map[bool]string{true: "true", false: "false"}[draft] + `,"updated_at":"2026-11-2` + n + `T10:00:00Z","author":{"username":"me"}}`
	}
	answers := map[string]string{"GET /api/v4/user?": `{"username":"me"}`}
	srv, calls := fakeForge(t, answers)
	answers["GET /api/v4/merge_requests?per_page=50&order_by=updated_at&scope=all&state=opened&reviewer_username=me"] = `[` + mr("2", "Theirs", "opened", false) + `,` + mr("4", "Approved", "opened", false) + `]`
	answers["GET /api/v4/merge_requests?scope=created_by_me&state=opened"] = `[` + mr("1", "Mine", "opened", true) + `]`
	answers["GET /api/v4/merge_requests?approved_by_usernames[]=me"] = `[` + mr("4", "Approved", "opened", false) + `]`
	f := Forge{HTTP: srv.Client(), GitHubAPI: "https://api.github.com", Secrets: Secrets{GitLab: "glpat", GitLabURL: srv.URL}}
	rows, err := f.ListReviews(context.Background(), time.Now())
	if err != nil || len(rows) != 3 {
		t.Fatalf("rows = %+v err = %v", rows, err)
	}
	groups := map[string]string{}
	for _, r := range rows {
		groups[r.Ref] = r.Group
	}
	if groups["group/proj!1"] != GroupDraft || groups["group/proj!2"] != GroupToReview || groups["group/proj!4"] != GroupAnswered {
		t.Fatalf("groups = %v", groups)
	}
	if (*calls)[0].auth != "glpat" {
		t.Errorf("the GitLab token is not sent: %+v", (*calls)[0])
	}
}

func TestAGitHubChangeIsReadAndSorted(t *testing.T) {
	srv, _ := fakeForge(t, map[string]string{
		"GET /repos/acme/api/pulls/7?":                  `{"title":"Cap the retries","body":"why","state":"open","draft":true,"commits":2,"mergeable_state":"behind","created_at":"2026-11-20T10:00:00Z","requested_reviewers":[{"login":"lee"}],"user":{"login":"me"},"head":{"ref":"fix/retries","sha":"abc123"},"base":{"ref":"main"}}`,
		"GET /repos/acme/api/pulls/7/reviews":           `[{"state":"COMMENTED","body":"hm","submitted_at":"2026-11-20T11:00:00Z","user":{"login":"sam"}},{"state":"APPROVED","body":"","submitted_at":"2026-11-21T11:00:00Z","user":{"login":"sam"}},{"state":"COMMENTED","body":"thanks","submitted_at":"2026-11-21T12:00:00Z","user":{"login":"sam"}},{"state":"COMMENTED","body":"mine","submitted_at":"2026-11-21T12:30:00Z","user":{"login":"me"}}]`,
		"GET /repos/acme/api/pulls/7/commits":           `[{"sha":"abc123def","commit":{"message":"cap retries\n\nbody","author":{"name":"Me","date":"2026-11-20T09:00:00Z"}},"author":{"login":"me"}}]`,
		"GET /repos/acme/api/commits/abc123/check-runs": `{"check_runs":[{"name":"test","status":"completed","conclusion":"success"},{"name":"lint","status":"completed","conclusion":"failure"},{"name":"e2e","status":"in_progress","conclusion":""},{"name":"test","status":"completed","conclusion":"success"}]}`,
		"GET /repos/acme/api/pulls/7/files":             `[{"filename":"src/retry.ts","status":"modified","additions":2,"deletions":1,"patch":"@@ -1,2 +1,3 @@\n keep\n-old\n+new\n+more\n"},{"filename":"bun.lock","status":"modified","additions":50,"deletions":10,"patch":"@@ -1 +1 @@\n-a\n+b\n"},{"filename":"big.bin","status":"modified","additions":5,"deletions":0}]`,
	})
	f := Forge{HTTP: srv.Client(), GitHubAPI: srv.URL, Secrets: Secrets{GitHub: "tok"}}
	c, err := f.PullChange(context.Background(), "https://github.com/acme/api/pull/7/files")
	if err != nil {
		t.Fatal(err)
	}
	if c.Ref != "acme/api#7" || c.State != "draft" || c.Branch != "fix/retries" || c.Head != "abc123" || c.Total != 68 || c.Eyes != 8 {
		t.Fatalf("change = %+v", c)
	}
	if !c.Behind || c.Commits != 2 || len(c.Reviewers) != 2 || c.Reviewers[0] != (ChangePerson{Name: "sam", State: PersonApproved}) || c.Reviewers[1] != (ChangePerson{Name: "lee", State: PersonWaiting}) {
		t.Fatalf("overview = behind %v commits %d reviewers %+v", c.Behind, c.Commits, c.Reviewers)
	}
	if len(c.CheckRuns) != 3 || c.CheckRuns[0] != (ChangeCheck{Name: "lint", State: CheckFailed}) || c.CheckRuns[1].State != CheckRunning {
		t.Fatalf("checks = %+v", c.CheckRuns)
	}
	if len(c.Activity) != 6 || c.Activity[0].Kind != "commit" || c.Activity[0].Text != "abc123d cap retries" || c.Activity[1].Kind != "opened" || c.Activity[3].Text != "approved" {
		t.Fatalf("activity = %+v", c.Activity)
	}
	if c.Files[0].Path != "src/retry.ts" && c.Files[0].Path != "big.bin" {
		t.Fatalf("first = %s", c.Files[0].Path)
	}
	for _, file := range c.Files {
		if file.Path == "big.bin" && !file.Cut {
			t.Errorf("a file without a patch does not say it is cut")
		}
	}
	if _, err := f.PullChange(context.Background(), "https://example.com/x"); err == nil {
		t.Errorf("any link is read as a pull request")
	}
	if _, err := (Forge{HTTP: srv.Client(), GitHubAPI: srv.URL}).PullChange(context.Background(), "https://github.com/acme/api/pull/7"); err != ErrNoToken {
		t.Errorf("err = %v", err)
	}
}

func TestAGitLabChangeIsRead(t *testing.T) {
	srv, _ := fakeForge(t, map[string]string{
		"GET /api/v4/projects/group/proj/merge_requests/3?":          `{"title":"Rename","description":"d","state":"opened","detailed_merge_status":"need_rebase","created_at":"2026-11-20T10:00:00Z","reviewers":[{"username":"me"},{"username":"lee"}],"head_pipeline":{"status":"failed"},"source_branch":"feat/x","target_branch":"main","author":{"username":"sam"},"diff_refs":{"base_sha":"b","head_sha":"h","start_sha":"s"}}`,
		"GET /api/v4/projects/group/proj/merge_requests/3/approvals": `{"approved_by":[{"user":{"username":"lee"}}]}`,
		"GET /api/v4/projects/group/proj/merge_requests/3/commits":   `[{"short_id":"h1","title":"rename","author_name":"Sam","created_at":"2026-11-20T09:00:00Z"}]`,
		"GET /api/v4/projects/group/proj/merge_requests/3/diffs":     `[{"old_path":"a.go","new_path":"b.go","renamed_file":true,"diff":""},{"old_path":"c.go","new_path":"c.go","new_file":true,"diff":"@@ -0,0 +1,2 @@\n+one\n+two\n"},{"old_path":"d.go","new_path":"d.go","deleted_file":true,"diff":"@@ -1,1 +0,0 @@\n-gone\n"}]`,
	})
	f := Forge{HTTP: srv.Client(), GitHubAPI: "https://api.github.com", Secrets: Secrets{GitLab: "glpat", GitLabURL: srv.URL}}
	link := srv.URL + "/group/proj/-/merge_requests/3"
	c, err := f.PullChange(context.Background(), link)
	if err != nil {
		t.Fatal(err)
	}
	if c.Ref != "group/proj!3" || c.Author != "sam" || c.Head != "h" || c.Total != 3 || c.Eyes != 2 || len(c.Files) != 3 {
		t.Fatalf("change = %+v", c)
	}
	if !c.Behind || c.Commits != 1 || len(c.Reviewers) != 2 || c.Reviewers[0] != (ChangePerson{Name: "lee", State: PersonApproved}) || c.Reviewers[1].State != PersonWaiting {
		t.Fatalf("overview = %+v %+v", c.Reviewers, c)
	}
	if len(c.CheckRuns) != 1 || c.CheckRuns[0] != (ChangeCheck{Name: "pipeline", State: CheckFailed}) || len(c.Activity) != 2 {
		t.Fatalf("checks = %+v activity = %+v", c.CheckRuns, c.Activity)
	}
	if c.Files[0].Path != "c.go" || c.Files[0].Status != "added" {
		t.Fatalf("first = %+v", c.Files[0])
	}
	if _, err := (Forge{HTTP: srv.Client(), Secrets: Secrets{GitLabURL: srv.URL}}).PullChange(context.Background(), link); err != ErrNoToken {
		t.Errorf("err = %v", err)
	}
}

func TestAGitHubReviewGoesOutAsOneReviewWithItsNotes(t *testing.T) {
	srv, calls := fakeForge(t, nil)
	f := Forge{HTTP: srv.Client(), GitHubAPI: srv.URL, Secrets: Secrets{GitHub: "tok"}}
	notes := []ReviewNote{{Path: "src/a.ts", Line: 12, Body: " cap this "}, {Path: "src/a.ts", Line: 4, Side: "-", Body: "why gone?"}, {Path: "", Line: 1, Body: "dropped"}}
	if err := f.SendReview(context.Background(), "https://github.com/acme/api/pull/7", "request", "two things", notes); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0].path != "/repos/acme/api/pulls/7/reviews" {
		t.Fatalf("calls = %+v", *calls)
	}
	var sent struct {
		Event    string `json:"event"`
		Body     string `json:"body"`
		Comments []struct {
			Path, Side, Body string
			Line             int
		} `json:"comments"`
	}
	if err := json.Unmarshal([]byte((*calls)[0].body), &sent); err != nil {
		t.Fatal(err)
	}
	if sent.Event != "REQUEST_CHANGES" || sent.Body != "two things" || len(sent.Comments) != 2 || sent.Comments[0].Body != "cap this" || sent.Comments[1].Side != "LEFT" {
		t.Fatalf("sent = %+v", sent)
	}
	if err := f.SendReview(context.Background(), "https://github.com/acme/api/pull/7", "comment", "", nil); err == nil {
		t.Errorf("an empty comment is sent")
	}
	if err := f.SendReview(context.Background(), "https://github.com/acme/api/pull/7", "shrug", "x", nil); err == nil {
		t.Errorf("an unknown verdict is sent")
	}
	if err := f.SendReview(context.Background(), "https://example.com/x", "approve", "", nil); err == nil {
		t.Errorf("a review goes to any link")
	}
}

func TestAGitLabReviewPostsEachNoteThenTheVerdict(t *testing.T) {
	srv, calls := fakeForge(t, map[string]string{
		"GET /api/v4/projects/group/proj/merge_requests/3?": `{"diff_refs":{"base_sha":"b","head_sha":"h","start_sha":"s"}}`,
	})
	f := Forge{HTTP: srv.Client(), GitHubAPI: "https://api.github.com", Secrets: Secrets{GitLab: "glpat", GitLabURL: srv.URL}}
	link := srv.URL + "/group/proj/-/merge_requests/3"
	notes := []ReviewNote{{Path: "b.go", Was: "a.go", Line: 5, Body: "rename back"}, {Path: "c.go", Line: 2, Side: "-", Body: "keep"}}
	if err := f.SendReview(context.Background(), link, "approve", "nice", notes); err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, c := range *calls {
		paths = append(paths, c.method+" "+c.path[strings.LastIndex(c.path, "/"):])
	}
	if got := strings.Join(paths, ","); got != "GET /3,POST /discussions,POST /discussions,POST /notes,POST /approve" {
		t.Fatalf("calls = %s", got)
	}
	if body := (*calls)[1].body; !strings.Contains(body, `"old_path":"a.go"`) || !strings.Contains(body, `"new_line":5`) || !strings.Contains(body, `"head_sha":"h"`) {
		t.Errorf("first note = %s", body)
	}
	if body := (*calls)[2].body; !strings.Contains(body, `"old_line":2`) {
		t.Errorf("second note = %s", body)
	}
	*calls = nil
	if err := f.SendReview(context.Background(), link, "request", "cap it", nil); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 2 || !strings.Contains((*calls)[0].body, "Changes requested: cap it") || !strings.HasSuffix((*calls)[1].path, "/unapprove") {
		t.Fatalf("calls = %+v", *calls)
	}
}

func TestAFixMessageIsOneLine(t *testing.T) {
	got := FixMessage("https://github.com/acme/api/pull/7", []ReviewNote{{Path: "a.ts", Line: 3, Body: "cap\nthis"}, {Path: "b.ts", Line: 9, Body: " "}}, "overall ok")
	if strings.Contains(got, "\n") || !strings.Contains(got, "(1) overall ok (2) a.ts:3: cap this") {
		t.Fatalf("message = %q", got)
	}
	if FixMessage("x", nil, " ") != "" {
		t.Errorf("an empty review still makes a message")
	}
}

func TestANewForgeFallsBackToTheCLIToken(t *testing.T) {
	old := githubAuthToken
	githubAuthToken = func() string { return "from-gh\n" }
	t.Cleanup(func() { githubAuthToken = old })
	f := NewForge(Secrets{})
	if f.Secrets.GitHub != "from-gh" || f.gitlabAPI() != "https://gitlab.com/api/v4" {
		t.Fatalf("forge = %+v", f)
	}
}
