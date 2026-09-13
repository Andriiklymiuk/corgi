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

	"andriiklymiuk/corgi/utils/agent/bots"
	"andriiklymiuk/corgi/utils/agent/watch"
	"andriiklymiuk/corgi/utils/agent/workspace"
)

// The phone lists the bots with what they are and what they run on, can
// write one, and opening one puts --bot on the new session's command
// line; a name nobody defined is 404.
func TestBotsReachThePhoneAndTheNewSessionLine(t *testing.T) {
	dir := phoneBoard(t, true)
	reg := &workspace.Registry{}
	reg.Upsert(workspace.Workspace{ID: "api", AbsPath: t.TempDir(), Status: workspace.StatusOK})
	if err := workspace.Save(agentRegistryPath(dir), reg); err != nil {
		t.Fatal(err)
	}
	store, _ := bots.Load(bots.Path(dir))
	store.Put(bots.Bot{Name: "reviewer", Title: "Code Reviewer", Workspace: "api", Soul: "You review pull requests. Never merge.", Model: "opus", Color: "orange", On: []string{"pr.review"}})
	if err := bots.Save(bots.Path(dir), store); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	launchBotsHandler(rec, httptest.NewRequest(http.MethodGet, "/launch/bots", nil))
	if rec.Code != 200 {
		t.Fatalf("bots: %d %s", rec.Code, rec.Body)
	}
	var got struct {
		Bots      []map[string]any `json:"bots"`
		Templates []map[string]any `json:"templates"`
		Triggers  []map[string]any `json:"triggers"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got.Bots) != 1 || got.Bots[0]["title"] != "Code Reviewer" || got.Bots[0]["hasSoul"] != true || got.Bots[0]["color"] != "orange" {
		t.Fatalf("the phone sees the bot: %+v", got.Bots)
	}
	// A paired phone reads the soul (the body travels sealed) and what the
	// bot runs on, in words; the templates ride along for "add a reviewer".
	if got.Bots[0]["soul"] != "You review pull requests. Never merge." || got.Bots[0]["onWords"] != "a review lands on a pull request" {
		t.Fatalf("soul and triggers: %+v", got.Bots[0])
	}
	if len(got.Templates) < 3 || len(got.Triggers) < 5 {
		t.Fatalf("templates %d triggers %d", len(got.Templates), len(got.Triggers))
	}

	// The phone edits it: a new soul, a second trigger; the colour it had stays.
	rec = post(launchBotsHandler, "/launch/bots", `{"name":"reviewer","title":"Code Reviewer","workspace":"api","soul":"Be brief.","model":"sonnet","on":["pr.review","ci.failed"]}`)
	if rec.Code != 200 {
		t.Fatalf("edit: %d %s", rec.Code, rec.Body)
	}
	store, _ = bots.Load(bots.Path(dir))
	if b, _ := store.Find("reviewer"); b.Soul != "Be brief." || b.Model != "sonnet" || b.Color != "orange" || len(b.On) != 2 {
		t.Fatalf("edited: %+v", b)
	}
	if rec := post(launchBotsHandler, "/launch/bots", `{"name":"x","workspace":"api","on":["pr.merged"]}`); rec.Code != 400 {
		t.Fatalf("an unknown trigger is refused: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	launchBotsHandler(rec, httptest.NewRequest(http.MethodGet, "/launch/bots?name=reviewer", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"runs":[]`) {
		t.Fatalf("detail: %d %s", rec.Code, rec.Body)
	}

	rec = post(launchNewHandler, "/launch/new", `{"bot":"reviewer"}`)
	if rec.Code != 200 {
		t.Fatalf("open: %d %s", rec.Code, rec.Body)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "commands"))
	spooled, _ := os.ReadFile(filepath.Join(dir, "commands", entries[len(entries)-1].Name()))
	if !strings.Contains(string(spooled), "agent claude --bot reviewer") {
		t.Fatalf("the session opens as the bot: %s", spooled)
	}
	if rec := post(launchNewHandler, "/launch/new", `{"bot":"nobody"}`); rec.Code != 404 {
		t.Fatalf("unknown bot: %d", rec.Code)
	}
	if rec := post(launchNewHandler, "/launch/new", `{"bot":"Bad Name"}`); rec.Code != 400 {
		t.Fatalf("bad name: %d", rec.Code)
	}
}

// Resuming needs the transcript to still be there under the account the
// launch runs as.
func TestResumeOnlyWhenTheTranscriptExists(t *testing.T) {
	cfg := t.TempDir()
	cwd := "/home/me/acme-api"
	if transcriptExists(map[string]string{"CLAUDE_CONFIG_DIR": cfg}, cwd, "sess-1") {
		t.Fatal("nothing there yet")
	}
	p := filepath.Join(cfg, "projects", "-home-me-acme-api", "sess-1.jsonl")
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	_ = os.WriteFile(p, []byte("{}\n"), 0o600)
	if !transcriptExists(map[string]string{"CLAUDE_CONFIG_DIR": cfg}, cwd, "sess-1") {
		t.Fatal("the transcript is there")
	}
}

// A bot's detail sums its runs into a ledger: runs, failures, retries,
// the pull requests it opened and how many merged, and the bill.
func TestABotsDetailCarriesItsLedger(t *testing.T) {
	dir := phoneBoard(t, true)
	fixes := watch.LoadFixLog(dir)
	now := time.Now()
	start := func(key, ref string, at time.Time) {
		fixes.StartFor(watch.Event{Key: key, Ref: ref, Kind: watch.KindPRReview, Title: "t"}, at)
		fixes.SetBot(key, "reviewer")
	}
	start("bot:reviewer:e1", "acme/api#1", now.Add(-3*time.Hour))
	fixes.SetCost("bot:reviewer:e1", 1.25, 40_000)
	fixes.Finish("bot:reviewer:e1", []string{"https://github.com/acme/api/pull/10"}, "two findings", "", now.Add(-3*time.Hour))
	start("bot:reviewer:e2", "acme/api#2", now.Add(-2*time.Hour))
	fixes.SetRetry("bot:reviewer:e2", "opus")
	fixes.SetCost("bot:reviewer:e2", 2.00, 60_000)
	fixes.Finish("bot:reviewer:e2", []string{"https://github.com/acme/api/pull/11"}, "ok", "", now.Add(-2*time.Hour))
	start("bot:reviewer:e3", "acme/api#3", now.Add(-time.Hour))
	fixes.Finish("bot:reviewer:e3", nil, "", "api error", now.Add(-time.Hour))
	start("fix:e4", "acme/api#4", now)
	fixes.SetBot("fix:e4", "other")
	pulls := watch.LoadPullLog(dir)
	_ = pulls.Set("acme/api#10", watch.PullStatus{State: "merged", At: now})
	_ = pulls.Set("acme/api#11", watch.PullStatus{State: "open", At: now})

	d := botDetail(dir, bots.Bot{Name: "reviewer", Title: "Code Reviewer", Workspace: "api"})
	r := d.ROI
	if r.Runs != 3 || r.Failed != 1 || r.Retried != 1 || r.PRs != 2 || r.Merged != 1 || r.CostUSD != 3.25 || r.Tokens != 100_000 {
		t.Fatalf("ledger: %+v", r)
	}
	if r.Since.IsZero() || !r.Since.Before(now.Add(-2*time.Hour)) {
		t.Fatalf("since the oldest run: %v", r.Since)
	}
	if got := r.Line(); got != "3 runs · 1 failed · 1 retried · 2 PRs, 1 merged · $3.25" {
		t.Fatalf("line: %q", got)
	}
	if (BotROI{}).Line() != "no runs yet" || (BotROI{Runs: 1, Tokens: 12_500}).Line() != "1 runs · 12k tokens" {
		t.Fatal("the empty and the token-only lines")
	}
}
