package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils/agent/watch"
)

// The editor's "Work on it" comes through the CLI: a ref finds its issue
// row, the prompt goes by id, and a refusal is a sentence, not a spool entry.
func TestWorkOnCommandFromARef(t *testing.T) {
	dir := phoneBoard(t, true)
	os.MkdirAll(filepath.Join(dir, "watch"), 0o700)
	lines := []watch.Event{
		{Key: "linear:ABC-1:comment:9", Kind: watch.KindIssueComment, Ref: "ABC-1", Title: "any news?", Workspace: "api"},
		{Key: "linear:ABC-1", Kind: watch.KindIssueNew, Ref: "ABC-1", Title: "Login loops", Workspace: "api"},
	}
	var buf bytes.Buffer
	for _, e := range lines {
		data, _ := json.Marshal(e)
		buf.Write(append(data, '\n'))
	}
	os.WriteFile(filepath.Join(dir, "watch", "events.jsonl"), buf.Bytes(), 0o600)

	keys := inboxKeysFor(dir, "ABC-1", "")
	if len(keys) != 2 {
		t.Fatalf("a ref covers every row: %v", keys)
	}
	if got := newestIssueKey(dir, keys); got != "linear:ABC-1" {
		t.Fatalf("the ticket itself is what work on it means, got %q", got)
	}

	c, status, msg := workOnCommand(dir, []string{"linear:ABC-1"}, workOnOptions{Model: "opus", Source: "cli"})
	if status != 0 {
		t.Fatalf("refused: %d %s", status, msg)
	}
	if c.Source != "cli" || !strings.Contains(c.Command, "--workspace api") || !strings.Contains(c.Command, "--model opus --ticket ABC-1 --ticket-key linear:ABC-1 --prompt-id ") {
		t.Fatalf("command: %+v", c)
	}
	if strings.Contains(c.Command, "Login loops") {
		t.Fatalf("the prompt must travel by id: %s", c.Command)
	}
	if strings.Contains(c.Command, "--isolate") {
		t.Fatalf("a worktree of its own is asked for, never assumed: %s", c.Command)
	}
	c, status, _ = workOnCommand(dir, []string{"linear:ABC-1"}, workOnOptions{Source: "phone", Isolate: true})
	if status != 0 || !strings.Contains(c.Command, " --isolate") {
		t.Fatalf("isolate reaches the session's command line: %d %s", status, c.Command)
	}

	for _, tc := range []struct {
		keys []string
		opt  workOnOptions
		want int
	}{
		{nil, workOnOptions{}, 400},
		{[]string{"linear:nope"}, workOnOptions{}, 404},
		{[]string{"linear:ABC-1"}, workOnOptions{Model: "opus; ls"}, 400},
		{[]string{"linear:ABC-1"}, workOnOptions{Profile: "nope"}, 400},
		{[]string{"linear:ABC-1"}, workOnOptions{Window: "win-9"}, 404},
	} {
		if _, status, _ := workOnCommand(dir, tc.keys, tc.opt); status != tc.want {
			t.Errorf("%v %+v: %d, want %d", tc.keys, tc.opt, status, tc.want)
		}
	}
}
