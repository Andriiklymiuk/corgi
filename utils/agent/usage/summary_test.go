package usage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSummaryOfReadsTheNewestAssistantTextAndThePRLink(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.jsonl")
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"do it"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Opened the PR: https://github.com/acme/api/pull/12"}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{}}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"## Done\n\n- **Tests pass.** The referral flow is wired.\n"}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, ok := SummaryOf(path)
	if !ok {
		t.Fatal("a transcript with assistant text must summarise")
	}
	if s.Line != "Tests pass. The referral flow is wired." {
		t.Fatalf("line = %q", s.Line)
	}
	if s.PR != "https://github.com/acme/api/pull/12" {
		t.Fatalf("pr = %q", s.PR)
	}
}

func TestSummaryOfCapsAndSkipsNoise(t *testing.T) {
	long := strings.Repeat("word ", 60)
	s, ok := summaryFromTail([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"`+"```go\\nx\\n```\\n"+long+`"}]}}`), false)
	if !ok || !strings.HasSuffix(s.Line, "…") || len([]rune(s.Line)) != maxSummaryLen {
		t.Fatalf("%q", s.Line)
	}
	if _, ok := summaryFromTail([]byte(`{"type":"user","message":{"content":"hi"}}`), false); ok {
		t.Fatal("no assistant text, no summary")
	}
}
