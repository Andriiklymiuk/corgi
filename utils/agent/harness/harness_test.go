package harness

import (
	"strings"
	"testing"
)

func TestForFallsBackToClaude(t *testing.T) {
	if h := For("", ""); h.Name != Claude || h.Bin != "claude" {
		t.Fatalf("default: %+v", h)
	}
	if h := For("custom", "/opt/bin/cc"); h.Name != Claude || h.Bin != "/opt/bin/cc" {
		t.Fatalf("custom keeps its bin: %+v", h)
	}
	if h := For("Codex", ""); h.Name != Codex || h.Bin != "codex" {
		t.Fatalf("codex: %+v", h)
	}
}

func TestClaudeArgsShape(t *testing.T) {
	args := For("", "").PrintArgs(Print{Prompt: "do it", System: "soul", Model: "sonnet"})
	want := []string{"-p", "do it", "--output-format", "json", "--append-system-prompt", "soul", "--model", "sonnet", "--permission-mode", "acceptEdits"}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("got %q", args)
	}
	args = For("", "").PrintArgs(Print{Prompt: "x", SkipPermissions: true, Resume: "abc"})
	if s := strings.Join(args, " "); !strings.Contains(s, "--resume abc") || !strings.Contains(s, "--dangerously-skip-permissions") {
		t.Fatalf("got %q", args)
	}
}

func TestCodexArgsAndUnwrap(t *testing.T) {
	args := For("codex", "").PrintArgs(Print{Prompt: "fix ABC-1", System: "Be brief.", Model: "gpt-5"})
	s := strings.Join(args, " ")
	if !strings.HasPrefix(s, "exec --skip-git-repo-check --json -m gpt-5 --full-auto -- Be brief.\n\nfix ABC-1") {
		t.Fatalf("got %q", s)
	}
	if s := strings.Join(For("codex", "").PrintArgs(Print{Prompt: "p", Resume: "th_1", SkipPermissions: true, Text: true}), " "); !strings.HasPrefix(s, "exec resume th_1 --skip-git-repo-check --dangerously-bypass-approvals-and-sandbox -- p") {
		t.Fatalf("got %q", s)
	}
	out, rc := For("codex", "").Unwrap([]byte(`{"type":"thread.started","thread_id":"th_9"}
{"type":"item.completed","item":{"type":"reasoning","text":"hm"}}
{"type":"item.completed","item":{"type":"agent_message","text":"Opened PR #4"}}
{"type":"turn.completed","usage":{"input_tokens":1200,"cached_input_tokens":800,"output_tokens":300}}
`))
	if string(out) != "Opened PR #4" || !rc.OK || rc.SessionID != "th_9" || rc.Tokens != 1500 || rc.Turns != 1 {
		t.Fatalf("got %q %+v", out, rc)
	}
	raw := []byte("plain words")
	if out, rc := For("codex", "").Unwrap(raw); string(out) != "plain words" || rc.OK {
		t.Fatalf("plain stays plain: %q %+v", out, rc)
	}
}

func TestInteractiveArgs(t *testing.T) {
	if got := For("codex", "").InteractiveArgs("acceptEdits"); strings.Join(got, " ") != "--full-auto" {
		t.Fatalf("got %q", got)
	}
	if got := For("", "").InteractiveArgs("plan"); strings.Join(got, " ") != "--permission-mode plan" {
		t.Fatalf("got %q", got)
	}
	if got := For("", "").InteractiveArgs(""); got != nil {
		t.Fatalf("got %q", got)
	}
}
