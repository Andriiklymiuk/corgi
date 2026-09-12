package bots

import (
	"path/filepath"
	"testing"
	"time"
)

// A bot keeps its thread through a redefinition, and the daemon's record of
// a new session moves it; a name that would not survive a shell is refused.
func TestABotKeepsItsThread(t *testing.T) {
	path := Path(t.TempDir())
	s, _ := Load(path)
	s.Put(Bot{Name: "reviewer", Workspace: "api", Soul: "You review.", Color: "orange"})
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	if err := RecordSession(path, "reviewer", "sess-1", time.Now()); err != nil {
		t.Fatal(err)
	}
	s, _ = Load(path)
	b, ok := s.Find("reviewer")
	if !ok || b.LastSession != "sess-1" || b.CreatedAt.IsZero() {
		t.Fatalf("the thread is recorded: %+v", b)
	}
	s.Put(Bot{Name: "reviewer", Title: "Code Reviewer", Workspace: "api", Model: "opus"})
	_ = Save(path, s)
	s, _ = Load(path)
	b, _ = s.Find("reviewer")
	if b.LastSession != "sess-1" || b.Display() != "Code Reviewer" || b.Model != "opus" {
		t.Fatalf("redefining keeps the thread: %+v", b)
	}
	if err := RecordSession(path, "nobody", "x", time.Now()); err != nil {
		t.Fatal("an unknown bot is nobody's business")
	}
	if !s.Remove("reviewer") || s.Remove("reviewer") {
		t.Fatal("remove once")
	}
	for _, bad := range []string{"", "Reviewer", "a b", "-x", "x/y", "abcdefghijklmnopqrstuvwxyz0123456789"} {
		if ValidName(bad) {
			t.Fatalf("%q must be refused", bad)
		}
	}
	if !ValidName("code-reviewer2") {
		t.Fatal("a plain name is fine")
	}
	if _, err := Load(filepath.Join(t.TempDir(), "none.json")); err != nil {
		t.Fatal("no file is an empty store")
	}
}
