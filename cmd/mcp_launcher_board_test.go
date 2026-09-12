package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/sessions"
)

func phoneBoard(t *testing.T, running bool, list ...sessions.Session) string {
	t.Helper()
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	dir, _ := agentDir()
	os.MkdirAll(dir, 0o700)
	raw, _ := json.Marshal(sessions.State{Sessions: list})
	os.WriteFile(daemon.SessionsPath(dir), raw, 0o600)
	if running {
		exe, _ := os.Executable()
		data, _ := json.Marshal(daemon.Info{PID: os.Getpid(), Executable: exe, Commands: true})
		os.WriteFile(filepath.Join(dir, "daemon.json"), data, 0o600)
	}
	return dir
}

func post(handler http.HandlerFunc, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	return rec
}

func TestLaunchAnswerRefusesWhatShouldNotBeAnsweredBlind(t *testing.T) {
	dir := phoneBoard(t, true,
		sessions.Session{ID: "s1", Display: "api", Status: sessions.StatusNeedsInput, Pending: &sessions.Pending{Tool: "Bash", Subject: "go"}},
		sessions.Session{ID: "s2", Display: "web", Status: sessions.StatusNeedsInput, Pending: &sessions.Pending{Tool: "Bash", Subject: "rm"}},
		sessions.Session{ID: "s3", Display: "idle", Status: sessions.StatusDone},
	)
	cases := []struct {
		body string
		want int
	}{
		{`{"session":"s1","answer":"allow"}`, 200},
		{`{"session":"api","answer":"deny"}`, 200},
		{`{"session":"s2","answer":"allow"}`, 403},
		{`{"session":"s2","answer":"deny"}`, 200},
		{`{"session":"s3","answer":"allow"}`, 409},
		{`{"session":"nope","answer":"allow"}`, 404},
		{`{"session":"s1","answer":"maybe"}`, 400},
		{`{"session":"","answer":"allow"}`, 400},
	}
	for _, c := range cases {
		if rec := post(launchAnswerHandler, "/launch/answer", c.body); rec.Code != c.want {
			t.Errorf("%s: %d, want %d: %s", c.body, rec.Code, c.want, rec.Body.String())
		}
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "commands"))
	if len(entries) != 3 {
		t.Fatalf("three answers reached the spool, got %d", len(entries))
	}
	rec := httptest.NewRecorder()
	launchAnswerHandler(rec, httptest.NewRequest(http.MethodGet, "/launch/answer", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET = %d", rec.Code)
	}
}

// Interrupt is Escape into a working session and nothing else: a session
// waiting on a prompt or done has no turn to stop.
func TestLaunchInterruptOnlyAWorkingSession(t *testing.T) {
	dir := phoneBoard(t, true,
		sessions.Session{ID: "w1", Display: "api", Status: sessions.StatusWorking},
		sessions.Session{ID: "p1", Display: "web", Status: sessions.StatusNeedsInput, Pending: &sessions.Pending{Tool: "Bash", Subject: "ls"}},
		sessions.Session{ID: "d1", Display: "idle", Status: sessions.StatusDone},
	)
	for body, want := range map[string]int{
		`{"session":"w1"}`:   200,
		`{"session":"api"}`:  200,
		`{"session":"p1"}`:   409,
		`{"session":"d1"}`:   409,
		`{"session":"nope"}`: 404,
		`{"session":""}`:     400,
	} {
		if rec := post(launchInterruptHandler, "/launch/interrupt", body); rec.Code != want {
			t.Errorf("%s: %d, want %d: %s", body, rec.Code, want, rec.Body.String())
		}
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "commands"))
	if len(entries) != 2 {
		t.Fatalf("two interrupts reached the spool, got %d", len(entries))
	}
}

func TestLaunchSendTypesIntoALiveSessionOnly(t *testing.T) {
	dir := phoneBoard(t, true,
		sessions.Session{ID: "s1", Display: "api", Status: sessions.StatusDone},
		sessions.Session{ID: "s2", Display: "old", Status: sessions.StatusGone},
	)
	if rec := post(launchSendHandler, "/launch/send", `{"session":"s1","text":"run the tests"}`); rec.Code != 200 {
		t.Fatalf("send = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := post(launchSendHandler, "/launch/send", `{"session":"s2","text":"hi"}`); rec.Code != 409 {
		t.Fatalf("closed session = %d", rec.Code)
	}
	if rec := post(launchSendHandler, "/launch/send", `{"session":"s1","text":"  "}`); rec.Code != 400 {
		t.Fatalf("empty text = %d", rec.Code)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "commands"))
	if len(entries) != 1 {
		t.Fatalf("spool has %d entries", len(entries))
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "commands", entries[0].Name()))
	if !strings.Contains(string(raw), `"enter":true`) || !strings.Contains(string(raw), `"phone"`) {
		t.Fatalf("the phone sends with Enter: %s", raw)
	}
}

func TestLaunchBoardActionsNeedTheDaemon(t *testing.T) {
	phoneBoard(t, false, sessions.Session{ID: "s1", Status: sessions.StatusDone})
	if rec := post(launchSendHandler, "/launch/send", `{"session":"s1","text":"x"}`); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no daemon = %d", rec.Code)
	}
}
