package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/sessions"
)

// The sweep sums each session's transcript from where it stopped last
// time, puts the total on the board, and rings once when a session passes
// its budget. A budget of its own beats the default.
func TestTheSweepSumsSpendAndRingsOnceOverBudget(t *testing.T) {
	origDiff, origTree, origRoot, origTranscript := driftDiff, workingTreeOf, workspaceRootOf, transcriptOf
	defer func() {
		driftDiff, workingTreeOf, workspaceRootOf, transcriptOf = origDiff, origTree, origRoot, origTranscript
	}()
	driftDiff = func(string) (int, []string, bool) { return 0, nil, false }
	workingTreeOf = func(dir string) string { return dir }
	workspaceRootOf = func(dir string) string { return dir }

	d := testDaemon(t)
	d.Sessions = sessions.New(t.TempDir()+"/sessions.json", 8)
	d.SessionCap = 1000
	var mu sync.Mutex
	var rang []string
	d.Notify = func(title, body string) { mu.Lock(); rang = append(rang, title+": "+body); mu.Unlock() }
	d.NotifyWithLink = func(title, body, _ string) { d.Notify(title, body) }

	dir := t.TempDir()
	transcriptOf = func(_, _, id string) string { return filepath.Join(dir, id+".jsonl") }
	row := `{"message":{"usage":{"input_tokens":300,"output_tokens":100}}}` + "\n"
	os.WriteFile(filepath.Join(dir, "one.jsonl"), []byte(row+row), 0o600)
	d.Sessions.Apply(sessions.Event{Name: "SessionStart", SessionID: "one", Cwd: dir, Source: "startup"})
	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "one", Cwd: dir})

	d.checkDrift(time.Now())
	s, _ := d.Sessions.Lookup("one")
	if s.Spend == nil || s.Spend.Tokens != 800 || s.Spend.Turns != 2 || s.OverCap {
		t.Fatalf("two rows, under budget: %+v", s.Spend)
	}

	f, _ := os.OpenFile(filepath.Join(dir, "one.jsonl"), os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString(row)
	f.Close()
	d.checkDrift(time.Now())
	time.Sleep(50 * time.Millisecond)
	s, _ = d.Sessions.Lookup("one")
	if s.Spend.Tokens != 1200 || !s.OverCap {
		t.Fatalf("the third row takes it over: %+v over=%v", s.Spend, s.OverCap)
	}
	mu.Lock()
	n := len(rang)
	mu.Unlock()
	if n != 1 || !strings.Contains(rang[0], "over its budget: 1k of 1k tokens") {
		t.Fatalf("one ring, saying how much: %v", rang)
	}

	// Its own budget, wider: under again, and no ring for staying there.
	d.Sessions.SetCap("one", 5000)
	f, _ = os.OpenFile(filepath.Join(dir, "one.jsonl"), os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString(row)
	f.Close()
	d.checkDrift(time.Now())
	time.Sleep(50 * time.Millisecond)
	s, _ = d.Sessions.Lookup("one")
	mu.Lock()
	n = len(rang)
	mu.Unlock()
	if s.Spend.Tokens != 1600 || s.OverCap || n != 1 {
		t.Fatalf("its own budget: %+v over=%v rang=%d", s.Spend, s.OverCap, n)
	}
}
