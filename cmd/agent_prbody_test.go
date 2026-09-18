package cmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/sessions"
)

func TestPRBodyReadsTheBranchAndTheSession(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", "main")
	_ = os.WriteFile(filepath.Join(repo, "auth.go"), []byte("package auth\n"), 0o644)
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-q", "-m", "base")
	gitRun(t, repo, "checkout", "-q", "-b", "corgi/APP-1")
	_ = os.WriteFile(filepath.Join(repo, "auth.go"), []byte("package auth\n\nfunc Grace() {}\n"), 0o644)
	_ = os.WriteFile(filepath.Join(repo, "go.sum"), []byte("x\n"), 0o644)
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-q", "-m", "a grace window on refresh")

	home := t.TempDir()
	prev := transcriptPathFor
	defer func() { transcriptPathFor = prev }()
	path := filepath.Join(home, "s1.jsonl")
	_ = os.WriteFile(path, []byte(`{"type":"user","uuid":"u1","timestamp":"2026-09-14T10:00:00Z","message":{"role":"user","content":"Add a grace window to token refresh. Use key sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789 for the test."}}
{"type":"assistant","uuid":"a1","timestamp":"2026-09-14T10:01:00Z","message":{"role":"assistant","content":[{"type":"text","text":"On it."}]}}
`), 0o600)
	transcriptPathFor = func(sessions.Session) string { return path }

	s := sessions.Session{ID: "s1", Label: "api", Display: "api·APP-1", Cwd: repo, Branch: "corgi/APP-1", Ticket: "APP-1",
		Tests: &sessions.TestRun{OK: true, Cmd: "go test ./...", At: time.Now().Add(-3 * time.Minute)}}
	body, err := prBodyFor(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## What", "APP-1 — Add a grace window to token refresh.", "## Commits", "- a grace window on refresh", "## Changes", "- `auth.go` +2 −0", "1 files, +2 −0 (1 generated left out)", "## Tests", "`go test ./...` ✓ · 3m ago"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
	if strings.Contains(body, "sk-ant-api03-abcdefghij") {
		t.Fatal("the key in the prompt must be scrubbed")
	}
}
