package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils/agent/sessions"
)

func TestExplainReadsTheDiffAndAnswersInThreeLines(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	_ = os.WriteFile(filepath.Join(repo, "auth.go"), []byte("package auth\n\nfunc Refresh() {}\n"), 0o644)
	run("add", ".")
	run("commit", "-q", "-m", "base")
	run("checkout", "-q", "-b", "corgi/APP-1")
	_ = os.WriteFile(filepath.Join(repo, "auth.go"), []byte("package auth\n\n// a grace window\nfunc Refresh() { grace() }\n"), 0o644)
	run("commit", "-qam", "grace")

	phoneBoard(t, true,
		sessions.Session{ID: "s1", Label: "api", Display: "api·auth", Status: sessions.StatusWorking, Cwd: repo, Branch: "corgi/APP-1", Ticket: "APP-1"},
		sessions.Session{ID: "s2", Label: "web", Display: "web·cart", Status: sessions.StatusWorking, Cwd: repo},
	)
	origAllowed := streamAllowedFor
	defer func() { streamAllowedFor = origAllowed }()
	streamAllowedFor = func(ws string) bool { return ws == "api" }

	var gotSystem, gotPrompt, gotModel string
	origRun := runClaudePrint
	defer func() { runClaudePrint = origRun }()
	runClaudePrint = func(_ context.Context, model, system, prompt string) (string, error) {
		gotModel, gotSystem, gotPrompt = model, system, prompt
		return "Refresh now calls grace().\nOne file, two lines.\nNo tests touched.\nrisk: low — a comment and one call", nil
	}

	post := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		launchAskHandler(rec, httptest.NewRequest(http.MethodPost, "/launch/ask", strings.NewReader(body)))
		return rec
	}
	if rec := post(`{"session":"s2","diff":true}`); rec.Code != http.StatusForbidden {
		t.Fatalf("web is not readable from a phone: %d %s", rec.Code, rec.Body)
	}
	rec := post(`{"session":"s1","diff":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("explain: %d %s", rec.Code, rec.Body)
	}
	var out struct {
		Answer string `json:"answer"`
		Risk   string `json:"risk"`
		Files  int    `json:"files"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Risk != "low" || out.Files != 1 || !strings.HasPrefix(out.Answer, "Refresh now calls grace().") {
		t.Fatalf("answer: %+v", out)
	}
	if gotModel != askModel || !strings.Contains(gotSystem, "risk:") {
		t.Fatalf("the chief's cheap model with the explainer's soul: %q %q", gotModel, gotSystem)
	}
	if !strings.Contains(gotPrompt, "auth.go") || !strings.Contains(gotPrompt, "+func Refresh() { grace() }") || !strings.Contains(gotPrompt, "APP-1") {
		t.Fatalf("the prompt carries the files, the patch and the ticket:\n%s", gotPrompt)
	}
	post(`{"session":"s1","diff":true,"question":"does it touch the token?"}`)
	if !strings.Contains(gotPrompt, "QUESTION: does it touch the token?") {
		t.Fatalf("the question: %s", gotPrompt)
	}
}

func TestRiskWordIsReadOffTheLastLine(t *testing.T) {
	for in, want := range map[string]string{
		"a\nb\nrisk: high — drops a table": "high",
		"one line\nRisk: Medium":           "medium",
		"no word at all":                   "",
		"risk: low":                        "low",
	} {
		if got := riskWord(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}
