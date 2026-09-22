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

func TestOpenArgsSpeakEachHarness(t *testing.T) {
	o := Open{PermissionMode: "bypassPermissions", Model: "gpt-5", System: "be brief", Resume: "t1", Prompt: "do ABC-1"}
	got := strings.Join(For("codex", "").OpenArgs(o), " ")
	want := "resume t1 --dangerously-bypass-approvals-and-sandbox -m gpt-5 be brief\n\ndo ABC-1"
	if got != want {
		t.Fatalf("codex:\n got %q\nwant %q", got, want)
	}
	got = strings.Join(For("", "").OpenArgs(Open{PermissionMode: "acceptEdits", Model: "opus", System: "be brief", Resume: "s1", Prompt: "hi"}), " ")
	want = "--permission-mode acceptEdits --model opus --append-system-prompt be brief --resume s1 hi"
	if got != want {
		t.Fatalf("claude:\n got %q\nwant %q", got, want)
	}
	if got := For("codex", "").OpenArgs(Open{}); got != nil {
		t.Fatalf("empty open adds nothing: %q", got)
	}
	if got := strings.Join(For("codex", "").OpenArgs(Open{Fork: true, Resume: "t1"}), " "); got != "resume t1" {
		t.Fatalf("codex has no fork, resumes plainly: %q", got)
	}
	if got := strings.Join(For("", "").OpenArgs(Open{Fork: true, Resume: "s1"}), " "); got != "--resume s1 --fork-session" {
		t.Fatalf("claude forks: %q", got)
	}
}

func TestPrintResumeAndBypass(t *testing.T) {
	got := strings.Join(For("codex", "").PrintArgs(Print{Prompt: "go", Resume: "t1", SkipPermissions: true}), " ")
	if !strings.HasPrefix(got, "exec resume t1 ") || !strings.Contains(got, "--dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("got %q", got)
	}
}

func TestAModelOfTheOtherHarnessIsDropped(t *testing.T) {
	codex := For("codex", "")
	for _, m := range []string{"opus", "sonnet", "haiku", "opusplan", "claude-opus-5", "opus[1m]"} {
		if got := strings.Join(codex.PrintArgs(Print{Prompt: "x", Model: m}), " "); strings.Contains(got, "-m ") {
			t.Fatalf("codex must not be asked for %s: %q", m, got)
		}
	}
	if got := strings.Join(codex.PrintArgs(Print{Prompt: "x", Model: "gpt-5-codex"}), " "); !strings.Contains(got, "-m gpt-5-codex") {
		t.Fatalf("its own model stays: %q", got)
	}
	claude := For("", "")
	for _, m := range []string{"gpt-5", "o3", "o4-mini", "gpt-5-codex", "codex-mini"} {
		if got := strings.Join(claude.OpenArgs(Open{Model: m}), " "); strings.Contains(got, "--model") {
			t.Fatalf("claude must not be asked for %s: %q", m, got)
		}
	}
	if got := strings.Join(claude.OpenArgs(Open{Model: "opus"}), " "); got != "--model opus" {
		t.Fatalf("its own model stays: %q", got)
	}
}
