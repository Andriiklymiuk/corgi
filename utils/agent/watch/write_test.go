package watch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A ticket key or a comment is someone else's text. It must never be able to
// close the GraphQL string it sits in and add fields of its own.
func TestGraphQLValuesAreQuotedNotPasted(t *testing.T) {
	nasty := `x") { id } } mutation { issueDelete(id: "y`
	got := jsonString(nasty)
	if strings.Contains(strings.ReplaceAll(got[1:len(got)-1], `\"`, ""), `"`) {
		t.Fatalf("an unescaped quote would close the string early: %s", got)
	}
	var back string
	if err := json.Unmarshal([]byte(got), &back); err != nil || back != nasty {
		t.Fatalf("quoting must round-trip: %v %q", err, back)
	}
	if jsonString(`a"b`) != `"a\"b"` {
		t.Fatalf("quotes are escaped: %s", jsonString(`a"b`))
	}
}

func TestJiraCommentBodyIsADFParagraphs(t *testing.T) {
	doc := adf("first\n\nthird")
	raw, _ := json.Marshal(doc)
	var back struct {
		Type    string `json:"type"`
		Version int    `json:"version"`
		Content []struct {
			Type    string `json:"type"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.Type != "doc" || back.Version != 1 {
		t.Fatalf("jira cloud takes a versioned doc, got %+v", back)
	}
	if len(back.Content) != 3 {
		t.Fatalf("one paragraph per line, blank ones kept: %d", len(back.Content))
	}
	if back.Content[0].Content[0].Text != "first" || back.Content[2].Content[0].Text != "third" {
		t.Fatalf("the text must survive: %+v", back.Content)
	}
	if len(back.Content[1].Content) != 0 {
		t.Fatal("an empty line is an empty paragraph, not a text node")
	}
}

// jiraStub answers the three calls a move makes.
func jiraStub(t *testing.T, transitions string, record *string) *Jira {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/transitions") && r.Method == http.MethodGet:
			io.WriteString(w, transitions)
		case strings.HasSuffix(r.URL.Path, "/transitions") && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			*record = string(body)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return &Jira{URL: srv.URL, Email: "me@example.com", Token: "t", Project: "ABC", Client: srv.Client()}
}

func TestJiraMovePicksTheTransitionByTargetStatus(t *testing.T) {
	const offered = `{"transitions":[{"id":"31","name":"Start work","to":{"name":"In Progress"}},{"id":"41","name":"Ship","to":{"name":"Ready for staging"}}]}`
	var posted string
	j := jiraStub(t, offered, &posted)

	if err := j.Move(context.Background(), "ABC-1", "in progress"); err != nil {
		t.Fatalf("the target status names the transition, case aside: %v", err)
	}
	if !strings.Contains(posted, `"id":"31"`) {
		t.Fatalf("it must post the transition id, not the status name: %s", posted)
	}

	if err := j.Move(context.Background(), "ABC-1", "Ship"); err != nil {
		t.Fatalf("the transition's own name works too: %v", err)
	}
	if !strings.Contains(posted, `"id":"41"`) {
		t.Fatalf("wrong transition: %s", posted)
	}
}

func TestJiraMoveSaysWhereItCouldHaveGone(t *testing.T) {
	const offered = `{"transitions":[{"id":"31","name":"Start work","to":{"name":"In Progress"}}]}`
	var posted string
	j := jiraStub(t, offered, &posted)

	err := j.Move(context.Background(), "ABC-1", "Done")
	if err == nil {
		t.Fatal("a status the workflow does not offer from here is an error")
	}
	if !strings.Contains(err.Error(), "In Progress") {
		t.Fatalf("the error must list the legal moves: %v", err)
	}
	if posted != "" {
		t.Fatal("nothing may be written when no transition matched")
	}
}

func TestJiraStatusesAreDedupedAcrossIssueTypes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `[{"statuses":[{"id":"1","name":"To Do"},{"id":"2","name":"Done"}]},
			{"statuses":[{"id":"1","name":"To Do"},{"id":"3","name":"In Review"}]}]`)
	}))
	defer srv.Close()
	j := &Jira{URL: srv.URL, Project: "ABC", Client: srv.Client()}

	got, err := j.Statuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("a status shared by two issue types is one column: %+v", got)
	}
}

func TestBoardCacheRemembersColumnsAndForgetsAWorkspace(t *testing.T) {
	dir := t.TempDir()
	c := LoadBoardCache(dir)
	if c.Get("api").Has() {
		t.Fatal("an empty cache has no columns")
	}
	info := BoardInfo{Tracker: "jira", Project: "ABC", FetchedAt: time.Now(),
		Statuses: []Status{{ID: "1", Name: "In Progress"}}, Me: Identity{ID: "u1", Name: "Me"}}
	if err := c.Set("api", info); err != nil {
		t.Fatal(err)
	}

	again := LoadBoardCache(dir).Get("api")
	if !again.Has() || again.Statuses[0].Name != "In Progress" || again.Me.Name != "Me" {
		t.Fatalf("the columns must survive a reload: %+v", again)
	}
	if again.Stale(time.Now()) {
		t.Fatal("a board read just now is not stale")
	}
	if !again.Stale(time.Now().Add(BoardMaxAge + time.Hour)) {
		t.Fatal("a board older than the window is stale")
	}

	if err := LoadBoardCache(dir).Forget("api"); err != nil {
		t.Fatal(err)
	}
	if LoadBoardCache(dir).Get("api").Has() {
		t.Fatal("forgetting must reach the file, so a later watch does not inherit it")
	}
}

func TestWriterForNeedsATokenForThatTracker(t *testing.T) {
	if WriterFor(Secrets{}, "jira", "ABC") != nil {
		t.Fatal("no token, no writer")
	}
	if WriterFor(Secrets{JiraToken: "t"}, "jira", "ABC") != nil {
		t.Fatal("a jira token with no site URL cannot write anywhere")
	}
	if WriterFor(Secrets{JiraToken: "t", JiraURL: "https://x.atlassian.net"}, "jira", "ABC") == nil {
		t.Fatal("a token and a site is a writer")
	}
	if WriterFor(Secrets{Linear: "lin_api_x"}, "linear", "TEAM") == nil {
		t.Fatal("a linear token is a writer")
	}
	if WriterFor(Secrets{Linear: "x"}, "github", "") != nil {
		t.Fatal("only trackers with columns are writers")
	}
}

func TestUndraftTitleDropsEveryDraftMarker(t *testing.T) {
	for in, want := range map[string]string{
		"Draft: Search":         "Search",
		"WIP: [Draft] Search":   "Search",
		"Search":                "Search",
		"  draft: (wip) Search": "Search",
	} {
		if got := undraftTitle(in); got != want {
			t.Errorf("undraftTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

// A review from the phone or the CLI: GitHub takes it as one review call
// with the verdict; GitLab as a note (changes requested say so) and an
// approve or unapprove. A request without words is refused before any call.
func TestReviewPRSpeaksBothForges(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(body))
		w.WriteHeader(200)
	}))
	defer srv.Close()
	ctx := context.Background()
	s := Secrets{GitLab: "glpat"}
	link := srv.URL + "/group/proj/-/merge_requests/7"
	if err := ReviewPR(ctx, s, link, ReviewApprove, "nice"); err != nil {
		t.Fatal(err)
	}
	if err := ReviewPR(ctx, s, link, ReviewRequest, "cap the retries"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		`POST /api/v4/projects/group/proj/merge_requests/7/notes {"body":"nice"}`,
		`POST /api/v4/projects/group/proj/merge_requests/7/approve `,
		`POST /api/v4/projects/group/proj/merge_requests/7/notes {"body":"Changes requested: cap the retries"}`,
		`POST /api/v4/projects/group/proj/merge_requests/7/unapprove `,
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("gitlab calls:\n%s", strings.Join(calls, "\n"))
	}
	if err := ReviewPR(ctx, s, link, ReviewRequest, "  "); err == nil || !strings.Contains(err.Error(), "say what to change") {
		t.Fatalf("a request needs words: %v", err)
	}
	if err := ReviewPR(ctx, s, link, "ship", "x"); err == nil {
		t.Fatal("unknown verdict")
	}
	if err := ReviewPR(ctx, Secrets{}, "https://github.com/acme/api/pull/7", ReviewApprove, ""); err != ErrNoToken {
		t.Fatalf("github without a token: %v", err)
	}
}
