package utils

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func previewFixture(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// livePreview records a preview backed by a real, long-running process so
// liveness checks behave as they would against a tunnel.
func livePreview(t *testing.T, dir, service string) Preview {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	SetProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// Reap it the way the production spawner does. Without this the killed
	// child lingers as a zombie owned by the test process, and a zombie still
	// answers kill(pid, 0) — so a liveness assertion would pass either way.
	waited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(waited) }()
	t.Cleanup(func() {
		_ = KillProcessGroup(cmd.Process.Pid)
		<-waited
	})

	p := Preview{
		ID:          service,
		Service:     service,
		Port:        65000,
		PID:         cmd.Process.Pid,
		State:       PreviewStarting,
		StartedAt:   time.Now().UTC(),
		LastTouched: time.Now().UTC(),
		IdleMinutes: DefaultPreviewIdleMinutes,
	}
	store, err := LoadPreviews(dir)
	if err != nil {
		t.Fatal(err)
	}
	store.Previews = append(store.Previews, p)
	if err := SavePreviews(dir, store); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestStartPreviewRefusesASensitiveWorkspace(t *testing.T) {
	_, err := StartPreview(PreviewOptions{
		ComposeDir: previewFixture(t),
		Workspace:  "client-work",
		Service:    "web",
		Port:       3000,
		Sensitive:  true,
	})

	if err == nil {
		t.Fatal("a sensitive workspace must never open a public tunnel")
	}
	if !strings.Contains(err.Error(), "corgi_diff") {
		t.Errorf("the refusal should point at the alternative that needs no tunnel, got %q", err)
	}
}

func TestStartPreviewRequiresAPort(t *testing.T) {
	_, err := StartPreview(PreviewOptions{ComposeDir: previewFixture(t), Service: "web"})

	if err == nil {
		t.Fatal("a service with no port cannot be tunneled")
	}
}

func TestExpiredIgnoresFrozenPreviews(t *testing.T) {
	now := time.Now()
	stale := Preview{IdleMinutes: 20, LastTouched: now.Add(-time.Hour)}

	if !stale.Expired(now) {
		t.Error("an untouched preview past its idle window should expire")
	}
	stale.Frozen = true
	if stale.Expired(now) {
		t.Error("freezing means someone is reading it; it must not be reaped underneath them")
	}
}

func TestExpiredWithNoIdleWindowNeverExpires(t *testing.T) {
	p := Preview{IdleMinutes: 0, LastTouched: time.Now().Add(-100 * time.Hour)}

	if p.Expired(time.Now()) {
		t.Error("idleMinutes 0 means no reaping")
	}
}

func TestReapRemovesPreviewsWhoseProcessIsGone(t *testing.T) {
	dir := previewFixture(t)
	store := &PreviewStore{Previews: []Preview{
		{ID: "web", Service: "web", PID: 0, LastTouched: time.Now()},
	}}
	if err := SavePreviews(dir, store); err != nil {
		t.Fatal(err)
	}

	reaped, err := ReapPreviews(dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	if len(reaped) != 1 {
		t.Fatalf("reaped %d, want 1 — a dead tunnel must not linger in the store", len(reaped))
	}
	after, _ := LoadPreviews(dir)
	if len(after.Previews) != 0 {
		t.Error("the store should be empty after reaping")
	}
}

func TestReapKeepsLivePreviews(t *testing.T) {
	dir := previewFixture(t)
	livePreview(t, dir, "web")

	reaped, err := ReapPreviews(dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	if len(reaped) != 0 {
		t.Errorf("reaped %d live previews, want 0", len(reaped))
	}
}

func TestReapTearsDownAnIdlePreview(t *testing.T) {
	dir := previewFixture(t)
	p := livePreview(t, dir, "web")

	// Rewind so it looks abandoned.
	store, _ := LoadPreviews(dir)
	store.Previews[0].LastTouched = time.Now().Add(-2 * time.Hour)
	if err := SavePreviews(dir, store); err != nil {
		t.Fatal(err)
	}

	reaped, err := ReapPreviews(dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	if len(reaped) != 1 {
		t.Fatalf("reaped %d, want 1 — a forgotten preview is a public URL onto seeded data", len(reaped))
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && PidAlive(p.PID, "") {
		time.Sleep(20 * time.Millisecond)
	}
	if PidAlive(p.PID, "") {
		t.Error("reaping must actually kill the tunnel, not just forget it")
	}
}

func TestFreezeSurvivesReaping(t *testing.T) {
	dir := previewFixture(t)
	livePreview(t, dir, "web")

	if _, err := FreezePreview(dir, "web", true); err != nil {
		t.Fatal(err)
	}
	store, _ := LoadPreviews(dir)
	store.Previews[0].LastTouched = time.Now().Add(-2 * time.Hour)
	if err := SavePreviews(dir, store); err != nil {
		t.Fatal(err)
	}

	reaped, err := ReapPreviews(dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	if len(reaped) != 0 {
		t.Error("a frozen preview must not be reaped out from under someone reading it")
	}
}

func TestFreezeUnknownPreview(t *testing.T) {
	if _, err := FreezePreview(previewFixture(t), "nope", true); err == nil {
		t.Error("freezing an unknown preview should error rather than silently do nothing")
	}
}

func TestStopRemovesFromTheStore(t *testing.T) {
	dir := previewFixture(t)
	livePreview(t, dir, "web")

	if err := StopPreview(dir, "web"); err != nil {
		t.Fatal(err)
	}

	after, _ := LoadPreviews(dir)
	if len(after.Previews) != 0 {
		t.Error("stopping should remove the entry")
	}
}

func TestPreviewStateReflectsALostTunnel(t *testing.T) {
	p := &Preview{ID: "web", Service: "web", PID: 0, State: PreviewReady, URL: "https://x.trycloudflare.com"}

	refreshPreviewFromLog(p)

	if p.State != PreviewStopped {
		t.Errorf("state = %q, want %q — a dead tunnel must not still read as ready", p.State, PreviewStopped)
	}
	if p.Error == "" {
		t.Error("a stopped preview should say why")
	}
}

func TestPreviewPicksUpTheURLFromTheTunnelLog(t *testing.T) {
	dir := previewFixture(t)
	p := livePreview(t, dir, "web")

	logFile := filepath.Join(dir, "tunnel.log")
	body := "2026-08-14T10:00:00Z INF |  https://kind-zebra-42.trycloudflare.com  |\n"
	if err := os.WriteFile(logFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	p.LogFile = logFile

	refreshPreviewFromLog(&p)

	if p.URL != "https://kind-zebra-42.trycloudflare.com" {
		t.Errorf("url = %q, want the one the tunnel printed", p.URL)
	}
	// Nothing is listening on the port, so this is the honest answer rather
	// than handing over a URL that shows a stack trace.
	if p.State != PreviewBroken {
		t.Errorf("state = %q, want %q when the port does not answer", p.State, PreviewBroken)
	}
	if p.Error == "" {
		t.Error("broken must carry a reason for the banner")
	}
}

func TestTunnelURLPatternMatchesEveryProvider(t *testing.T) {
	cases := map[string]string{
		"https://kind-zebra-42.trycloudflare.com": "cloudflared",
		"https://abcd-1-2-3-4.ngrok-free.app":     "ngrok free",
		"https://abcd.ngrok.io":                   "ngrok",
		"https://tidy-fish-12.loca.lt":            "localtunnel",
	}
	for url, provider := range cases {
		if got := anyTunnelURL.FindString("some log line " + url + " trailing"); got != url {
			t.Errorf("%s: extracted %q, want %q", provider, got, url)
		}
	}
}

func TestPreviewIDIsStablePerServiceAndBranch(t *testing.T) {
	if a, b := previewID("web", "feature/x"), previewID("web", "feature/x"); a != b {
		t.Error("the same service and branch must produce the same id, so re-asking finds the existing preview")
	}
	if strings.Contains(previewID("web", "feature/x"), "/") {
		t.Error("the id becomes a filename, so it must not contain a separator")
	}
	if previewID("web", "") == previewID("web", "feature/x") {
		t.Error("different branches are different previews")
	}
}

func TestSavePreviewsAddsGitignoreEntries(t *testing.T) {
	dir := previewFixture(t)

	if err := SavePreviews(dir, &PreviewStore{}); err != nil {
		t.Fatal(err)
	}

	// corgi_services/ is not wholly ignored, so per-developer state under it
	// must add its own entries or it shows up as untracked.
	data, err := os.ReadFile(filepath.Join(dir, "corgi_services", ".gitignore"))
	if err != nil {
		t.Fatalf("no .gitignore written: %v", err)
	}
	for _, want := range []string{"previews.json", ".previews/"} {
		if !strings.Contains(string(data), want) {
			t.Errorf(".gitignore missing %q, got %q", want, data)
		}
	}
}

func TestLoadPreviewsMissingIsEmpty(t *testing.T) {
	store, err := LoadPreviews(previewFixture(t))

	if err != nil {
		t.Fatalf("a first run must not error, got %v", err)
	}
	if len(store.Previews) != 0 {
		t.Error("want an empty store")
	}
}

func TestSavePreviewsLeavesNoTempFile(t *testing.T) {
	dir := previewFixture(t)
	if err := SavePreviews(dir, &PreviewStore{}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dir, "corgi_services", "previews.json.tmp")); !os.IsNotExist(err) {
		t.Error("the temp file must be renamed away")
	}
}

// The tunnel argv is what a preview actually depends on. An earlier version
// passed --tunnel-name and --tunnel-hostname, which `corgi tunnel` does not
// define, so every named-tunnel preview died instantly with "unknown flag"
// while still reporting "starting".
func TestPreviewSpawnsTunnelWithFlagsThatExist(t *testing.T) {
	dir := previewFixture(t)
	bin := filepath.Join(t.TempDir(), "fake-corgi")
	argvFile := filepath.Join(dir, "argv.txt")
	script := "#!/bin/sh\necho \"$@\" > " + argvFile + "\nsleep 30\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	p, err := StartPreview(PreviewOptions{
		ComposeDir: dir,
		Workspace:  "acme",
		Service:    "web",
		Port:       3000,
		Provider:   "cloudflared",
		CorgiBin:   bin,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = StopPreview(dir, p.ID) })

	deadline := time.Now().Add(5 * time.Second)
	var argv []byte
	for time.Now().Before(deadline) {
		if argv, err = os.ReadFile(argvFile); err == nil && len(argv) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	got := strings.TrimSpace(string(argv))
	if got == "" {
		t.Fatal("the tunnel process never ran")
	}
	if !strings.Contains(got, "tunnel web") || !strings.Contains(got, "--provider cloudflared") {
		t.Errorf("argv = %q, want `tunnel web --provider cloudflared`", got)
	}
	for _, invented := range []string{"--tunnel-name", "--tunnel-hostname"} {
		if strings.Contains(got, invented) {
			t.Errorf("argv contains %s, which corgi tunnel does not define", invented)
		}
	}
}

func TestStartPreviewRecordsAndReusesTheSameTunnel(t *testing.T) {
	dir := previewFixture(t)
	bin := filepath.Join(t.TempDir(), "fake-corgi")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	opts := PreviewOptions{ComposeDir: dir, Workspace: "acme", Service: "web", Port: 3000, CorgiBin: bin}

	first, err := StartPreview(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = StopPreview(dir, first.ID) })

	second, err := StartPreview(opts)
	if err != nil {
		t.Fatal(err)
	}

	if second.PID != first.PID {
		t.Error("a second request for the same service must reuse the live tunnel — a new one would hand the user a different URL for the same thing")
	}
	store, _ := LoadPreviews(dir)
	if len(store.Previews) != 1 {
		t.Errorf("previews = %d, want 1", len(store.Previews))
	}
}

func TestPreviewStatusAndListReportTheRecordedPreview(t *testing.T) {
	dir := previewFixture(t)
	livePreview(t, dir, "web")

	byID, err := PreviewStatus(dir, "web")
	if err != nil {
		t.Fatal(err)
	}
	if byID.Service != "web" {
		t.Errorf("service = %q", byID.Service)
	}

	all, err := ListPreviews(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Errorf("ListPreviews returned %d, want 1", len(all))
	}
}

func TestPreviewStatusUnknownID(t *testing.T) {
	if _, err := PreviewStatus(previewFixture(t), "nope"); err == nil {
		t.Error("an unknown preview should error rather than return a zero value")
	}
}

func TestStopPreviewUnknownID(t *testing.T) {
	if err := StopPreview(previewFixture(t), "nope"); err == nil {
		t.Error("stopping an unknown preview should error")
	}
}

func TestPreviewDirIsUnderCorgiServices(t *testing.T) {
	if got := PreviewDir("/stack"); got != filepath.Join("/stack", "corgi_services", ".previews") {
		t.Errorf("PreviewDir = %q", got)
	}
}

func TestReapOnAnUntouchedStackIsHarmless(t *testing.T) {
	reaped, err := ReapPreviews(previewFixture(t), time.Now())

	if err != nil {
		t.Fatalf("reaping nothing must not error, got %v", err)
	}
	if len(reaped) != 0 {
		t.Errorf("reaped = %v, want nothing", reaped)
	}
}

func TestFreezeThenUnfreeze(t *testing.T) {
	dir := previewFixture(t)
	livePreview(t, dir, "web")

	frozen, err := FreezePreview(dir, "web", true)
	if err != nil || !frozen.Frozen {
		t.Fatalf("freeze failed: %+v %v", frozen, err)
	}
	thawed, err := FreezePreview(dir, "web", false)
	if err != nil || thawed.Frozen {
		t.Fatalf("unfreeze failed: %+v %v", thawed, err)
	}
}
