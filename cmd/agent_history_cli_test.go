package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestTheBoardIsReconciledFromTheCommandLineAndTheHistoryTellsIt(t *testing.T) {
	dir, ws := watchFixture(t, nil)
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
	if _, err := watch.LoadPlans(dir).Add(watch.Plan{Goal: "Ship it", Workspace: "acme-stack", By: "cli", Tasks: []watch.PlanTask{{ID: 1}, {ID: 2, After: []int{1}}}, Slots: 1}, now); err != nil {
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
	runs := filepath.Join(dir, "watch", "runs")
	_ = os.MkdirAll(runs, 0o700)
	_ = os.WriteFile(filepath.Join(runs, "k-old.log"), []byte("=== start\nread the diff\nOutcome: nothing\n"), 0o600)
	if out, code := cli(t, "agent", "history", "log", "abc-1"); code != 0 || !strings.Contains(out, "Outcome: nothing") {
		t.Fatalf("a run's log by its ref: %d %s", code, out)
	}
	if out, code := cli(t, "agent", "history", "log", "ABC-2", "--tail", "1"); code != 0 || !strings.Contains(out, "log is gone") {
		t.Fatalf("a run with no log says so: %d %s", code, out)
	}
	if _, code := cli(t, "agent", "history", "log", "ABC-404"); code != 2 {
		t.Fatal("no run on a ref is exit 2")
	}
	inWorkspace(t, ws)
	if out, code := cli(t, "--json", "agent", "history", "log", "ABC-1", "--workspace", "."); code != 0 || !strings.Contains(out, `"workspace": "acme-stack"`) {
		t.Fatalf("--workspace . is the one you are in: %d %s", code, out)
	}
	if out, code := cli(t, "agent", "history", "--days", "30", "--workspace", "."); code != 0 || !strings.Contains(out, "ABC-1") {
		t.Fatalf("history --workspace .: %d %s", code, out)
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
	if _, err := watch.LoadPlans(dir).Add(watch.Plan{Goal: "Goal", Workspace: "api", By: "cli", Tasks: []watch.PlanTask{{ID: 1}}, Slots: 1}, now); err != nil {
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

func TestHistoryKeepsOnlyTheKindAsked(t *testing.T) {
	dir, _ := watchFixture(t, nil)
	now := time.Now()
	fixes := watch.LoadFixLog(dir)
	fixes.StartFor(watch.Event{Key: "k-run", Ref: "ABC-3", Workspace: "acme-stack", Kind: watch.KindIssueNew, Title: "a run", At: now}, now.Add(-time.Minute))
	fixes.Finish("k-run", nil, "", "", now)
	tasks := watch.LoadTasks(dir)
	for _, title := range []string{"finished", "dropped"} {
		if _, err := tasks.Add(title, "", "acme-stack", "cli", now); err != nil {
			t.Fatal(err)
		}
	}
	_, _ = tasks.Move("TASK-1", "Done", now)
	_, _ = tasks.Move("TASK-2", "Canceled", now)
	_ = watch.LoadArchive(dir).Put(watch.Archived{Key: "k-left", Ref: "ABC-7", Workspace: "acme-stack", Column: ColInbox, Why: "quiet", At: now})

	for kind, want := range map[string]string{"run": "ABC-3", "done": "TASK-1", "canceled": "TASK-2", "left": "ABC-7"} {
		out, code := cli(t, "agent", "history", "--kind", kind)
		if code != 0 || !strings.Contains(out, want) {
			t.Fatalf("--kind %s keeps %s: %d %s", kind, want, code, out)
		}
		for _, other := range []string{"ABC-3", "TASK-1", "TASK-2", "ABC-7"} {
			if other != want && strings.Contains(out, other) {
				t.Fatalf("--kind %s drops %s: %s", kind, other, out)
			}
		}
	}
	if out, code := cli(t, "agent", "history", "--kind", "merged"); code != 0 || !strings.Contains(out, "nothing in the last 14 days") {
		t.Fatalf("no merges: %d %s", code, out)
	}
	out, code := cli(t, "--json", "agent", "history", "--kind", "left")
	var got struct {
		Entries []HistoryEntry `json:"entries"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || code != 0 || len(got.Entries) != 1 || got.Entries[0].Kind != HistLeft {
		t.Fatalf("json --kind left: %d %v %s", code, err, out)
	}
	if out, code := cli(t, "--json", "agent", "history", "--kind", "lost"); code != 2 || !strings.Contains(out, "run, merged, done, canceled, left") {
		t.Fatalf("an unknown kind is exit 2 with the kinds: %d %s", code, out)
	}

	rec := httptest.NewRecorder()
	launchHistoryHandler(rec, httptest.NewRequest(http.MethodGet, "/launch/history?kind=done", nil))
	got.Entries = nil
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if rec.Code != 200 || len(got.Entries) != 1 || got.Entries[0].Ref != "TASK-1" {
		t.Fatalf("?kind=done: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	launchHistoryHandler(rec, httptest.NewRequest(http.MethodGet, "/launch/history?kind=lost", nil))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "run, merged, done, canceled, left") {
		t.Fatalf("?kind=lost: %d %s", rec.Code, rec.Body.String())
	}
}
