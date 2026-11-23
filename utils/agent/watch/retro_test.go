package watch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAReportThatTalksAboutAProblemIsNotARunThatHadIt(t *testing.T) {
	now := time.Now()
	run := func(key, ws, ref string, kind Kind, gaps ...Gap) FixRecord {
		return FixRecord{Key: key, Workspace: ws, Ref: ref, Kind: string(kind), StartedAt: now.Add(-time.Hour), FinishedAt: now, Gaps: gaps}
	}
	recs := []FixRecord{
		run("1", "acme", "acme/api#1", KindPRComment, GapNoDatabase),
		run("2", "acme", "routine/retro", KindRoutine, GapNoDatabase),
		run("3", "web", "routine/digest", KindRoutine, GapNoDatabase),
		run("4", "web", "routine/babysit-pr", KindRoutine, GapNoTool),
	}
	all := strings.Join(SelfCheckOf(recs, nil, now, 7).Lines, "\n")
	if !strings.Contains(all, "1 run skipped a check: no database (acme acme/api#1)") {
		t.Errorf("only the run that skipped it, by name:\n%s", all)
	}
	if !strings.Contains(all, "tool not installed (web routine/babysit-pr)") {
		t.Errorf("a routine that works on code still counts:\n%s", all)
	}
	own := SelfCheckOf(RecordsOf(recs, "web"), nil, now, 7)
	if strings.Contains(strings.Join(own.Lines, "\n"), "no database") || own.Runs != 2 {
		t.Errorf("one workspace's own runs: %+v", own)
	}
	if len(RecordsOf(recs, "")) != 4 {
		t.Error("no workspace is every workspace")
	}
	if got := someRuns([]string{"a", "b", "c", "d", "e", "f"}); got != "a, b, c, d and 2 more" {
		t.Errorf("%q", got)
	}
}

func TestBackfillLeavesAReportAlone(t *testing.T) {
	l := LoadFixLog(t.TempDir())
	now := time.Now()
	for key, e := range map[string]Event{"r": {Key: "r", Ref: "routine/retro", Kind: KindRoutine}, "f": {Key: "f", Ref: "ABC-1", Kind: KindIssueNew}} {
		l.StartFor(e, now.Add(-time.Hour))
		l.Finish(key, nil, "", "", now)
	}
	l.Backfill(func(string) string { return "## You do next\n- Press Work.\nThere was no database." }, nil, now)
	for _, r := range l.Records() {
		if report := r.Key == "r"; report != (len(r.Left) == 0 && len(r.Gaps) == 0) {
			t.Errorf("%s: %v %v", r.Key, r.Left, r.Gaps)
		}
	}
}

func TestTheRetroKillsACauseBeforeItFilesIt(t *testing.T) {
	k, ok := CatalogKind("retro")
	if !ok {
		t.Fatal("no retro")
	}
	for _, want := range []string{"Nothing filed is a good retro", "selfcheck --workspace .", "--show <session>:<line>", "The replay", "canceled one", "corgi feedback note", "corgi feedback list --json", "No list of things for the person"} {
		if !strings.Contains(k.Prompt, want) {
			t.Errorf("missing %q", want)
		}
	}
	if !ReadOnlyRoutine(Event{Kind: KindRoutine, Title: "retro", Body: k.Prompt}) {
		t.Error("it still reads only")
	}
}

func TestAWatchLeavesAnEventAnotherWorkspaceOwns(t *testing.T) {
	st := LoadState(filepath.Join(t.TempDir(), "state.json"))
	took := 0
	w := &Watch{Workspace: "any", Rules: Rules{Enabled: true, PRs: true}, State: st, Sink: func(context.Context, Event) { took++ },
		Elsewhere: func(e Event) bool { return e.Ref == "acme/api#1" }}
	theirs := Event{Key: "github:1", Kind: KindPRReview, Ref: "acme/api#1", Mine: true, Body: "please fix"}
	if w.Handle(context.Background(), theirs) || took != 0 {
		t.Fatal("it is theirs")
	}
	if !st.MarkSeen(theirs.Key) {
		t.Fatal("and not marked seen, so its owner still gets it")
	}
	if !w.Handle(context.Background(), Event{Key: "github:2", Kind: KindPRReview, Ref: "else/where#1", Mine: true, Body: "please fix"}) || took != 1 {
		t.Fatal("what nobody owns is taken")
	}
}

func TestJiraReadsAWeekOfChangelogsInOneAnswer(t *testing.T) {
	long := strings.Repeat("x", 5<<20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/myself" {
			_, _ = w.Write([]byte(`{"accountId":"me","timeZone":"UTC"}`))
			return
		}
		_, _ = w.Write([]byte(`{"pad":"` + long + `","issues":[{"key":"OPS-1","fields":{"summary":"s"},"changelog":{"histories":[
			{"created":"2026-11-20T10:00:00.000+0000","author":{"displayName":"Kim"},"items":[{"field":"status","fromString":"A","toString":"B"}]}]}}]}`))
	}))
	defer srv.Close()
	j := &Jira{URL: srv.URL, Email: "me@acme.io", Token: "t", Client: srv.Client()}
	moves, err := j.Moves(context.Background(), time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC))
	if err != nil || len(moves) != 1 {
		t.Fatalf("an answer over 4 MB is read whole: %v %v", moves, err)
	}
}

func TestTasksFiledByNamesOnlyTheRunsOwn(t *testing.T) {
	now := time.Now()
	tasks := []Task{
		{ID: 1, Workspace: "a", CreatedAt: now.Add(-time.Hour)},
		{ID: 2, Workspace: "a", Title: "Retro:  one cause", CreatedAt: now},
		{ID: 21, Workspace: "a", CreatedAt: now},
	}
	got := TasksFiledBy(tasks, "a", now.Add(-time.Minute), "TASK-1 and TASK-2.")
	if len(got) != 1 || got[0].ID != 2 || TaskHeadline(got[0]) != "one cause" {
		t.Fatalf("%+v", got)
	}
	if TasksFiledBy(tasks, "a", time.Time{}, "TASK-2") != nil {
		t.Fatal("a run with no start filed nothing")
	}
}
