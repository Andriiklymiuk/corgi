package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func textResult(texts ...string) *mcp.CallToolResult {
	res := &mcp.CallToolResult{}
	for _, t := range texts {
		res.Content = append(res.Content, mcp.TextContent{Type: "text", Text: t})
	}
	return res
}

type truncationNote struct {
	Truncated    bool   `json:"truncated"`
	EmittedChars int    `json:"emittedChars"`
	Cap          int    `json:"cap"`
	Hint         string `json:"hint"`
}

func lastNote(t *testing.T, res *mcp.CallToolResult) truncationNote {
	t.Helper()
	last, ok := res.Content[len(res.Content)-1].(mcp.TextContent)
	if !ok {
		t.Fatalf("last block is %T, want the truncation note", res.Content[len(res.Content)-1])
	}
	var n truncationNote
	if err := json.Unmarshal([]byte(last.Text), &n); err != nil || !n.Truncated {
		t.Fatalf("last block is not a truncation note: %q", last.Text)
	}
	return n
}

func assertValidCappedResult(t *testing.T, res *mcp.CallToolResult, limit int) {
	t.Helper()
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > limit {
		t.Errorf("emitted %d bytes, cap %d", len(b), limit)
	}
	if !json.Valid(b) {
		t.Error("emitted result is not valid JSON")
	}
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok && !utf8.ValidString(tc.Text) {
			t.Error("a text block is no longer valid UTF-8")
		}
	}
}

// A log made of quotes doubles on the wire; a cap that measures the raw
// slice lets 60k of it through as 120k.
func TestResultGuardQuoteHeavyPayload(t *testing.T) {
	const limit = 100_000
	raw := strings.Repeat(`"`, 60_000)
	res, cut := capToolResult(textResult(raw), limit)
	if !cut {
		t.Fatalf("60k quotes emit as %d bytes and must be cut", emittedSize(textResult(raw)))
	}
	assertValidCappedResult(t, res, limit)
	n := lastNote(t, res)
	if n.EmittedChars <= limit || n.Cap != limit || n.Hint == "" {
		t.Errorf("note = %+v", n)
	}
	head := res.Content[0].(mcp.TextContent).Text
	if len(head) < 40_000 {
		t.Errorf("head kept only %d chars; the cut should be as small as the cap allows", len(head))
	}
}

func TestResultGuardNewlineHeavyPayload(t *testing.T) {
	const limit = 50_000
	raw := strings.Repeat("\n\r\t", 30_000)
	res, cut := capToolResult(textResult(raw), limit)
	if !cut {
		t.Fatal("newline-heavy payload must be cut")
	}
	assertValidCappedResult(t, res, limit)
	lastNote(t, res)
}

func TestResultGuardCutsOnARuneBoundary(t *testing.T) {
	const limit = 20_000
	raw := strings.Repeat("🐕", 10_000)
	res, cut := capToolResult(textResult(raw), limit)
	if !cut {
		t.Fatal("emoji payload must be cut")
	}
	assertValidCappedResult(t, res, limit)
	head := res.Content[0].(mcp.TextContent).Text
	if head == "" || !strings.HasSuffix(head, "🐕") {
		t.Errorf("head must end on a whole rune, got %q…", head[max(0, len(head)-8):])
	}
}

func TestResultGuardSmallResultUntouched(t *testing.T) {
	res := textResult(`{"ok":true}`)
	before, _ := json.Marshal(res)
	out, cut := capToolResult(res, 100_000)
	after, _ := json.Marshal(out)
	if cut || string(before) != string(after) || out != res {
		t.Error("a result under the cap must pass through byte for byte")
	}
}

func TestResultGuardDropsStructuredContentFirst(t *testing.T) {
	const limit = 30_000
	text := strings.Repeat("x", 20_000)
	res := textResult(text)
	res.StructuredContent = map[string]any{"lines": strings.Repeat("y", 20_000)}
	out, cut := capToolResult(res, limit)
	if !cut || out.StructuredContent != nil {
		t.Error("structured content repeats the text and must go first")
	}
	assertValidCappedResult(t, out, limit)
}

func TestResultGuardMultipleBlocksCutLargestFirst(t *testing.T) {
	const limit = 30_000
	res := textResult(strings.Repeat("a", 1_000), strings.Repeat("b", 60_000), strings.Repeat("c", 2_000))
	out, _ := capToolResult(res, limit)
	assertValidCappedResult(t, out, limit)
	if out.Content[0].(mcp.TextContent).Text != strings.Repeat("a", 1_000) {
		t.Error("the small first block must stay whole")
	}
	if out.Content[2].(mcp.TextContent).Text != strings.Repeat("c", 2_000) {
		t.Error("the small third block must stay whole")
	}
}

func TestResultGuardMiddlewareWrapsHandlers(t *testing.T) {
	const limit = 5_000
	mw := mcpResultGuard(limit)
	handler := mw(func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return textResult(strings.Repeat("z", 50_000)), nil
	})
	res, err := handler(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	assertValidCappedResult(t, res, limit)
	lastNote(t, res)
}

func TestMCPMaxEmittedCharsEnv(t *testing.T) {
	t.Setenv("CORGI_MCP_MAX_RESULT_CHARS", "")
	if got := mcpMaxEmittedChars(); got != defaultMaxEmittedChars {
		t.Errorf("default = %d", got)
	}
	t.Setenv("CORGI_MCP_MAX_RESULT_CHARS", "140000")
	if got := mcpMaxEmittedChars(); got != 140000 {
		t.Errorf("env override = %d", got)
	}
	t.Setenv("CORGI_MCP_MAX_RESULT_CHARS", "10")
	if got := mcpMaxEmittedChars(); got != defaultMaxEmittedChars {
		t.Errorf("a cap too small to hold a note must fall back to the default, got %d", got)
	}
}

var _ server.ToolHandlerMiddleware = mcpResultGuard(1000)
