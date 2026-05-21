package cmd

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils"
)

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

func TestExecService_UnknownService(t *testing.T) {
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
		t.Errorf("expected %s in output, got %q", utils.ErrServiceNotFound, out)
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
