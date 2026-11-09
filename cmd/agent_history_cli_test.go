package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestTheBoardIsReconciledFromTheCommandLineAndTheHistoryTellsIt(t *testing.T) {
	dir, _ := watchFixture(t, nil)
	now := time.Now()
	old := now.Add(-15 * 24 * time.Hour)
	fixes := watch.LoadFixLog(dir)
	stale := watch.Event{Key: "k-old", Ref: "ABC-1", Workspace: "acme-stack", Kind: watch.KindIssueComment, Title: "old one", At: old}
	fixes.StartFor(stale, old)
	fixes.SetHarness("k-old", "codex")
	fixes.SetModel("k-old", "gpt-5-codex")
	fixes.SetCost("k-old", 0.25, 4000)
	fixes.Finish("k-old", nil, "", "", old.Add(5*time.Minute))
	fresh := watch.Event{Key: "k-new", Ref: "ABC-2", Workspace: "acme-stack", Kind: watch.KindIssueNew, Title: "new one", State: "Todo", At: now}
	fixes.StartFor(fresh, now.Add(-time.Minute))
	fixes.SetModel("k-new", "opus-4-1")
	tasks := watch.LoadTasks(dir)
	for _, title := range []string{"schema", "api"} {
		if _, err := tasks.Add(title, "", "acme-stack", "cli", now); err != nil {
			t.Fatal(err)
		}
	}
	_, _ = tasks.Move("TASK-1", "Done", now)
	if _, err := watch.LoadPlans(dir).Add("Ship it", "", "acme-stack", "", "cli", []watch.PlanTask{{ID: 1}, {ID: 2, After: []int{1}}}, 1, now); err != nil {
		t.Fatal(err)
	}

	out, code := cli(t, "agent", "kanban")
	if code != 0 || !strings.Contains(out, "Features (1)") || !strings.Contains(out, "P-1  Ship it") || !strings.Contains(out, "TASK-1 → TASK-2") {
		t.Fatalf("the board names the plan and its order: %d %s", code, out)
	}
	if !strings.Contains(out, "now: a run is on it (opus-4-1)") || !strings.Contains(out, "a run answered") || !strings.Contains(out, "gpt-5-codex") {
		t.Fatalf("the live line and the models: %s", out)
	}
	if out, code := cli(t, "--json", "agent", "kanban"); code != 0 || !strings.Contains(out, `"features"`) || !strings.Contains(out, `"waves"`) {
		t.Fatalf("json: %d %s", code, out)
	}

	if out, code := cli(t, "agent", "kanban", "reconcile", "--dry-run"); code != 0 || !strings.Contains(out, "would: ABC-1 (Inbox)") || strings.Contains(out, "ABC-2") {
		t.Fatalf("a dry run names the stale card only: %d %s", code, out)
	}
	if _, code := cli(t, "agent", "kanban", "reconcile", "--keep", "soon"); code == 0 {
		t.Fatal("a bad age is refused")
	}
	if out, code := cli(t, "--json", "agent", "kanban", "reconcile"); code != 0 || !strings.Contains(out, `"left"`) || !strings.Contains(out, "ABC-1") {
		t.Fatalf("reconcile: %d %s", code, out)
	}
	if out, code := cli(t, "agent", "kanban"); strings.Contains(out, "ABC-1") || code != 0 {
		t.Fatalf("the card left: %s", out)
	}
	if out, code := cli(t, "agent", "kanban", "reconcile"); code != 0 || !strings.Contains(out, "the board is current") {
		t.Fatalf("nothing more to let go: %d %s", code, out)
	}
	if out, code := cli(t, "agent", "kanban", "reconcile", "--back", "ABC-1"); code != 0 || !strings.Contains(out, "ABC-1 is back") {
		t.Fatalf("back: %d %s", code, out)
	}
	if out, _ := cli(t, "agent", "kanban"); !strings.Contains(out, "ABC-1") {
		t.Fatalf("and it shows again: %s", out)
	}
	if _, code := cli(t, "agent", "kanban", "reconcile", "--back", "ABC-9"); code == 0 {
		t.Fatal("what was never archived cannot come back")
	}
	if lines := reconcileBoard(dir)(now); len(lines) != 1 || !strings.HasPrefix(lines[0], "ABC-1") {
		t.Fatalf("the daemon's hourly pass does the same: %v", lines)
	}

	out, code = cli(t, "agent", "history", "--days", "30")
	if code != 0 || !strings.Contains(out, "ABC-1") || !strings.Contains(out, "left the board") || !strings.Contains(out, "gpt-5-codex") || !strings.Contains(out, "TASK-1") {
		t.Fatalf("history: %d %s", code, out)
	}
	if out, code := cli(t, "agent", "history", "--days", "30", "--workspace", "other"); code != 0 || !strings.Contains(out, "nothing in the last 30 days") {
		t.Fatalf("another workspace has nothing: %d %s", code, out)
	}
	if out, code := cli(t, "--json", "agent", "history"); code != 0 || !strings.Contains(out, `"entries"`) {
		t.Fatalf("json history: %d %s", code, out)
	}
	if line := historyLine(HistoryEntry{At: now, Ref: "X-1", Kind: HistRun, Why: "pushed", Took: "3m", Cost: &CardCost{Tokens: 1500, Models: []string{"opus-4-1"}}}); !strings.Contains(line, "run: pushed") || !strings.Contains(line, "2k tok") || !strings.Contains(line, "opus-4-1") || !strings.HasSuffix(line, "3m") {
		t.Fatalf("history line: %q", line)
	}
	if line := featureLine(KanbanFeature{Ref: "P-9", Title: "Big", State: "done", Why: "every card is done", Cost: &CardCost{Tokens: 10}}); !strings.Contains(line, "P-9  Big · done · every card is done · 10 tok") {
		t.Fatalf("feature line: %q", line)
	}
}

func TestThePhoneReadsFeaturesAndTheHistory(t *testing.T) {
	dir := phoneBoard(t, false)
	now := time.Now()
	tasks := watch.LoadTasks(dir)
	if _, err := tasks.Add("one", "", "api", "phone", now); err != nil {
		t.Fatal(err)
	}
	_, _ = tasks.Move("TASK-1", "Done", now)
	if _, err := watch.LoadPlans(dir).Add("Goal", "", "api", "", "cli", []watch.PlanTask{{ID: 1}}, 1, now); err != nil {
		t.Fatal(err)
	}
	_ = watch.LoadArchive(dir).Put(watch.Archived{Key: "k", Ref: "ABC-7", Workspace: "api", Column: ColInbox, Why: "quiet", At: now})

	rec := httptest.NewRecorder()
	launchKanbanHandler(rec, httptest.NewRequest(http.MethodGet, "/launch/kanban", nil))
	var kb struct {
		Features []KanbanFeature `json:"features"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &kb)
	if rec.Code != 200 || len(kb.Features) != 1 || kb.Features[0].Ref != "P-1" || kb.Features[0].State != "done" {
		t.Fatalf("features on the phone: %d %+v", rec.Code, kb.Features)
	}
	rec = httptest.NewRecorder()
	launchHistoryHandler(rec, httptest.NewRequest(http.MethodGet, "/launch/history?days=x", nil))
	var hist struct {
		Entries []HistoryEntry `json:"entries"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &hist)
	refs := map[string]string{}
	for _, e := range hist.Entries {
		refs[e.Ref] = e.Kind
	}
	if rec.Code != 200 || refs["ABC-7"] != HistLeft || refs["TASK-1"] != HistDone {
		t.Fatalf("history on the phone: %d %+v", rec.Code, refs)
	}
	rec = httptest.NewRecorder()
	launchHistoryHandler(rec, httptest.NewRequest(http.MethodGet, "/launch/history?days=1&workspace=other", nil))
	_ = json.Unmarshal(rec.Body.Bytes(), &hist)
	if len(hist.Entries) != 0 {
		t.Fatalf("another workspace: %+v", hist.Entries)
	}
}
