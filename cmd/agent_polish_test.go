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

	"github.com/spf13/cobra"
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

	live, _, last := workspaceActivity("acme", "", "")
	if live != 0 {
		t.Errorf("live = %d with no workspace path", live)
	}
	if last == nil || last.Kind != "exited" || last.Cause != "network-timeout" || last.At == 0 {
		t.Errorf("lastEvent = %+v", last)
	}

	if _, _, none := workspaceActivity("ghost", "", ""); none != nil {
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
	first := withCorgiHook([]any{other}, "acme", false)
	if len(first) != 2 {
		t.Fatalf("entries = %d, want the existing one plus ours", len(first))
	}
	again := withCorgiHook(first, "acme", false)
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
		"corgi_hidden", "dataset.role", "ngrok-skip-browser-warning",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the launcher must carry %q", want)
		}
	}
	if strings.Contains(body, "src=\"http") || strings.Contains(body, "href=\"http") {
		t.Error("the launcher must stay self-contained")
	}
}

func TestWorkspaceUsageIsOmittedWhenIdle(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", dir)
	t.Setenv("HOME", t.TempDir())
	agentD, _ := agentDir()
	stack := stackWithAgentConfig(t, "")
	registerStack(t, agentD, "acme", stack)

	if got := workspaceUsage("acme", stack, ""); got != nil {
		t.Errorf("the first call must not block on a scan, got %+v", got)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		usageCache.mu.Lock()
		done := usageCache.reps["acme"] != nil && !usageCache.reps["acme"].computed.IsZero()
		usageCache.mu.Unlock()
		if done {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := workspaceUsage("acme", stack, ""); got != nil {
		t.Errorf("a workspace with no transcripts must report no usage, got %+v", got)
	}
	if got := workspaceUsage("acme", "", ""); got != nil {
		t.Errorf("no path means no usage, got %+v", got)
	}
	if line := workspaceUsageLine("acme"); line != "" {
		t.Errorf("status must print nothing when there is no usage, got %q", line)
	}
}

func TestFormatTokens(t *testing.T) {
	cases := map[int64]string{0: "0", 999: "999", 1500: "1k", 2_400_000: "2.4M", 1_050_000_000: "1.1B"}
	for in, want := range cases {
		if got := formatTokens(in); got != want {
			t.Errorf("formatTokens(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestAgentHooksEnableThenDisableRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", dir)
	agentD, _ := agentDir()
	stack := stackWithAgentConfig(t, "")
	registerStack(t, agentD, "acme", stack)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(stack); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	path := claudeLocalSettingsPath(stack)
	if err := writeJSONObject(path, map[string]any{
		"hooks": map[string]any{"Notification": []any{
			map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "echo mine"}}},
		}},
		"other": "kept",
	}); err != nil {
		t.Fatal(err)
	}

	runAgentHooksEnable(nil, nil)
	after := readJSONObject(path)
	hooks, _ := after["hooks"].(map[string]any)
	if hooks == nil || hooks[hookEventNotification] == nil {
		t.Fatalf("the needs-you event must be hooked, got %v", after["hooks"])
	}
	if !strings.Contains(marshalCompact(hooks), "--workspace acme") {
		t.Errorf("our hook must name the workspace: %s", marshalCompact(hooks))
	}
	if after["other"] != "kept" {
		t.Error("unrelated settings must survive")
	}

	runAgentHooksDisable(nil, nil)
	after = readJSONObject(path)
	body := marshalCompact(after)
	if strings.Contains(body, hookMarker) {
		t.Errorf("disable must remove our hooks: %s", body)
	}
	if !strings.Contains(body, "echo mine") {
		t.Errorf("disable must leave other hooks alone: %s", body)
	}
	if after["other"] != "kept" {
		t.Error("disable must not touch unrelated settings")
	}
}

func TestReadJSONObjectTolerates(t *testing.T) {
	dir := t.TempDir()
	if got := readJSONObject(filepath.Join(dir, "nope.json")); len(got) != 0 {
		t.Errorf("a missing file is an empty object, got %v", got)
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readJSONObject(bad); len(got) != 0 {
		t.Errorf("unparseable settings must not be treated as content, got %v", got)
	}
}

func TestLaunchProfileNamesFromTrustedConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", dir)
	agentD, _ := agentDir()
	if err := os.MkdirAll(agentD, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := launchProfileNames(); got != nil {
		t.Errorf("no config means no profiles, got %v", got)
	}
	cfg := "version: 1\nprofiles:\n  work:\n    configDir: ~/.claude-work\n  personal:\n    configDir: ~/.claude\n"
	if err := os.WriteFile(agentUserConfigPath(agentD), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	got := launchProfileNames()
	if len(got) != 2 || got[0] != "personal" || got[1] != "work" {
		t.Errorf("profiles = %v, want them sorted", got)
	}
}

func TestRunAgentHookIsSilentWithoutADaemon(t *testing.T) {
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	c := &cobra.Command{}
	c.Flags().String("workspace", "acme", "")

	// No daemon, no workspace flag, and a junk payload: every path must return
	// quietly, because anything printed lands in the user's Claude session.
	runAgentHook(c, []string{hookEventNotification})
	runAgentHook(c, nil)
	empty := &cobra.Command{}
	empty.Flags().String("workspace", "", "")
	runAgentHook(empty, []string{hookEventStop})
}

func TestExecRunnerReportsAFailure(t *testing.T) {
	if _, err := execRunner("sh", "-c", "exit 3"); err == nil {
		t.Error("a failing command must report an error")
	}
	if out, err := execRunner("sh", "-c", "echo hi"); err != nil || !strings.Contains(out, "hi") {
		t.Errorf("out=%q err=%v", out, err)
	}
}

func TestLookPathFindsAndMisses(t *testing.T) {
	if err := lookPath("sh"); err != nil {
		t.Errorf("sh must be found: %v", err)
	}
	if err := lookPath("corgi-not-a-real-binary"); err == nil {
		t.Error("a missing binary must report an error")
	}
}

func TestSetupNgrokTunnelChecksTheAuthtoken(t *testing.T) {
	installed := func(string) error { return nil }
	failing := func(string, ...string) (string, error) { return "", fmt.Errorf("no authtoken") }
	if err := setupNgrokTunnel(failing, installed, "x.ngrok-free.app"); err == nil ||
		!strings.Contains(err.Error(), "authtoken") {
		t.Errorf("a missing authtoken must be reported with the fix, got %v", err)
	}
	ok := func(string, ...string) (string, error) { return "", nil }
	if err := setupNgrokTunnel(ok, installed, "x.ngrok-free.app"); err != nil {
		t.Errorf("a configured ngrok must pass, got %v", err)
	}
}

func tunnelSetupCmd(t *testing.T, provider string, dryRun bool) *cobra.Command {
	t.Helper()
	c := &cobra.Command{}
	c.Flags().String("provider", provider, "")
	c.Flags().String("name", "", "")
	c.Flags().Bool("dry-run", dryRun, "")
	return c
}

func TestRunAgentTunnelSetupSavesTheSettings(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", dir)
	agentD, _ := agentDir()
	if err := os.MkdirAll(agentD, 0o700); err != nil {
		t.Fatal(err)
	}

	var ran []string
	origExec, origLook := tunnelExec, tunnelLookup
	t.Cleanup(func() { tunnelExec, tunnelLookup = origExec, origLook })
	tunnelExec = func(name string, args ...string) (string, error) {
		ran = append(ran, name+" "+strings.Join(args, " "))
		if len(args) > 1 && args[1] == "list" {
			return "", nil
		}
		return "", nil
	}
	tunnelLookup = func(string) error { return nil }

	runAgentTunnelSetup(tunnelSetupCmd(t, "cloudflared", false), []string{"corgi.example.com"})

	saved := loadUpSettings(agentD)
	if saved.TunnelHostname != "corgi.example.com" || saved.TunnelName != "corgi-agent" || saved.Provider != "cloudflared" {
		t.Fatalf("settings = %+v", saved)
	}
	joined := strings.Join(ran, "\n")
	if !strings.Contains(joined, "tunnel create corgi-agent") {
		t.Errorf("a tunnel missing from the list must be created:\n%s", joined)
	}
	if !strings.Contains(joined, "route dns corgi-agent corgi.example.com") {
		t.Errorf("the DNS route must run:\n%s", joined)
	}
}

func TestRunAgentTunnelSetupDryRunSavesNothing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", dir)
	agentD, _ := agentDir()
	if err := os.MkdirAll(agentD, 0o700); err != nil {
		t.Fatal(err)
	}
	origExec, origLook := tunnelExec, tunnelLookup
	t.Cleanup(func() { tunnelExec, tunnelLookup = origExec, origLook })
	tunnelExec = func(string, ...string) (string, error) { return "", nil }
	tunnelLookup = func(string) error { return nil }

	runAgentTunnelSetup(tunnelSetupCmd(t, "cloudflared", true), []string{"corgi.example.com"})
	if saved := loadUpSettings(agentD); saved != (upSettings{}) {
		t.Errorf("a dry run must save nothing, got %+v", saved)
	}
}

func TestRunAgentTunnelSetupNgrokSavesNoName(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", dir)
	agentD, _ := agentDir()
	if err := os.MkdirAll(agentD, 0o700); err != nil {
		t.Fatal(err)
	}
	origExec, origLook := tunnelExec, tunnelLookup
	t.Cleanup(func() { tunnelExec, tunnelLookup = origExec, origLook })
	tunnelExec = func(string, ...string) (string, error) { return "", nil }
	tunnelLookup = func(string) error { return nil }

	runAgentTunnelSetup(tunnelSetupCmd(t, "ngrok", false), []string{"x.ngrok-free.app"})
	saved := loadUpSettings(agentD)
	if saved.Provider != "ngrok" || saved.TunnelHostname != "x.ngrok-free.app" || saved.TunnelName != "" {
		t.Errorf("settings = %+v", saved)
	}
}

func TestSamePathThroughSymlinks(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks unavailable")
	}
	if !samePath(real, link) {
		t.Error("a symlink and its target are the same directory")
	}
	if !samePath(real, real) {
		t.Error("an identical path must match without resolving")
	}
	if samePath(real, filepath.Join(real, "nope")) {
		t.Error("different directories must not match")
	}
	if samePath("/no/such/a", "/no/such/b") {
		t.Error("unresolvable paths must not match")
	}
}

func TestWebhookNotifierSendsTheClickTarget(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", dir)
	agentD, _ := agentDir()
	if err := os.MkdirAll(agentD, 0o700); err != nil {
		t.Fatal(err)
	}
	recordPublicURL("https://x.trycloudflare.com")

	got := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.Header.Get("Click")
	}))
	defer srv.Close()

	notify := webhookNotifier(srv.URL, srv.Client())
	notify("corgi agent", "session restarted")
	select {
	case click := <-got:
		if click != "https://x.trycloudflare.com/app" {
			t.Errorf("Click = %q", click)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("webhook never arrived")
	}
}

func TestWorkspaceActivityCountsLiveSessions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", dir)
	home := t.TempDir()
	t.Setenv("HOME", home)
	agentD, _ := agentDir()
	stack := stackWithAgentConfig(t, "")
	registerStack(t, agentD, "acme", stack)

	sessions := filepath.Join(home, ".claude", "sessions")
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		t.Fatal(err)
	}
	row := fmt.Sprintf(`{"pid":%d,"sessionId":"u1","cwd":%q,"kind":"interactive","startedAt":1,"name":"acme-0a"}`, os.Getpid(), stack)
	if err := os.WriteFile(filepath.Join(sessions, "1.json"), []byte(row), 0o600); err != nil {
		t.Fatal(err)
	}

	live, _, _ := workspaceActivity("acme", stack, "")
	if live != 1 {
		t.Errorf("live = %d, want the one running process", live)
	}
}

func TestRefreshUsageStoresAReport(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", dir)
	cfg := t.TempDir()
	repo := "/dev/app"
	proj := filepath.Join(cfg, "projects", mungeClaudeProjectDir(repo))
	if err := os.MkdirAll(proj, 0o700); err != nil {
		t.Fatal(err)
	}
	row := fmt.Sprintf(`{"timestamp":"%s","message":{"usage":{"input_tokens":10,"output_tokens":5}}}`, time.Now().Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(proj, "a.jsonl"), []byte(row+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	usageCache.mu.Lock()
	usageCache.reps["acme"] = &cachedUsage{refreshing: true}
	usageCache.mu.Unlock()
	refreshUsage("acme", repo, cfg)

	usageCache.mu.Lock()
	entry := usageCache.reps["acme"]
	usageCache.mu.Unlock()
	if entry.report == nil || entry.report.Today.Total() != 15 {
		t.Fatalf("report = %+v", entry.report)
	}
	if entry.refreshing || entry.computed.IsZero() {
		t.Error("a finished refresh must clear the flag and stamp the time")
	}
}

func TestLaunchInfoReportsARunningDaemon(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", dir)
	agentD, _ := agentDir()
	if err := os.MkdirAll(agentD, 0o700); err != nil {
		t.Fatal(err)
	}
	info := map[string]any{
		"pid": os.Getpid(), "version": APP_VERSION, "startedAt": time.Now().Format(time.RFC3339),
		"executable": mustExecutable(t), "workspaces": []string{}, "commands": true,
	}
	data, _ := json.Marshal(info)
	if err := os.WriteFile(filepath.Join(agentD, "daemon.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	launchInfoHandler(rec, httptest.NewRequest(http.MethodGet, "/launch/info", nil))
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["daemon"] != true {
		t.Errorf("a running daemon must be reported, got %v", body)
	}
}

func mustExecutable(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return exe
}
