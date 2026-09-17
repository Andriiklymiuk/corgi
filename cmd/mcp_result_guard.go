package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// defaultMaxEmittedChars is what one tool result may weigh on the wire.
// Claude.ai and Desktop refuse a result past ~150,000 characters and Claude
// Code's default budget is 25,000 tokens; 100,000 sits under both.
const defaultMaxEmittedChars = 100_000

const truncationHint = "narrow the query: fewer lines, a grep, a since, or a service filter"

func mcpMaxEmittedChars() int {
	if v, err := strconv.Atoi(os.Getenv("CORGI_MCP_MAX_RESULT_CHARS")); err == nil && v >= 1000 {
		return v
	}
	return defaultMaxEmittedChars
}

// mcpResultGuard caps every tool result at limit emitted characters. It is
// the one backstop; tools with their own head+tail caps keep them.
func mcpResultGuard(limit int) server.ToolHandlerMiddleware {
	return func(next server.ToolHandlerFunc) server.ToolHandlerFunc {
		return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			res, err := next(ctx, req)
			if err != nil || res == nil {
				return res, err
			}
			capped, _ := capToolResult(res, limit)
			return capped, nil
		}
	}
}

// emittedSize is the size of the result as the JSON-RPC envelope carries
// it — after escaping, which is what the client counts. A quote-heavy log
// doubles on the wire, so measuring the raw text would let it through.
func emittedSize(res *mcp.CallToolResult) int {
	b, err := json.Marshal(res)
	if err != nil {
		return 0
	}
	return len(b)
}

// capToolResult returns res unchanged when it fits, otherwise a copy whose
// largest text blocks are cut (head kept) until the emitted form fits, with
// a final text block saying so. Structured content goes first: it repeats
// the text and would double the payload.
func capToolResult(res *mcp.CallToolResult, limit int) (*mcp.CallToolResult, bool) {
	original := emittedSize(res)
	if original <= limit {
		return res, false
	}
	out := *res
	out.StructuredContent = nil
	out.Content = append([]mcp.Content(nil), res.Content...)
	note := mcp.TextContent{Type: "text", Text: fmt.Sprintf(
		`{"truncated":true,"emittedChars":%d,"cap":%d,"hint":%q}`, original, limit, truncationHint)}
	out.Content = append(out.Content, note)

	for emittedSize(&out) > limit {
		i := largestTextBlock(out.Content[:len(out.Content)-1])
		if i < 0 {
			break
		}
		block := out.Content[i].(mcp.TextContent)
		block.Text = shrinkToFit(block.Text, func(candidate string) bool {
			block := block
			block.Text = candidate
			trial := out
			trial.Content = append([]mcp.Content(nil), out.Content...)
			trial.Content[i] = block
			return emittedSize(&trial) <= limit
		})
		out.Content[i] = block
	}
	return &out, true
}

func largestTextBlock(content []mcp.Content) int {
	best, bestLen := -1, 0
	for i, c := range content {
		if t, ok := c.(mcp.TextContent); ok && len(t.Text) > bestLen {
			best, bestLen = i, len(t.Text)
		}
	}
	return best
}

// shrinkToFit finds the longest head of text (on a rune boundary) for which
// fits holds; fits must be monotone in the length, which an emitted-size
// check is. An empty head is returned when nothing fits.
func shrinkToFit(text string, fits func(string) bool) string {
	lo, hi := 0, len(text)
	for lo < hi {
		mid := runeBoundary(text, (lo+hi+1)/2)
		if mid <= lo {
			break
		}
		if fits(text[:mid]) {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return text[:runeBoundary(text, lo)]
}

// runeBoundary backs n off to the start of the rune it falls inside.
func runeBoundary(s string, n int) int {
	if n >= len(s) {
		return len(s)
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return n
}
