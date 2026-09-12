package cmd

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/transcript"
)

// The phone reads a conversation only for a workspace the laptop allowed;
// it opens at the newest entries, continues from where it left off, and a
// long-poll returns the moment a line lands.
func TestTranscriptStreamsOnlyAllowedWorkspaces(t *testing.T) {
	phoneBoard(t, true,
		sessions.Session{ID: "s1", Label: "api", Display: "api·auth", Status: sessions.StatusWorking, Cwd: "/w/api"},
		sessions.Session{ID: "s2", Label: "web", Display: "web·cart", Status: sessions.StatusWorking, Cwd: "/w/web"},
	)
	dir := t.TempDir()
	path := filepath.Join(dir, "s1.jsonl")
	line := func(id, text string) string {
		return `{"type":"assistant","uuid":"` + id + `","timestamp":"2026-09-12T10:00:00Z","message":{"role":"assistant","content":[{"type":"text","text":"` + text + `"}]}}` + "\n"
	}
	os.WriteFile(path, []byte(line("a1", "hello")+line("a2", "world")), 0o600)
	origAllowed, origPath := streamAllowedFor, transcriptPathFor
	defer func() { streamAllowedFor, transcriptPathFor = origAllowed, origPath }()
	streamAllowedFor = func(ws string) bool { return ws == "api" }
	transcriptPathFor = func(s sessions.Session) string {
		if s.ID == "s1" {
			return path
		}
		return filepath.Join(dir, "none.jsonl")
	}

	if rec := post(launchTranscriptHandler, "/launch/transcript", `{"session":"s2"}`); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "stream enable --workspace web") {
		t.Fatalf("web is not allowed: %d %s", rec.Code, rec.Body)
	}
	rec := post(launchTranscriptHandler, "/launch/transcript", `{"session":"api·auth"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("open: %d %s", rec.Code, rec.Body)
	}
	var got struct {
		Entries []transcript.Entry `json:"entries"`
		Offset  int64              `json:"offset"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got.Entries) != 2 || got.Entries[1].Text != "world" || got.Offset == 0 {
		t.Fatalf("opens at the newest: %+v", got)
	}

	// A long-poll returns when a line lands, not before.
	done := make(chan struct{})
	var next struct {
		Entries []transcript.Entry `json:"entries"`
		Offset  int64              `json:"offset"`
	}
	started := time.Now()
	go func() {
		defer close(done)
		rec := post(launchTranscriptHandler, "/launch/transcript", `{"session":"s1","after":`+strconv.FormatInt(got.Offset, 10)+`,"wait":10}`)
		_ = json.Unmarshal(rec.Body.Bytes(), &next)
	}()
	time.Sleep(700 * time.Millisecond)
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString(line("a3", "and again"))
	f.Close()
	<-done
	if len(next.Entries) != 1 || next.Entries[0].Text != "and again" || next.Offset <= got.Offset {
		t.Fatalf("the new line, and where to continue: %+v", next)
	}
	if time.Since(started) > 5*time.Second {
		t.Fatal("the poll returned on the line, not on the deadline")
	}
	if rec := post(launchTranscriptHandler, "/launch/transcript", `{"session":"nope"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown session: %d", rec.Code)
	}
}
