package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/events"
	"andriiklymiuk/corgi/utils/agent/pairing"
)

func TestSanitizeSessionName(t *testing.T) {
	cases := map[string]string{
		"  fix login  ":         "fix login",
		"--permission-mode x":   "permission-mode x",
		"idé ✨ ok":              "id  ok",
		strings.Repeat("a", 80): strings.Repeat("a", 60),
		"":                      "",
	}
	for in, want := range cases {
		if got := sanitizeSessionName(in); got != want {
			t.Errorf("sanitizeSessionName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLaunchInfoReportsTheMachine(t *testing.T) {
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	rec := httptest.NewRecorder()
	launchInfoHandler(rec, httptest.NewRequest(http.MethodGet, "/launch/info", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["version"] != APP_VERSION {
		t.Errorf("version = %v, want %s", body["version"], APP_VERSION)
	}
	if body["daemon"] != false {
		t.Errorf("with no daemon running, daemon must be false, got %v", body["daemon"])
	}

	rec = httptest.NewRecorder()
	launchInfoHandler(rec, httptest.NewRequest(http.MethodPost, "/launch/info", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST must be 405, got %d", rec.Code)
	}
}

func TestLaunchDevicesListsAndRevokes(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", dir)
	agentD, _ := agentDir()
	if err := os.MkdirAll(agentD, 0o700); err != nil {
		t.Fatal(err)
	}
	store := &pairing.Store{Devices: []pairing.Device{
		{Name: "phone", TokenHash: pairing.HashToken("t1"), CreatedAt: time.Now()},
		{Name: "tablet", TokenHash: pairing.HashToken("t2"), CreatedAt: time.Now()},
	}}
	if err := pairing.Save(pairing.StorePath(agentD), store); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	launchDevicesHandler(rec, httptest.NewRequest(http.MethodGet, "/launch/devices", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "tablet") {
		t.Fatalf("list = %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	launchDevicesHandler(rec, httptest.NewRequest(http.MethodDelete, "/launch/devices?name=tablet", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke = %d %s", rec.Code, rec.Body.String())
	}
	after, _ := pairing.Load(pairing.StorePath(agentD))
	if _, ok := after.Find("tablet"); ok {
		t.Error("tablet must be gone from the store")
	}

	rec = httptest.NewRecorder()
	launchDevicesHandler(rec, httptest.NewRequest(http.MethodDelete, "/launch/devices", nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("a revoke without a name must be 400, got %d", rec.Code)
	}
}

func TestLaunchDevicesRefusesSelfRevoke(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", dir)
	agentD, _ := agentDir()
	if err := os.MkdirAll(agentD, 0o700); err != nil {
		t.Fatal(err)
	}
	token, err := pairing.NewDeviceToken()
	if err != nil {
		t.Fatal(err)
	}
	store := &pairing.Store{Devices: []pairing.Device{{Name: "phone", TokenHash: pairing.HashToken(token), CreatedAt: time.Now()}}}
	if err := pairing.Save(pairing.StorePath(agentD), store); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/launch/devices?name=phone", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	launchDevicesHandler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a device must not revoke itself, got %d", rec.Code)
	}
	after, _ := pairing.Load(pairing.StorePath(agentD))
	if _, ok := after.Find("phone"); !ok {
		t.Error("the device must still be paired after the refusal")
	}
}

func TestLaunchDoctorServesTheChecks(t *testing.T) {
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	rec := httptest.NewRecorder()
	launchDoctorHandler(rec, httptest.NewRequest(http.MethodGet, "/launch/doctor", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		Checks []agentCheck `json:"checks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Checks) == 0 {
		t.Error("doctor must return its checks")
	}
}

func TestWorkspaceActivityReadsTheTimeline(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", dir)
	agentD, _ := agentDir()
	events.NewLog(agentD).Append("acme", events.Event{Kind: "exited", Cause: "network-timeout", Reason: "boom"})

	live, last := workspaceActivity("acme", "")
	if live != 0 {
		t.Errorf("live = %d with no workspace path", live)
	}
	if last == nil || last.Kind != "exited" || last.Cause != "network-timeout" || last.At == 0 {
		t.Errorf("lastEvent = %+v", last)
	}

	if _, none := workspaceActivity("ghost", ""); none != nil {
		t.Errorf("a workspace with no timeline has no last event, got %+v", none)
	}
}

func TestRecentEventsIsNewestFirst(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", dir)
	agentD, _ := agentDir()
	log := events.NewLog(agentD)
	log.Append("acme", events.Event{Kind: "started"})
	log.Append("acme", events.Event{Kind: "attention", Reason: "waiting for permission"})

	got := recentEvents("acme", 6)
	if len(got) != 2 || got[0].Kind != "attention" {
		t.Fatalf("events = %+v", got)
	}
	if len(recentEvents("acme", 1)) != 1 {
		t.Error("limit must cap the result")
	}
}

func TestLauncherURLFromRecordedTunnel(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", dir)
	agentD, _ := agentDir()
	if err := os.MkdirAll(agentD, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := launcherURL(); got != "" {
		t.Errorf("no recorded URL means no link, got %q", got)
	}
	recordPublicURL("https://x.trycloudflare.com")
	if got := launcherURL(); got != "https://x.trycloudflare.com/app" {
		t.Errorf("launcherURL = %q", got)
	}
	if err := os.WriteFile(filepath.Join(agentD, publicURLName), []byte("file:///etc/passwd\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := launcherURL(); got != "" {
		t.Errorf("a non-http URL must be refused, got %q", got)
	}
}

func TestTunnelNameFor(t *testing.T) {
	if got := tunnelNameFor("ngrok", "corgi-agent"); got != "" {
		t.Errorf("ngrok selects by domain, so the name must not be saved, got %q", got)
	}
	if got := tunnelNameFor("cloudflared", "corgi-agent"); got != "corgi-agent" {
		t.Errorf("cloudflared name = %q", got)
	}
}

func TestSetupCloudflaredTunnelPlan(t *testing.T) {
	var ran [][]string
	run := func(name string, args ...string) (string, error) {
		ran = append(ran, append([]string{name}, args...))
		if len(args) > 1 && args[1] == "list" {
			return "corgi-agent\tid\n", nil
		}
		return "", nil
	}
	installed := func(string) error { return nil }
	if err := setupCloudflaredTunnel(run, installed, "corgi-agent", "corgi.example.com", true); err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, c := range ran {
		joined += strings.Join(c, " ") + "\n"
	}
	if strings.Contains(joined, "tunnel create") {
		t.Errorf("an existing tunnel must not be recreated:\n%s", joined)
	}
	if !strings.Contains(joined, "route dns corgi-agent corgi.example.com") {
		t.Errorf("the DNS route must be planned:\n%s", joined)
	}
}

func TestSetupTunnelRequiresTheBinary(t *testing.T) {
	missing := func(name string) error { return fmt.Errorf("%s not found", name) }
	run := func(string, ...string) (string, error) { return "", nil }
	if err := setupCloudflaredTunnel(run, missing, "corgi-agent", "h", true); err == nil ||
		!strings.Contains(err.Error(), "not installed") {
		t.Errorf("a missing cloudflared must say so, got %v", err)
	}
	if err := setupNgrokTunnel(run, missing, "h"); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("a missing ngrok must say so, got %v", err)
	}
}

func TestHookDetail(t *testing.T) {
	if got := hookDetail(hookEventNotification, strings.NewReader(`{"message":"Claude needs your permission to run rm"}`)); got != "Claude needs your permission to run rm" {
		t.Errorf("detail = %q", got)
	}
	if got := hookDetail(hookEventStop, strings.NewReader("")); got != "a session finished its turn" {
		t.Errorf("stop fallback = %q", got)
	}
	if got := hookDetail(hookEventNotification, nil); got != "a session is waiting for you" {
		t.Errorf("nil stdin fallback = %q", got)
	}
	long := `{"message":"` + strings.Repeat("x", 300) + `"}`
	if got := hookDetail(hookEventNotification, strings.NewReader(long)); len([]rune(got)) > 161 {
		t.Errorf("detail must be truncated, got %d runes", len([]rune(got)))
	}
}

func TestWithCorgiHookReplacesOnlyOurs(t *testing.T) {
	other := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "echo mine"}}}
	first := withCorgiHook([]any{other}, "acme")
	if len(first) != 2 {
		t.Fatalf("entries = %d, want the existing one plus ours", len(first))
	}
	again := withCorgiHook(first, "acme")
	if len(again) != 2 {
		t.Errorf("re-running must replace ours, not add another: %d entries", len(again))
	}
	if !strings.Contains(marshalCompact(again), "--workspace acme") {
		t.Error("our hook must carry the workspace id")
	}
	stripped := stripCorgiHooks(again)
	if len(stripped) != 1 || !strings.Contains(marshalCompact(stripped), "echo mine") {
		t.Errorf("disable must leave other hooks alone: %v", stripped)
	}
}

func TestLauncherPageCarriesTheNewControls(t *testing.T) {
	rec := httptest.NewRecorder()
	launcherPageHandler(rec, httptest.NewRequest(http.MethodGet, "/app", nil))
	body := rec.Body.String()
	for _, want := range []string{
		"/launch/info", "/launch/devices", "/launch/doctor",
		"corgi_hidden", "data-role=\"profile\"", "ngrok-skip-browser-warning",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the launcher must carry %q", want)
		}
	}
	if strings.Contains(body, "src=\"http") || strings.Contains(body, "href=\"http") {
		t.Error("the launcher must stay self-contained")
	}
}
