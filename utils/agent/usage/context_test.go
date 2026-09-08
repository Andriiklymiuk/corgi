package usage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestContextOfReadsTheNewestAssistantUsage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"hi"}}`,
		`{"type":"assistant","timestamp":"2026-09-08T10:00:00Z","message":{"model":"claude-opus-4-7","usage":{"input_tokens":10,"cache_read_input_tokens":50000,"cache_creation_input_tokens":1000,"output_tokens":5}}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"` + strings.Repeat("x", 3000) + `"}]}}`,
		`{"type":"assistant","timestamp":"2026-09-08T10:01:00Z","message":{"model":"claude-opus-4-7","usage":{"input_tokens":20,"cache_read_input_tokens":90000,"cache_creation_input_tokens":10000,"output_tokens":7}}}`,
		`{"type":"progress","usage":{"input_tokens":1}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, ok := ContextOf(path)
	if !ok {
		t.Fatal("expected a context")
	}
	if c.Tokens != 100020 || c.Percent != 50 || c.Window != DefaultWindow || c.Model != "claude-opus-4-7" {
		t.Fatalf("got %+v", c)
	}
	if !c.At.Equal(time.Date(2026, 9, 8, 10, 1, 0, 0, time.UTC)) {
		t.Fatalf("at: %v", c.At)
	}
}

func TestContextOfHandlesTailStartingMidLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	var b strings.Builder
	b.WriteString(`{"type":"assistant","message":{"model":"m","usage":{"input_tokens":5000}}}` + "\n")
	b.WriteString(`{"type":"user","message":{"content":"` + strings.Repeat("y", contextTail+100) + `"}}` + "\n")
	b.WriteString(`{"type":"assistant","timestamp":"2026-09-08T10:01:00Z","message":{"model":"claude-opus-4-7[1m]","usage":{"input_tokens":400000}}}` + "\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	c, ok := ContextOf(path)
	if !ok || c.Tokens != 400000 || c.Window != 1_000_000 || c.Percent != 40 {
		t.Fatalf("got %+v ok=%v", c, ok)
	}
}

func TestContextOfMissingOrEmpty(t *testing.T) {
	if _, ok := ContextOf(filepath.Join(t.TempDir(), "nope.jsonl")); ok {
		t.Fatal("missing file is no context")
	}
	path := filepath.Join(t.TempDir(), "s.jsonl")
	_ = os.WriteFile(path, []byte(`{"type":"user"}`+"\n"), 0o600)
	if _, ok := ContextOf(path); ok {
		t.Fatal("no assistant turn is no context")
	}
}

func TestWindowForKnowsTheLongContextModels(t *testing.T) {
	t.Setenv("CORGI_CONTEXT_WINDOW", "")
	for model, want := range map[string]int64{
		"claude-opus-4-7": DefaultWindow, "claude-opus-4-7[1m]": 1_000_000,
		"claude-fable-5-1": 1_000_000, "claude-mythos-5-1": 1_000_000, "claude-sonnet-5": 1_000_000,
		"claude-haiku-4-5-20251001": DefaultWindow, "": DefaultWindow,
	} {
		if got := WindowFor(model); got != want {
			t.Errorf("%q: got %d want %d", model, got, want)
		}
	}
	t.Setenv("CORGI_CONTEXT_WINDOW", "500000")
	if got := WindowFor("claude-opus-4-7"); got != 500_000 {
		t.Errorf("override: got %d", got)
	}
}
