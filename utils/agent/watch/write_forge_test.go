package watch

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

type forgeCalls struct {
	mu    sync.Mutex
	calls []string
}

func (f *forgeCalls) add(r *http.Request, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.EscapedPath()+"?"+r.URL.RawQuery+" "+body)
}

func (f *forgeCalls) joined() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.calls, "\n")
}

type toServer struct {
	target *url.URL
	base   http.RoundTripper
}

func (t toServer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.URL.Scheme, r.URL.Host = t.target.Scheme, t.target.Host
	return t.base.RoundTrip(r)
}

func fakeGitHub(t *testing.T, draft bool) *forgeCalls {
	t.Helper()
	calls := &forgeCalls{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls.add(r, string(body))
		switch {
		case r.URL.Path == "/graphql":
			io.WriteString(w, `{"data":{}}`)
		case strings.HasSuffix(r.URL.Path, "/pulls/9") && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusNotFound)
		case strings.HasSuffix(r.URL.Path, "/pulls/7") && r.Method == http.MethodGet:
			if draft {
				io.WriteString(w, `{"node_id":"PR_7","draft":true,"state":"open"}`)
			} else {
				io.WriteString(w, `{"node_id":"PR_7","draft":false,"state":"open"}`)
			}
		case strings.HasSuffix(r.URL.Path, "/merge") && strings.Contains(r.URL.Path, "/pulls/8"):
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, `{"message":"Resource not accessible by integration"}`)
		default:
			io.WriteString(w, `{}`)
		}
	}))
	t.Cleanup(srv.Close)
	target, _ := url.Parse(srv.URL)
	prev := http.DefaultTransport
	base := prev
	if chained, ok := prev.(toServer); ok {
		base = chained.base
	}
	http.DefaultTransport = toServer{target: target, base: base}
	t.Cleanup(func() { http.DefaultTransport = prev })
	return calls
}

func TestGitHubPullRequestVerbsCallTheRightEndpoints(t *testing.T) {
	ctx := context.Background()
	s := Secrets{GitHub: "gh-token"}
	link := "https://github.com/acme/api/pull/7"

	calls := fakeGitHub(t, true)
	if err := ReadyPR(ctx, s, link); err != nil {
		t.Fatal(err)
	}
	if err := ClosePR(ctx, s, link); err != nil {
		t.Fatal(err)
	}
	if err := ReopenPR(ctx, s, link); err != nil {
		t.Fatal(err)
	}
	if err := MergePR(ctx, s, link); err != nil {
		t.Fatal(err)
	}
	if err := ReviewPR(ctx, s, link, "approve", ""); err != nil {
		t.Fatal(err)
	}
	if err := DraftPR(ctx, s, link); err != nil {
		t.Fatal(err)
	}
	got := calls.joined()
	for _, want := range []string{"markPullRequestReadyForReview", `PATCH /repos/acme/api/pulls/7? {"state":"closed"}`, `"state":"open"`, "PUT /repos/acme/api/pulls/7/merge", `"event":"APPROVE"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "convertPullRequestToDraft") {
		t.Errorf("a draft stays a draft without a second call:\n%s", got)
	}

	calls = fakeGitHub(t, false)
	if err := DraftPR(ctx, s, link); err != nil {
		t.Fatal(err)
	}
	if err := ReadyPR(ctx, s, link); err != nil {
		t.Fatal(err)
	}
	if got := calls.joined(); !strings.Contains(got, "convertPullRequestToDraft") || strings.Contains(got, "markPullRequestReadyForReview") {
		t.Errorf("an open one becomes a draft, a ready one is left alone:\n%s", got)
	}

	err := MergePR(ctx, s, "https://github.com/acme/api/pull/8")
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") || !Permanent(err) {
		t.Fatalf("a refusal says why and is permanent: %v", err)
	}
	if err := ReadyPR(ctx, s, "https://github.com/acme/api/pull/9"); err == nil {
		t.Fatal("a pull request that is not there")
	}
}

func TestPullRequestVerbsRefuseWithoutATokenOrALink(t *testing.T) {
	ctx := context.Background()
	gh := "https://github.com/acme/api/pull/7"
	gl := "https://gitlab.com/acme/api/-/merge_requests/7"
	for name, call := range map[string]func(context.Context, Secrets, string) error{
		"ready": ReadyPR, "draft": DraftPR, "reopen": ReopenPR, "merge": MergePR, "close": ClosePR,
	} {
		for _, link := range []string{gh, gl} {
			if err := call(ctx, Secrets{}, link); !errors.Is(err, ErrNoToken) {
				t.Errorf("%s %s without a token: %v", name, link, err)
			}
		}
		if err := call(ctx, Secrets{GitHub: "t", GitLab: "t"}, "https://example.com/x"); err == nil {
			t.Errorf("%s on a link that is no pull request", name)
		}
	}
	if err := ReviewPR(ctx, Secrets{GitHub: "t"}, gh, "maybe", ""); err == nil {
		t.Fatal("an unknown verdict")
	}
	if err := ReviewPR(ctx, Secrets{GitHub: "t"}, gh, "request", " "); err == nil {
		t.Fatal("asking for changes says what to change")
	}
	if err := ClosePR(ctx, Secrets{GitLab: "t", GitLabURL: "https://gitlab.example.com"}, gl); err == nil || !strings.Contains(err.Error(), "token is not sent there") {
		t.Fatalf("the GitLab token stays on its own host: %v", err)
	}
}

func TestGitLabMergeRequestVerbsCallTheRightEndpoints(t *testing.T) {
	calls := &forgeCalls{}
	title := "Draft: Phone field"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls.add(r, string(body))
		if r.Method == http.MethodGet {
			io.WriteString(w, `{"title":"`+title+`","draft":`+map[bool]string{true: "true", false: "false"}[strings.HasPrefix(title, "Draft:")]+`}`)
			return
		}
		io.WriteString(w, `{}`)
	}))
	defer srv.Close()
	ctx := context.Background()
	s := Secrets{GitLab: "gl-token", GitLabURL: srv.URL}
	link := srv.URL + "/acme/api/-/merge_requests/7"
	for _, call := range []func(context.Context, Secrets, string) error{ReadyPR, ClosePR, ReopenPR, MergePR} {
		if err := call(ctx, s, link); err != nil {
			t.Fatal(err)
		}
	}
	title = "Phone field"
	if err := DraftPR(ctx, s, link); err != nil {
		t.Fatal(err)
	}
	got := calls.joined()
	for _, want := range []string{`{"title":"Phone field"}`, "state_event=close", "state_event=reopen", "/merge_requests/7/merge", `{"title":"Draft: Phone field"}`, "/api/v4/projects/acme%2Fapi/"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestJiraWritesAndReadsWhatATicketNeeds(t *testing.T) {
	calls := &forgeCalls{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls.add(r, string(body))
		switch {
		case r.URL.Path == "/rest/api/3/myself":
			io.WriteString(w, `{"accountId":"u1","displayName":"Me"}`)
		case strings.HasSuffix(r.URL.Path, "/comment") && r.Method == http.MethodGet:
			io.WriteString(w, `{"comments":[{"id":"c1","created":"2026-10-23T10:00:00.000+0200","author":{"displayName":"Sam"},"body":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"looks good"}]}]}}]}`)
		case r.URL.Path == "/rest/api/3/issue/ABC-1" && r.Method == http.MethodGet:
			io.WriteString(w, `{"fields":{"status":{"name":"In Review"}}}`)
		case r.URL.Path == "/rest/api/3/issue/ABC-404":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	j := &Jira{URL: srv.URL, Email: "me@example.com", Token: "t", Project: "ABC", Client: srv.Client()}
	if me, err := j.Whoami(ctx); err != nil || me.ID != "u1" || me.Name != "Me" {
		t.Fatalf("whoami: %+v %v", me, err)
	}
	if err := j.Assign(ctx, "ABC-1", "u1"); err != nil {
		t.Fatal(err)
	}
	if err := j.Comment(ctx, "ABC-1", "Picked up"); err != nil {
		t.Fatal(err)
	}
	if err := j.UpdateComment(ctx, "ABC-1", "c 1", "Done"); err != nil {
		t.Fatal(err)
	}
	comments, err := j.RecentComments(ctx, "ABC-1", 5)
	if err != nil || len(comments) != 1 || comments[0].Author != "Sam" || comments[0].Body != "looks good" || comments[0].At.IsZero() {
		t.Fatalf("comments: %+v %v", comments, err)
	}
	if state := j.RefState(ctx, "ABC-1"); state != "In Review" {
		t.Fatalf("state %q", state)
	}
	if state := j.RefState(ctx, "ABC-404"); state != "" {
		t.Fatalf("an unknown ticket has no state: %q", state)
	}
	got := calls.joined()
	for _, want := range []string{`PUT /rest/api/3/issue/ABC-1/assignee? {"accountId":"u1"}`, "POST /rest/api/3/issue/ABC-1/comment", "PUT /rest/api/3/issue/ABC-1/comment/c%201", "orderBy=-created"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if err := (&Jira{URL: srv.URL, Client: srv.Client()}).Assign(ctx, "ABC-404", "u1"); err != nil {
		t.Fatalf("a 204 is fine: %v", err)
	}
	if _, err := (&Jira{URL: srv.URL}).Statuses(ctx); err == nil {
		t.Fatal("statuses need a project")
	}
}

func TestLinearWritesAndReadsWhatATicketNeeds(t *testing.T) {
	calls := &forgeCalls{}
	refuse := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		q := string(body)
		calls.add(r, q)
		switch {
		case strings.Contains(q, "workflowStates"):
			io.WriteString(w, `{"data":{"workflowStates":{"nodes":[{"id":"s1","name":"Todo"},{"id":"s2","name":"In Review"}]}}}`)
		case strings.Contains(q, "viewer"):
			io.WriteString(w, `{"data":{"viewer":{"id":"u1","name":"Me"}}}`)
		case strings.Contains(q, "comments(last"):
			io.WriteString(w, `{"data":{"issue":{"comments":{"nodes":[{"id":"c1","body":"ship it","createdAt":"2026-10-23T08:00:00.000Z","user":{"name":"Sam"}},{"id":"c2","body":"bot","createdAt":"2026-10-23T09:00:00.000Z"}]}}}}`)
		case strings.Contains(q, "state { name }"):
			io.WriteString(w, `{"data":{"issue":{"state":{"name":"In Review"}}}}`)
		case strings.Contains(q, `issue(id: \"ABC-404\")`):
			io.WriteString(w, `{"data":{"issue":null}}`)
		case strings.Contains(q, "issue(id:"):
			io.WriteString(w, `{"data":{"issue":{"id":"uuid-1"}}}`)
		case strings.Contains(q, "issueUpdate"):
			io.WriteString(w, `{"data":{"issueUpdate":{"success":`+map[bool]string{true: "false", false: "true"}[refuse]+`}}}`)
		case strings.Contains(q, "commentCreate"):
			io.WriteString(w, `{"data":{"commentCreate":{"success":true}}}`)
		case strings.Contains(q, "commentUpdate"):
			io.WriteString(w, `{"data":{"commentUpdate":{"success":true}}}`)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	l := &Linear{Token: "t", Team: "ABC", URL: srv.URL, Client: srv.Client()}
	if me, err := l.Whoami(ctx); err != nil || me.ID != "u1" {
		t.Fatalf("whoami: %+v %v", me, err)
	}
	if err := l.Move(ctx, "ABC-1", "in review"); err != nil {
		t.Fatal(err)
	}
	if err := l.Move(ctx, "ABC-1", "Shipped"); err == nil || !strings.Contains(err.Error(), "Todo, In Review") {
		t.Fatalf("an unknown status lists the real ones: %v", err)
	}
	if err := l.Assign(ctx, "ABC-1", "u1"); err != nil {
		t.Fatal(err)
	}
	if err := l.Comment(ctx, "ABC-1", "Picked up"); err != nil {
		t.Fatal(err)
	}
	if err := l.UpdateComment(ctx, "ABC-1", "c1", "Done"); err != nil {
		t.Fatal(err)
	}
	comments, err := l.RecentComments(ctx, "ABC-1", 5)
	if err != nil || len(comments) != 2 || comments[0].Author != "Sam" || comments[1].Author != "" {
		t.Fatalf("comments: %+v %v", comments, err)
	}
	if state := l.RefState(ctx, "ABC-1"); state != "In Review" {
		t.Fatalf("state %q", state)
	}
	if err := l.Comment(ctx, "ABC-404", "x"); err == nil {
		t.Fatal("no issue, no comment")
	}
	refuse = true
	if err := l.Assign(ctx, "ABC-1", "u1"); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("a refused change says so: %v", err)
	}
	got := calls.joined()
	for _, want := range []string{`stateId: \"s2\"`, `assigneeId: \"u1\"`, "commentCreate", "commentUpdate", `key: {eq: \"ABC\"}`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestWriteFixNamesWhatToDoAboutARefusal(t *testing.T) {
	for msg, want := range map[string]string{
		"merging https://github.com/a/b/pull/1: HTTP 403: insufficient_scope": "repo scope",
		"merging https://gitlab.com/a/-/merge_requests/1: insufficient_scope": "api scope",
		"HTTP 401: bad credentials": "refused",
		"HTTP 403: forbidden":       "maintainer role",
		"HTTP 500: oops":            "",
	} {
		if got := WriteFix(errors.New(msg)); !strings.Contains(got, want) || (want == "" && got != "") {
			t.Errorf("%q: %q", msg, got)
		}
	}
	if WriteFix(nil) != "" || Permanent(errors.New("HTTP 502")) {
		t.Fatal("no error, no fix; a bad gateway passes")
	}
	if WriterFor(Secrets{JiraToken: "t", JiraURL: "https://x.atlassian.net"}, "Jira", "ABC") == nil || WriterFor(Secrets{Linear: "t"}, "linear", "ABC") == nil || WriterFor(Secrets{}, "jira", "ABC") != nil {
		t.Fatal("a writer only with its token")
	}
}
