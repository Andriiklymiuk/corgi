package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"andriiklymiuk/corgi/utils/agent/watch"
)

// "Ignore ABC-1" means the ticket, not one of its rows: the issue and every
// comment on it go together. A key from --json still targets one row.
func TestIgnoringARefCoversEveryRowOnIt(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "watch"), 0o700); err != nil {
		t.Fatal(err)
	}
	lines := `{"key":"linear:ABC-1","ref":"ABC-1","kind":"issue.new","workspace":"api"}
{"key":"linear:ABC-1:c1","ref":"ABC-1","kind":"issue.comment","workspace":"api"}
{"key":"linear:ABC-2","ref":"ABC-2","kind":"issue.new","workspace":"api"}
{"key":"github:acme/web#5","ref":"acme/web#5","kind":"pr.comment","workspace":"web"}
`
	if err := os.WriteFile(filepath.Join(dir, "watch", "events.jsonl"), []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}

	got := inboxKeysFor(dir, "abc-1", "")
	if len(got) != 2 {
		t.Fatalf("a ref covers all of its rows, got %v", got)
	}
	if got := inboxKeysFor(dir, "linear:ABC-1:c1", ""); len(got) != 1 || got[0] != "linear:ABC-1:c1" {
		t.Fatalf("a key is one row, got %v", got)
	}
	if got := inboxKeysFor(dir, "ABC-1", "web"); len(got) != 0 {
		t.Fatalf("--workspace narrows, got %v", got)
	}
	if got := inboxKeysFor(dir, "ABC-9", ""); got != nil {
		t.Fatalf("unknown ref is nothing, got %v", got)
	}

	state := watch.LoadState(dir)
	for _, k := range inboxKeysFor(dir, "ABC-1", "") {
		if err := state.Ignore(k); err != nil {
			t.Fatal(err)
		}
	}
	if !watch.LoadState(dir).IsIgnored("linear:ABC-1:c1") || watch.LoadState(dir).IsIgnored("linear:ABC-2") {
		t.Fatal("only ABC-1's rows are ignored")
	}
}
