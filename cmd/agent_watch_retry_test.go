package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRetryPicksTheNewestRowOfARef(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "watch"), 0o700); err != nil {
		t.Fatal(err)
	}
	lines := `{"key":"github:acme/api#7:c1","ref":"acme/api#7","kind":"pr.comment","workspace":"api","title":"first"}
{"key":"github:acme/api#7:c2","ref":"acme/api#7","kind":"pr.comment","workspace":"api","title":"second"}
{"key":"linear:ABC-2","ref":"ABC-2","kind":"issue.new","workspace":"api"}
`
	if err := os.WriteFile(filepath.Join(dir, "watch", "events.jsonl"), []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}

	e, ok := retryEventFor(dir, "acme/api#7", "")
	if !ok || e.Key != "github:acme/api#7:c2" {
		t.Fatalf("the newest row on the ref is the one to run again, got %+v", e)
	}
	if e, ok := retryEventFor(dir, "github:acme/api#7:c1", ""); !ok || e.Title != "first" {
		t.Fatalf("a key names one row, got %+v", e)
	}
	if _, ok := retryEventFor(dir, "acme/api#7", "web"); ok {
		t.Fatal("--workspace narrows")
	}
	if _, ok := retryEventFor(dir, "ABC-9", ""); ok {
		t.Fatal("unknown ref is nothing")
	}
}
