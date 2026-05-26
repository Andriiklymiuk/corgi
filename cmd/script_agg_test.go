package cmd

import (
	"strings"
	"testing"
)

func TestSummarizeScriptResults(t *testing.T) {
	results := []scriptResult{
		{Service: "api", Name: "test", OK: true},
		{Service: "broker", Name: "test", OK: false},
	}
	lines, failed := summarizeScriptResults(results)
	if failed != 1 {
		t.Fatalf("want 1 failed, got %d", failed)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "broker") || !strings.Contains(joined, "api") {
		t.Fatalf("summary should list both services: %q", joined)
	}
}

func TestSummarizeScriptResults_AllPass(t *testing.T) {
	_, failed := summarizeScriptResults([]scriptResult{{Service: "api", Name: "t", OK: true}})
	if failed != 0 {
		t.Fatalf("want 0 failed, got %d", failed)
	}
}
