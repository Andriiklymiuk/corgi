package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/events"
)

func TestWebhookNotifierPostsTitleAndBody(t *testing.T) {
	got := make(chan [2]string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 128)
		n, _ := r.Body.Read(body)
		got <- [2]string{r.Header.Get("Title"), string(body[:n])}
	}))
	defer srv.Close()

	notify := webhookNotifier(srv.URL, srv.Client())
	if notify == nil {
		t.Fatal("a valid http URL must produce a notifier")
	}
	notify("corgi agent · acme", "session restarted")

	select {
	case pair := <-got:
		if pair[0] != "corgi agent - acme" || pair[1] != "session restarted" {
			t.Errorf("posted %q / %q", pair[0], pair[1])
		}
	case <-time.After(3 * time.Second):
		t.Fatal("webhook never arrived")
	}
}

func TestAsciiHeader(t *testing.T) {
	if got := asciiHeader("corgi agent · idé"); got != "corgi agent - id" {
		t.Errorf("asciiHeader = %q", got)
	}
}

func TestWebhookNotifierRejectsBadURLs(t *testing.T) {
	for _, u := range []string{"", "not a url", "ftp://x/y", "file:///etc/passwd", "https://"} {
		if webhookNotifier(u, nil) != nil {
			t.Errorf("url %q must not produce a notifier", u)
		}
	}
}

func TestCombinedNotifierSkipsNil(t *testing.T) {
	if combinedNotifier(nil, nil) != nil {
		t.Error("all-nil must collapse to nil")
	}
	calls := 0
	n := combinedNotifier(nil, func(string, string) { calls++ })
	n("t", "b")
	if calls != 1 {
		t.Errorf("calls = %d", calls)
	}
}

func TestLaunchStopHandlerMethodAndValidation(t *testing.T) {
	rec := httptest.NewRecorder()
	launchStopHandler(rec, httptest.NewRequest(http.MethodGet, "/launch/stop", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET must be 405, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	launchStopHandler(rec, httptest.NewRequest(http.MethodPost, "/launch/stop", strings.NewReader(`{}`)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing workspace must be 400, got %d", rec.Code)
	}
}

func TestSessionHistoryReadsTheTimeline(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", dir)
	agentD, _ := agentDir()

	log := events.NewLog(agentD)
	log.Append("acme", events.Event{Kind: "started", PID: 1})
	log.Append("acme", events.Event{Kind: "session", URL: "https://claude.ai/code/session_01A"})
	log.Append("acme", events.Event{Kind: "session", URL: "https://claude.ai/code/session_01B"})
	log.Append("acme", events.Event{Kind: "session", URL: "https://claude.ai/code/session_01A"})

	got := sessionHistory("acme")
	if len(got) != 2 {
		t.Fatalf("history = %+v, want 2 deduped links", got)
	}
	if got[0].URL != "https://claude.ai/code/session_01A" || got[1].URL != "https://claude.ai/code/session_01B" {
		t.Errorf("order = %q then %q, want newest first", got[0].URL, got[1].URL)
	}
	if got[0].At == 0 {
		t.Error("history entries must carry a timestamp")
	}
}

func TestSessionHistoryEmptyWithoutEvents(t *testing.T) {
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	if got := sessionHistory("nope"); len(got) != 0 {
		t.Errorf("no timeline means empty history, got %+v", got)
	}
}

func TestCheckWorkspaceTrust(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", dir)
	home := t.TempDir()
	t.Setenv("HOME", home)

	agentD, _ := agentDir()
	stack := stackWithAgentConfig(t, "")
	registerStack(t, agentD, "acme", stack)

	checks := checkWorkspaceTrust()
	if len(checks) != 1 {
		t.Fatalf("checks = %+v, want one row for acme", checks)
	}
	if checks[0].OK {
		t.Error("no .claude.json means untrusted, the row must fail with a fix")
	}
	if checks[0].Fix == "" {
		t.Error("an untrusted workspace needs the fix instruction")
	}

	trust := map[string]any{"projects": map[string]any{stack: map[string]any{"hasTrustDialogAccepted": true}}}
	data, _ := json.Marshal(trust)
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	checks = checkWorkspaceTrust()
	if len(checks) != 1 || !checks[0].OK {
		t.Errorf("a trusted workspace must pass, got %+v", checks)
	}
}

func TestDescribeEvent(t *testing.T) {
	cases := map[string]events.Event{
		"started (pid 7)":                  {Kind: "started", PID: 7},
		"session https://claude.ai/code/x": {Kind: "session", URL: "https://claude.ai/code/x"},
		"disabled — auth broke":            {Kind: "disabled", Reason: "auth broke"},
		"exited · crash — boom":            {Kind: "exited", Cause: "crash", Reason: "boom"},
		"exited":                           {Kind: "exited"},
	}
	for want, e := range cases {
		if got := describeEvent(e); got != want {
			t.Errorf("describeEvent(%+v) = %q, want %q", e, got, want)
		}
	}
}
