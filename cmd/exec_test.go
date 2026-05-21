package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils"
)

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
