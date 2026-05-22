package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

// freeTCPPort opens then closes an ephemeral listener and returns the port that
// is now free (nothing listening), for the readiness-timeout paths.
func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

// liveTCPPort returns an open listener and its port; caller closes the listener.
func liveTCPPort(t *testing.T) (net.Listener, int) {
	t.Helper()
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	return ln, ln.Addr().(*net.TCPAddr).Port
}

// captureStderr mirrors captureStdout but redirects os.Stderr, so tests can
// assert that human-mode error output stays off stdout.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	var buf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&buf, r)
	}()
	fn()
	w.Close()
	os.Stderr = orig
	wg.Wait()
	return buf.String()
}

func execTestCompose(dir string) *utils.CorgiCompose {
	return &utils.CorgiCompose{
		Services: []utils.Service{
			{ServiceName: "svc", AbsolutePath: dir},
		},
	}
}

func TestExecService_ExitCodePropagates(t *testing.T) {
	corgi := execTestCompose(t.TempDir())
	code, err := execService(corgi, "svc", []string{"sh", "-c", "exit 7"}, false, time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 7 {
		t.Errorf("expected exit code 7, got %d", code)
	}
}

func TestExecService_Success(t *testing.T) {
	corgi := execTestCompose(t.TempDir())
	code, err := execService(corgi, "svc", []string{"true"}, false, time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
}

func TestExecService_UnknownService_Human(t *testing.T) {
	corgi := execTestCompose(t.TempDir())

	var stdout, stderr string
	stdout = captureStdout(t, func() {
		stderr = captureStderr(t, func() {
			code, err := execService(corgi, "nope", []string{"true"}, false, time.Second)
			if code != 2 {
				t.Errorf("expected exit code 2, got %d", code)
			}
			if err == nil {
				t.Error("expected error for unknown service")
			}
		})
	})

	// Human mode: the error must go to stderr, never stdout (no JSON code).
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("human-mode error leaked onto stdout: %q", stdout)
	}
	if strings.Contains(stderr, utils.ErrServiceNotFound) {
		t.Errorf("human-mode stderr should not contain JSON code, got %q", stderr)
	}
	if !strings.Contains(stderr, "not found") || !strings.Contains(stderr, "svc") {
		t.Errorf("expected human message with valid service list on stderr, got %q", stderr)
	}
}

func TestExecService_UnknownService_JSON(t *testing.T) {
	prev := utils.JSONOutput
	utils.JSONOutput = true
	t.Cleanup(func() { utils.JSONOutput = prev })

	corgi := execTestCompose(t.TempDir())

	out := captureStdout(t, func() {
		code, err := execService(corgi, "nope", []string{"true"}, false, time.Second)
		if code != 2 {
			t.Errorf("expected exit code 2, got %d", code)
		}
		if err == nil {
			t.Error("expected error for unknown service")
		}
	})

	if !strings.Contains(out, utils.ErrServiceNotFound) {
		t.Errorf("expected %s in JSON output, got %q", utils.ErrServiceNotFound, out)
	}
	if !strings.Contains(out, "svc") {
		t.Errorf("expected valid service list (svc) in output, got %q", out)
	}
}

func TestSplitExecArgs(t *testing.T) {
	// `corgi exec svc -- npm run migrate` → dash=1
	svc, tokens := splitExecArgs([]string{"svc", "npm", "run", "migrate"}, 1)
	if svc != "svc" {
		t.Errorf("expected svc, got %q", svc)
	}
	if strings.Join(tokens, " ") != "npm run migrate" {
		t.Errorf("expected command tokens, got %v", tokens)
	}

	// Missing command tokens: `corgi exec svc --` → dash=1, no trailing tokens.
	svc, tokens = splitExecArgs([]string{"svc"}, 1)
	if svc != "svc" {
		t.Errorf("expected svc, got %q", svc)
	}
	if len(tokens) != 0 {
		t.Errorf("expected no command tokens, got %v", tokens)
	}

	// No `--`: first arg is service, rest is command.
	svc, tokens = splitExecArgs([]string{"svc", "ls"}, -1)
	if svc != "svc" || strings.Join(tokens, " ") != "ls" {
		t.Errorf("got svc=%q tokens=%v", svc, tokens)
	}

	// Empty args → empty service, no tokens (usage error path).
	svc, tokens = splitExecArgs(nil, -1)
	if svc != "" || len(tokens) != 0 {
		t.Errorf("expected empty, got svc=%q tokens=%v", svc, tokens)
	}

	// >1 pre-dash tokens join into a bogus service name — this is exactly the
	// case runExec's dash>1 guard rejects before calling splitExecArgs.
	svc, tokens = splitExecArgs([]string{"svc", "extra", "cmd"}, 2)
	if svc != "svc extra" {
		t.Errorf("expected joined bogus service 'svc extra', got %q", svc)
	}
	if strings.Join(tokens, " ") != "cmd" {
		t.Errorf("expected command tokens 'cmd', got %v", tokens)
	}
}

func TestEnsureServiceDeps_DBTimeout(t *testing.T) {
	port := freeTCPPort(t) // nothing listening here
	corgi := &utils.CorgiCompose{
		DatabaseServices: []utils.DatabaseService{
			{ServiceName: "db", Port: port},
		},
		Services: []utils.Service{
			{ServiceName: "svc", DependsOnDb: []utils.DependsOnDb{{Name: "db"}}},
		},
	}
	err := ensureServiceDeps(corgi, corgi.Services[0], 300*time.Millisecond)
	if err == nil {
		t.Fatal("expected readiness timeout error for unreachable db")
	}
	if !strings.Contains(err.Error(), utils.ErrReadinessTimeout) {
		t.Errorf("expected %s in error, got %v", utils.ErrReadinessTimeout, err)
	}
}

func TestEnsureServiceDeps_ServiceTimeout(t *testing.T) {
	port := freeTCPPort(t)
	corgi := &utils.CorgiCompose{
		Services: []utils.Service{
			{ServiceName: "producer", Port: port},
			{ServiceName: "svc", DependsOnServices: []utils.DependsOnService{{Name: "producer"}}},
		},
	}
	err := ensureServiceDeps(corgi, corgi.Services[1], 300*time.Millisecond)
	if err == nil {
		t.Fatal("expected readiness timeout error for unreachable service dep")
	}
}

func TestEnsureServiceDeps_Success(t *testing.T) {
	dbLn, dbPort := liveTCPPort(t)
	defer dbLn.Close()
	svcLn, svcPort := liveTCPPort(t)
	defer svcLn.Close()

	corgi := &utils.CorgiCompose{
		DatabaseServices: []utils.DatabaseService{
			{ServiceName: "db", Port: dbPort},
		},
		Services: []utils.Service{
			{ServiceName: "producer", Port: svcPort},
			{ServiceName: "svc",
				DependsOnDb:       []utils.DependsOnDb{{Name: "db"}},
				DependsOnServices: []utils.DependsOnService{{Name: "producer"}}},
		},
	}
	if err := ensureServiceDeps(corgi, corgi.Services[1], 2*time.Second); err != nil {
		t.Fatalf("expected ready, got %v", err)
	}
}

func TestEnsureServiceDeps_UnknownDepsSkipped(t *testing.T) {
	// Unknown db/service deps are skipped (corgi validate flags them), so this
	// returns nil without probing anything.
	corgi := &utils.CorgiCompose{
		Services: []utils.Service{
			{ServiceName: "svc",
				DependsOnDb:       []utils.DependsOnDb{{Name: "ghost-db"}},
				DependsOnServices: []utils.DependsOnService{{Name: "ghost-svc"}}},
		},
	}
	if err := ensureServiceDeps(corgi, corgi.Services[0], time.Second); err != nil {
		t.Fatalf("expected nil for unknown deps, got %v", err)
	}
}

func TestExecService_ReadinessTimeout(t *testing.T) {
	port := freeTCPPort(t)
	corgi := &utils.CorgiCompose{
		DatabaseServices: []utils.DatabaseService{{ServiceName: "db", Port: port}},
		Services: []utils.Service{
			{ServiceName: "svc", AbsolutePath: t.TempDir(),
				DependsOnDb: []utils.DependsOnDb{{Name: "db"}}},
		},
	}
	stderr := captureStderr(t, func() {
		code, err := execService(corgi, "svc", []string{"true"}, true, 300*time.Millisecond)
		if code != 1 {
			t.Errorf("expected exit code 1 on readiness timeout, got %d", code)
		}
		if err == nil {
			t.Error("expected error on readiness timeout")
		}
	})
	if !strings.Contains(stderr, "not ready") {
		t.Errorf("expected readiness message on stderr, got %q", stderr)
	}
}

func TestExecService_SpawnFailure(t *testing.T) {
	// A working dir that does not exist makes exec.Cmd.Start fail before any
	// child exit code, exercising the spawn-failure branch (code 1 + error).
	corgi := execTestCompose(t.TempDir() + "/does-not-exist")
	stderr := captureStderr(t, func() {
		code, err := execService(corgi, "svc", []string{"true"}, false, time.Second)
		if err == nil {
			t.Error("expected spawn failure error")
		}
		if code != 1 {
			t.Errorf("expected exit code 1 on spawn failure, got %d", code)
		}
	})
	if !strings.Contains(stderr, "failed to run command") {
		t.Errorf("expected spawn-failure message on stderr, got %q", stderr)
	}
}

func TestReadyTimeoutFlag(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Duration("ready-timeout", defaultReadyTimeout, "")

	// Unset => default.
	if got := readyTimeoutFlag(cmd); got != defaultReadyTimeout {
		t.Errorf("expected default %v, got %v", defaultReadyTimeout, got)
	}

	// Explicit positive value is honored.
	_ = cmd.Flags().Set("ready-timeout", "5s")
	if got := readyTimeoutFlag(cmd); got != 5*time.Second {
		t.Errorf("expected 5s, got %v", got)
	}

	// Non-positive falls back to default.
	_ = cmd.Flags().Set("ready-timeout", "0s")
	if got := readyTimeoutFlag(cmd); got != defaultReadyTimeout {
		t.Errorf("expected default for 0s, got %v", got)
	}
}

func TestExecService_JSONOutput(t *testing.T) {
	prev := utils.JSONOutput
	utils.JSONOutput = true
	t.Cleanup(func() { utils.JSONOutput = prev })

	corgi := execTestCompose(t.TempDir())

	out := captureStdout(t, func() {
		code, err := execService(corgi, "svc", []string{"sh", "-c", "echo hi; exit 3"}, false, time.Second)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != 3 {
			t.Errorf("expected exit code 3, got %d", code)
		}
	})

	// stdout must be pure JSON (child "hi" goes to stderr in JSON mode).
	var result map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &result); err != nil {
		t.Fatalf("stdout is not pure JSON: %v\noutput: %q", err, out)
	}
	if result["service"] != "svc" {
		t.Errorf("expected service=svc, got %v", result["service"])
	}
	if result["exitCode"].(float64) != 3 {
		t.Errorf("expected exitCode=3, got %v", result["exitCode"])
	}
	if _, ok := result["durationMs"]; !ok {
		t.Error("expected durationMs in result")
	}
	if strings.Contains(out, "hi") {
		t.Errorf("child output leaked into stdout: %q", out)
	}
}
