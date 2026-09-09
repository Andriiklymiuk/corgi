package watch

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

type fakeSource struct {
	name   string
	events []Event
	err    error
	polls  int
}

func (f *fakeSource) Name() string { return f.name }
func (f *fakeSource) Poll(_ context.Context, c Cursor) ([]Event, Cursor, error) {
	f.polls++
	next := Cursor{"at": "t" + string(rune('0'+f.polls))}
	return f.events, next, f.err
}

func TestRulesMatch(t *testing.T) {
	bug := Event{Kind: KindIssueNew, Labels: []string{"Bug"}, State: "Todo", Mine: true}
	cases := []struct {
		name string
		r    Rules
		e    Event
		want bool
	}{
		{"disabled", Rules{}, bug, false},
		{"mine by default", Rules{Enabled: true}, bug, true},
		{"not mine, assignee me", Rules{Enabled: true}, Event{Kind: KindIssueNew}, false},
		{"not mine, assignee any", Rules{Enabled: true, Assignee: "any"}, Event{Kind: KindIssueNew}, true},
		{"label case-insensitive", Rules{Enabled: true, Labels: []string{"bug"}}, bug, true},
		{"label missing", Rules{Enabled: true, Labels: []string{"defect"}}, bug, false},
		{"state", Rules{Enabled: true, States: []string{"Backlog"}}, bug, false},
		{"pr needs prs", Rules{Enabled: true}, Event{Kind: KindPRComment, Mine: true}, false},
		{"pr mine", Rules{Enabled: true, PRs: true}, Event{Kind: KindPRComment, Mine: true}, true},
		{"pr not mine", Rules{Enabled: true, PRs: true}, Event{Kind: KindPRReview}, false},
		{"comment needs comments", Rules{Enabled: true}, Event{Kind: KindIssueComment, Mine: true}, false},
		{"comment", Rules{Enabled: true, Comments: true}, Event{Kind: KindIssueComment, Mine: true}, true},
	}
	for _, tc := range cases {
		if got := tc.r.Match(tc.e); got != tc.want {
			t.Errorf("%s: got %v", tc.name, got)
		}
	}
}

func TestOnceDedupesAndSavesCursors(t *testing.T) {
	dir := t.TempDir()
	src := &fakeSource{name: "fake", events: []Event{
		{Key: "fake:1", Kind: KindIssueNew, Mine: true, Ref: "ABC-1"},
		{Key: "fake:1", Kind: KindIssueNew, Mine: true, Ref: "ABC-1"},
		{Key: "fake:2", Kind: KindPRComment, Mine: true, Ref: "acme/api#2"},
	}}
	var got []string
	w := &Watch{Workspace: "acme", Rules: Rules{Enabled: true}, Sources: []Source{src}, State: LoadState(dir),
		Sink: func(_ context.Context, e Event) { got = append(got, e.Key) }}
	if n := w.Once(context.Background(), time.Now()); n != 0 || len(got) != 0 {
		t.Fatalf("the first round only sets the bookmark, handed %d", n)
	}
	if n := w.Once(context.Background(), time.Now()); n != 1 {
		t.Fatalf("handed %d, want the one unseen match", n)
	}
	if len(got) != 1 || got[0] != "fake:1" {
		t.Fatalf("sink got %v", got)
	}
	if n := w.Once(context.Background(), time.Now()); n != 0 {
		t.Fatalf("third round handed %d", n)
	}
	reloaded := LoadState(dir)
	if reloaded.cursor("acme", "fake")["at"] != "t3" {
		t.Fatalf("cursor not saved: %v", reloaded.Cursors)
	}
	if reloaded.MarkSeen("fake:1") {
		t.Fatal("seen list not saved")
	}
}

func TestErrorsAreRememberedAndBackOff(t *testing.T) {
	dir := t.TempDir()
	src := &fakeSource{name: "fake", err: errors.New("401")}
	var logged []string
	w := &Watch{Workspace: "acme", Rules: Rules{Enabled: true}, Sources: []Source{src}, State: LoadState(dir), Log: func(s string) { logged = append(logged, s) }}
	w.Once(context.Background(), time.Now())
	if !w.failing() || len(logged) != 1 || !strings.Contains(logged[0], "401") {
		t.Fatalf("failing=%v logged=%v", w.failing(), logged)
	}
	if s := w.State.Summaries(); len(s) != 1 || s[0].Error != "401" {
		t.Fatalf("summary %v", s)
	}
}

func TestSeenListIsBounded(t *testing.T) {
	s := LoadState(t.TempDir())
	for i := 0; i < seenKeep+50; i++ {
		s.MarkSeen("k" + string(rune(i)))
	}
	if len(s.Seen) > seenKeep {
		t.Fatalf("seen grew to %d", len(s.Seen))
	}
}

func TestSecretsEnvWins(t *testing.T) {
	dir := t.TempDir()
	if err := SaveSecrets(dir, Secrets{Linear: "file", GitHub: "gh-file"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LINEAR_API_KEY", "env")
	t.Setenv("GITHUB_TOKEN", "")
	s := LoadSecrets(dir)
	if s.Linear != "env" || s.GitHub != "gh-file" {
		t.Fatalf("%+v", s)
	}
	if Fingerprint("") != "none" || len(Fingerprint("x")) != 8 {
		t.Fatal("fingerprint")
	}
}

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyHook(t *testing.T) {
	body := []byte(`{"a":1}`)
	req := func(h map[string]string, query string) *http.Request {
		r, _ := http.NewRequest("POST", "/hooks/x"+query, nil)
		for k, v := range h {
			r.Header.Set(k, v)
		}
		return r
	}
	ok := []struct {
		source string
		r      *http.Request
	}{
		{"linear", req(map[string]string{"Linear-Signature": sign("s", body)}, "")},
		{"github", req(map[string]string{"X-Hub-Signature-256": "sha256=" + sign("s", body)}, "")},
		{"gitlab", req(map[string]string{"X-Gitlab-Token": "s"}, "")},
		{"jira", req(nil, "?token=s")},
	}
	for _, c := range ok {
		if err := VerifyHook(c.source, c.r, body, "s"); err != nil {
			t.Errorf("%s: %v", c.source, err)
		}
		if err := VerifyHook(c.source, c.r, body, "other"); !errors.Is(err, ErrBadSignature) {
			t.Errorf("%s with the wrong secret: %v", c.source, err)
		}
	}
	if err := VerifyHook("linear", ok[0].r, body, ""); err == nil {
		t.Error("no secret must refuse")
	}
	if err := VerifyHook("bitbucket", ok[0].r, body, "s"); err == nil {
		t.Error("unknown source must refuse")
	}
}

func TestParseHooks(t *testing.T) {
	linear := `{"action":"create","type":"Issue","data":{"id":"i1","identifier":"ABC-7","title":"Login loops","url":"https://linear.app/x/ABC-7","createdAt":"2026-09-09T10:00:00.000Z","state":{"name":"Todo"},"labels":[{"name":"Bug"}],"assignee":{"id":"u1","name":"Andrii","email":"a@x.io"}}}`
	r, _ := http.NewRequest("POST", "/", nil)
	events, err := ParseHook("linear", r, []byte(linear), "u1")
	if err != nil || len(events) != 1 {
		t.Fatalf("linear: %v %v", err, events)
	}
	if e := events[0]; e.Key != "linear:ABC-7" || e.Kind != KindIssueNew || !e.Mine || e.Labels[0] != "Bug" || e.State != "Todo" {
		t.Fatalf("linear event %+v", e)
	}

	comment := `{"action":"create","type":"Comment","data":{"id":"c9","body":"please also cover the empty path","createdAt":"2026-09-09T10:05:00.000Z","user":{"id":"u2","name":"Max"},"issue":{"identifier":"ABC-7","title":"Login loops","url":"https://linear.app/x/ABC-7","assignee":{"id":"u1"}}}}`
	events, _ = ParseHook("linear", r, []byte(comment), "u1")
	if len(events) != 1 || events[0].Kind != KindIssueComment || events[0].Author != "Max" || !events[0].Mine {
		t.Fatalf("linear comment %+v", events)
	}
	events, _ = ParseHook("linear", r, []byte(strings.Replace(comment, `"id":"u2"`, `"id":"u1"`, 1)), "u1")
	if len(events) != 0 {
		t.Fatal("my own comment is not an event")
	}

	gh := `{"action":"submitted","repository":{"full_name":"acme/api"},"pull_request":{"number":12,"title":"Referrals","html_url":"https://github.com/acme/api/pull/12","user":{"login":"andrii"}},"review":{"id":5,"body":"nit: name","state":"commented","submitted_at":"2026-09-09T10:00:00Z","user":{"login":"max"}}}`
	r.Header.Set("X-GitHub-Event", "pull_request_review")
	events, _ = ParseHook("github", r, []byte(gh), "andrii")
	if len(events) != 1 || events[0].Key != "github:acme/api#12:r5" || events[0].Kind != KindPRReview || !events[0].Mine {
		t.Fatalf("github %+v", events)
	}

	gl := `{"object_kind":"note","user":{"username":"max"},"project":{"path_with_namespace":"acme/web"},"object_attributes":{"id":44,"note":"looks wrong","noteable_type":"MergeRequest","created_at":"2026-09-09 10:00:00 UTC","url":"https://gitlab.com/acme/web/-/merge_requests/3#note_44"},"merge_request":{"iid":3,"title":"Search"}}`
	events, _ = ParseHook("gitlab", r, []byte(gl), "andrii")
	if len(events) != 1 || events[0].Ref != "acme/web!3" || events[0].Kind != KindPRComment {
		t.Fatalf("gitlab %+v", events)
	}

	jira := `{"webhookEvent":"comment_created","issue":{"key":"ABC-9","self":"https://acme.atlassian.net/rest/api/3/issue/1","fields":{"summary":"Crash","labels":["bug"],"status":{"name":"To Do"},"assignee":{"accountId":"me1"}}},"comment":{"id":"7","created":"2026-09-09T10:00:00.000+0000","author":{"accountId":"u2","displayName":"Max"},"body":{"content":[{"content":[{"text":"repro attached"}]}]}}}`
	events, _ = ParseHook("jira", r, []byte(jira), "me1")
	if len(events) != 1 || events[0].Key != "jira:ABC-9:c7" || events[0].Body != "repro attached" || events[0].URL != "https://acme.atlassian.net/browse/ABC-9" || !events[0].Mine {
		t.Fatalf("jira %+v", events)
	}
	if events, _ := ParseHook("github", r, []byte(`{"action":"opened"}`), ""); len(events) != 0 {
		t.Fatal("other actions are ignored")
	}
}
