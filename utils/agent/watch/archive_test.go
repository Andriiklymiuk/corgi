package watch

import (
	"testing"
	"time"
)

func TestArchivedCardsStayHiddenUntilSomethingNewHappens(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	l := LoadArchive(dir)
	if err := l.Put(Archived{Key: "k1", Ref: "ABC-1", Column: "Inbox", Why: "quiet", LastAt: now.Add(-10 * 24 * time.Hour), At: now}); err != nil {
		t.Fatal(err)
	}
	l = LoadArchive(dir)
	if !l.Hidden("k1", now.Add(-10*24*time.Hour), now) {
		t.Fatal("the card is hidden")
	}
	if l.Hidden("k1", now.Add(-time.Hour), now) {
		t.Fatal("a newer event on the key brings it back")
	}
	if l.Hidden("k2", now, now) {
		t.Fatal("a key never archived is not hidden")
	}
	_ = l.Put(Archived{Key: "k3", Ref: "ABC-3", Column: "Done", Why: "merged", LastAt: now.Add(-time.Hour), At: now})
	if l.Hidden("k3", now.Add(-time.Hour), now) {
		t.Fatal("a Done card shows for a day after it went into the history")
	}
	if !l.Hidden("k3", now.Add(-time.Hour), now.Add(25*time.Hour)) {
		t.Fatal("then it is gone")
	}
	_ = l.Put(Archived{Key: "k1", Ref: "ABC-1", Column: "Done", Why: "merged", LastAt: now, At: now.Add(time.Minute)})
	if rows := l.Since(now.Add(-time.Second)); len(rows) != 2 || rows[0].Key != "k1" || rows[0].Why != "merged" {
		t.Fatalf("one row per key, newest first: %+v", rows)
	}
	if _, ok := l.Forget("abc-1"); !ok || l.Hidden("k1", now, now.Add(48*time.Hour)) {
		t.Fatal("forgetting puts the card back")
	}
}

func TestCostNamesWhoWorked(t *testing.T) {
	var c Cost
	c.AddWorker("", "sonnet-4-5")
	c.AddWorker("codex", "gpt-5-codex, sonnet-4-5")
	c.AddWorker("Claude", "")
	if len(c.Agents) != 2 || c.Agents[0] != "claude" || c.Agents[1] != "codex" {
		t.Fatalf("agents once each, claude when unnamed: %v", c.Agents)
	}
	if len(c.Models) != 2 || c.Models[0] != "sonnet-4-5" || c.Models[1] != "gpt-5-codex" {
		t.Fatalf("models once each: %v", c.Models)
	}
	if FinishedState("Ready to deploy") == "" || FinishedState("ready for release") == "" {
		t.Fatal("a ticket waiting on a deploy is over for the board")
	}
}
