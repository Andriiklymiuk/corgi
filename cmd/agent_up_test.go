package cmd

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils"

	"andriiklymiuk/corgi/utils/agent/workspace"

	"github.com/spf13/cobra"
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

func TestOrDefault(t *testing.T) {
	if orDefault("", "fallback") != "fallback" {
		t.Error("empty must yield the fallback")
	}
	if orDefault("x", "fallback") != "x" {
		t.Error("a value must pass through")
	}
}

func TestMcpListeningDetectsAListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	addr := ln.Addr().String()
	if !mcpListening(addr) {
		t.Errorf("a live listener at %s must read as listening", addr)
	}
}

func TestMcpListeningFalseOnAFreePort(t *testing.T) {
	// Bind then close to get a port nothing listens on.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	if mcpListening(addr) {
		t.Errorf("a closed port %s must not read as listening", addr)
	}
}

func TestAwaitMCPLogReadsURLAndCode(t *testing.T) {
	f := filepath.Join(t.TempDir(), "mcp.log")
	if err := os.WriteFile(f, []byte("🌐 ✓ public MCP endpoint: https://abc.trycloudflare.com/mcp\n  pairing code: WORD-9\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := awaitMCPLog(f, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got.publicURL != "https://abc.trycloudflare.com" || got.pairCode != "WORD-9" {
		t.Fatalf("parsed = %+v", got)
	}
}

func TestAwaitMCPLogReturnsErrorOnAFatalLine(t *testing.T) {
	f := filepath.Join(t.TempDir(), "mcp.log")
	if err := os.WriteFile(f, []byte("mcp server error: bind: address already in use\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := awaitMCPLog(f, 2*time.Second); err == nil {
		t.Error("a fatal line must make awaitMCPLog fail fast, not wait out the timeout")
	}
}

func TestAwaitMCPLogTimesOutWithNoCode(t *testing.T) {
	f := filepath.Join(t.TempDir(), "mcp.log")
	_ = os.WriteFile(f, []byte("some progress\n"), 0o600)
	if _, err := awaitMCPLog(f, 150*time.Millisecond); err == nil {
		t.Error("no pairing code within the timeout must be an error")
	}
}

func TestPrintTerminalQRDoesNotPanic(t *testing.T) {
	// Best-effort renderer; must never crash the command.
	printTerminalQR("https://example.com/pair#CODE")
}

func TestPrintAgentUpJSON(t *testing.T) {
	utils.JSONOutput = true
	defer func() { utils.JSONOutput = false }()
	// Just exercise the JSON branch; it must not panic and must serialize.
	printAgentUp(agentUpResult{Workspace: "acme", Registered: true, DaemonPID: 1, PublicURL: "https://x", PairCode: "C"})
}

func TestPrintAgentUpHumanPaths(t *testing.T) {
	utils.JSONOutput = false
	// Exercise the human-readable branches: public URL + pair QR, and the
	// no-tunnel + hint fallback. Must not panic.
	printAgentUp(agentUpResult{
		Workspace: "acme", Registered: true, DaemonPID: 7,
		PublicURL: "https://x.trycloudflare.com", PairCode: "C1",
		PairURL: "https://x.trycloudflare.com/pair#C1",
	})
	printAgentUp(agentUpResult{
		Workspace: "acme", Registered: false, DaemonPID: 7,
		MCPAddr: "127.0.0.1:8765", LogPath: "/tmp/mcp.log",
		Hint: "already in use", PairCode: "C2",
	})
}

func TestMustAgentDirReturnsTheAgentDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", dir)
	want, _ := agentDir()
	if got := mustAgentDir(); got != want {
		t.Errorf("mustAgentDir() = %q, want %q", got, want)
	}
}

func TestReadAgentPidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, mcpPidName)

	if _, ok := readAgentPidFile(path); ok {
		t.Error("a missing pid file must report not-ok, so `agent down` has nothing to stop")
	}
	if err := os.WriteFile(path, []byte("4321\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if pid, ok := readAgentPidFile(path); !ok || pid != 4321 {
		t.Errorf("readAgentPidFile = %d, %v; want 4321, true", pid, ok)
	}
	for _, bad := range []string{"garbage\n", "-1\n", ""} {
		if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, ok := readAgentPidFile(path); ok {
			t.Errorf("a malformed pid %q must report not-ok", bad)
		}
	}
}

func TestRunAgentRestartRunsDownThenFreshUp(t *testing.T) {
	origDown, origUp := restartDown, restartUp
	defer func() { restartDown, restartUp = origDown, origUp }()

	var order []string
	freshWhenUpRan := false
	restartDown = func(*cobra.Command, []string) { order = append(order, "down") }
	restartUp = func(cmd *cobra.Command, _ []string) {
		order = append(order, "up")
		freshWhenUpRan, _ = cmd.Flags().GetBool("fresh")
	}

	runAgentRestart(agentRestartCmd, nil)

	if len(order) != 2 || order[0] != "down" || order[1] != "up" {
		t.Fatalf("order = %v, want down then up", order)
	}
	if !freshWhenUpRan {
		t.Error("restart must force --fresh before up runs")
	}
}

func TestTunnelArgs(t *testing.T) {
	if args, err := tunnelArgs("", "", ""); err != nil || len(args) != 0 {
		t.Errorf("quick tunnel: %v %v", args, err)
	}
	if args, err := tunnelArgs("ngrok", "", ""); err != nil || strings.Join(args, " ") != "--tunnel-provider ngrok" {
		t.Errorf("provider only: %v %v", args, err)
	}
	if _, err := tunnelArgs("", "corgi-agent", ""); err == nil || !strings.Contains(err.Error(), "--tunnel-hostname") {
		t.Errorf("a name without a hostname must be refused with the fix, got %v", err)
	}
	args, err := tunnelArgs("", "corgi-agent", "corgi.example.com")
	if err != nil || strings.Join(args, " ") != "--tunnel-name corgi-agent --tunnel-hostname corgi.example.com" {
		t.Errorf("named: %v %v", args, err)
	}
	if args, err := tunnelArgs("ngrok", "", "x.ngrok-free.app"); err != nil || strings.Join(args, " ") != "--tunnel-provider ngrok --tunnel-hostname x.ngrok-free.app" {
		t.Errorf("hostname only (ngrok static domain): %v %v", args, err)
	}
}

func TestMergeUpSettingsReusesOnlyUnchangedFlags(t *testing.T) {
	saved := upSettings{Provider: "cloudflared", TunnelName: "corgi-agent", TunnelHostname: "corgi.example.com"}
	changed := func(f string) bool { return f == "tunnel-hostname" }
	got, reused := mergeUpSettings(upSettings{TunnelHostname: ""}, changed, saved)
	if got.TunnelHostname != "" {
		t.Errorf("an explicitly passed empty hostname must win, got %q", got.TunnelHostname)
	}
	if got.TunnelName != "corgi-agent" || got.Provider != "cloudflared" {
		t.Errorf("unchanged flags must come from the saved run, got %+v", got)
	}
	if !strings.Contains(reused, "--tunnel-name corgi-agent") || strings.Contains(reused, "hostname") {
		t.Errorf("notice must name what was reused: %q", reused)
	}
	if _, reused := mergeUpSettings(upSettings{}, func(string) bool { return false }, upSettings{}); reused != "" {
		t.Errorf("nothing saved means nothing reused, got %q", reused)
	}
}

func TestUpSettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if got := loadUpSettings(dir); got != (upSettings{}) {
		t.Errorf("no file means empty settings, got %+v", got)
	}
	in := upSettings{HTTP: defaultMCPAddr, Provider: "ngrok", TunnelHostname: "x.ngrok-free.app"}
	if err := saveUpSettings(dir, in); err != nil {
		t.Fatal(err)
	}
	got := loadUpSettings(dir)
	if got.HTTP != "" || got.Provider != "ngrok" || got.TunnelHostname != "x.ngrok-free.app" {
		t.Errorf("round trip = %+v (the default addr must not be pinned)", got)
	}
}

func TestTunnelPreflightRejectsUnknownProvider(t *testing.T) {
	if err := tunnelPreflight("nope", "", ""); err == nil || !strings.Contains(err.Error(), "unknown tunnel provider") {
		t.Errorf("err = %v", err)
	}
}

func TestAwaitMCPLogSurfacesTheTunnelError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.log")
	if err := os.WriteFile(path, []byte("🌐 ✗ tunnel: ngrok authtoken not configured.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := awaitMCPLog(path, 2*time.Second)
	if err == nil || !strings.Contains(err.Error(), "authtoken") {
		t.Errorf("the tunnel's own error must be returned at once, got %v", err)
	}
}
