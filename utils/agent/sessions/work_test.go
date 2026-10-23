package sessions

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSetWorkSaysWhetherTheBoardChanged(t *testing.T) {
	r := New(filepath.Join(t.TempDir(), "sessions.json"), 6)
	w := Work{Left: []LeftItem{{ID: "k1#0", Workspace: "acme", Text: "Run it", Kind: "other", At: time.Now()}}}
	if !r.SetWork(w) {
		t.Fatal("the first work is a change")
	}
	if r.SetWork(w) {
		t.Fatal("the same work again is not")
	}
	if got := r.Snapshot(time.Now()).Left; len(got) != 1 || got[0].ID != "k1#0" {
		t.Fatalf("the board carries it: %+v", got)
	}
}
