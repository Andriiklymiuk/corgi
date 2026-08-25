package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"andriiklymiuk/corgi/utils/agent/workspace"
)

func TestParseMCPLogNeedsBothURLAndCode(t *testing.T) {
	if _, done := parseMCPLog("corgi mcp serving Streamable HTTP on 127.0.0.1:8765/mcp\n"); done {
		t.Error("a log with no tunnel URL or code is not done")
	}

	full := "🌐 ✓ public MCP endpoint: https://abc.trycloudflare.com/mcp\n  pairing code: WORD-123\n"
	got, done := parseMCPLog(full)
	if !done {
		t.Fatal("a log with both the URL and the code is done")
	}
	if got.publicURL != "https://abc.trycloudflare.com" {
		t.Errorf("publicURL = %q", got.publicURL)
	}
	if got.pairCode != "WORD-123" {
		t.Errorf("pairCode = %q", got.pairCode)
	}
}

func TestParseMCPLogFatalRecognition(t *testing.T) {
	if _, done := parseMCPLog("some ordinary progress line\n"); done {
		t.Error("ordinary output must not read as complete")
	}
	if !mcpFatalPattern.MatchString("mcp server error: bind: address already in use\n") {
		t.Error("a real fatal line must be recognised")
	}
	if mcpFatalPattern.MatchString("🌐 ✓ public MCP endpoint: https://x/mcp\n") {
		t.Error("the success line must not be treated as fatal")
	}
}

func TestRegisterCwdWorkspaceDoesNotHijackABasenameCollision(t *testing.T) {
	data := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", data)
	agentD, err := agentDir()
	if err != nil {
		t.Fatal(err)
	}

	// An existing "api" workspace, pointing somewhere else entirely.
	elsewhere := stackWithAgentConfig(t, "")
	registerStack(t, agentD, "api", elsewhere)

	// A different directory that also happens to be called "api".
	parent := t.TempDir()
	collide := filepath.Join(parent, "api")
	if err := os.MkdirAll(filepath.Join(collide, ".corgi"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(collide, "corgi-compose.yml"),
		[]byte("name: s\nservices:\n  api:\n    path: .\n    manualRun: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(collide)

	id, registered := registerCwdWorkspace()

	if !registered || id == "api" {
		t.Fatalf("must register under a fresh id, not hijack 'api'; got id=%q registered=%v", id, registered)
	}
	reg, _ := workspace.Load(agentRegistryPath(agentD))
	orig, _ := reg.Find("api")
	if orig.AbsPath != elsewhere {
		t.Errorf("the existing 'api' workspace was repointed to %q — hijacked", orig.AbsPath)
	}
	mine, ok := reg.Find(id)
	if !ok || mine.AbsPath != collide {
		t.Errorf("the new workspace %q must point at cwd %q, got %q", id, collide, mine.AbsPath)
	}
}

func TestParseMCPLogIgnoresATruncatedCode(t *testing.T) {
	// mcp.log read mid-write: URL is complete, code line has no newline yet.
	partial := "🌐 ✓ public MCP endpoint: https://abc.trycloudflare.com/mcp\n  pairing code: WOR"
	if _, done := parseMCPLog(partial); done {
		t.Error("a code with no line terminator must not be treated as complete — it could be mid-write")
	}
	full := partial + "D-123\n"
	got, done := parseMCPLog(full)
	if !done || got.pairCode != "WORD-123" {
		t.Errorf("once the line completes, the whole code must be captured, got %q done=%v", got.pairCode, done)
	}
}

func TestUpLockBlocksASecondHolderAndReleases(t *testing.T) {
	dir := t.TempDir()
	release, err := acquireUpLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err2 := acquireUpLock(dir); err2 == nil {
		t.Error("a second agent up must be refused while the first holds the lock")
	}
	release()
	release2, err := acquireUpLock(dir)
	if err != nil {
		t.Fatalf("the lock must be retakeable after release, got %v", err)
	}
	release2()
}

func TestUpLockReclaimsAStaleLock(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// A lock file with a pid that cannot be parsed is stale by definition.
	if err := os.WriteFile(filepath.Join(dir, "agent-up.lock"), []byte("not-a-pid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	release, err := acquireUpLock(dir)
	if err != nil {
		t.Fatalf("a stale lock must be reclaimed, got %v", err)
	}
	release()
}
