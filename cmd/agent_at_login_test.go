package cmd

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

func atLoginCmd(t *testing.T) *cobra.Command {
	t.Helper()
	c := &cobra.Command{Use: "up"}
	addAgentUpFlags(c)
	return c
}

// A bare `up` must never install anything on its own — start-at-login writes a
// file into the user's login session, which is not a side effect of "start my
// endpoint".
func TestEnsureAtLoginInstallsNothingWhenNotAsked(t *testing.T) {
	dir := t.TempDir()
	utils.NonInteractive = true
	defer func() { utils.NonInteractive = false }()

	settings := upSettings{}
	ensureAtLogin(dir, atLoginCmd(t), &settings)

	if settings.AtLogin {
		t.Fatal("a bare up turned start-at-login on by itself")
	}
	if upSettingsExist(dir) {
		t.Fatal("a bare up wrote settings when it had nothing to record")
	}
}

// --at-login=false is how you turn it back off, and it must be remembered so
// the offer is not made again on the next up.
func TestEnsureAtLoginFalseRecordsTheDecision(t *testing.T) {
	dir := t.TempDir()
	cmd := atLoginCmd(t)
	if err := cmd.Flags().Set(atLoginFlag, "false"); err != nil {
		t.Fatal(err)
	}

	settings := upSettings{AtLogin: true}
	ensureAtLogin(dir, cmd, &settings)

	if settings.AtLogin {
		t.Fatal("--at-login=false left restore on")
	}
	if !settings.AtLoginAsked {
		t.Fatal("--at-login=false did not record that the question is settled")
	}
	if got := loadUpSettings(dir); got.AtLogin || !got.AtLoginAsked {
		t.Fatalf("saved settings not persisted: %+v", got)
	}
}

// Already on, service in place: nothing to do, and in particular no reinstall
// (which would shell out to launchctl on every up).
func TestEnsureAtLoginIsAnIdempotentNoOpWhenAlreadySet(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no login service on windows")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := loginServicePath()
	if path == "" {
		t.Skip("no login service path on " + runtime.GOOS)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	settings := upSettings{AtLogin: true, AtLoginAsked: true}
	ensureAtLogin(dir, atLoginCmd(t), &settings)

	if !settings.AtLogin {
		t.Fatal("an installed service was reported as not restoring")
	}
	if upSettingsExist(dir) {
		t.Fatal("nothing changed, so nothing should have been rewritten")
	}
}

func TestLoginServiceInstalledFollowsTheFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := loginServicePath()
	if path == "" {
		t.Skip("no login service path on " + runtime.GOOS)
	}
	if loginServiceInstalled() {
		t.Fatal("reported installed with no file")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !loginServiceInstalled() {
		t.Fatal("reported not installed with the file in place")
	}
}

// The daemon starts on every login, including for people who never asked for a
// tunnel. It must not open one.
func TestRestoreUpAtLoginDoesNothingWithoutTheFlag(t *testing.T) {
	dir := t.TempDir()
	if err := saveUpSettings(dir, upSettings{TunnelHostname: "corgi.example.com"}); err != nil {
		t.Fatal(err)
	}
	restoreUpAtLogin(dir)
	if _, err := os.Stat(filepath.Join(dir, mcpPidName)); err == nil {
		t.Fatal("an MCP was started for a daemon that was never told to restore one")
	}
}

// An `agent up` holding the lock is about to start the MCP itself; the daemon
// it just spawned must not race it for the port.
func TestRestoreUpAtLoginYieldsToARunningUp(t *testing.T) {
	dir := t.TempDir()
	if err := saveUpSettings(dir, upSettings{AtLogin: true}); err != nil {
		t.Fatal(err)
	}
	release, err := acquireUpLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	restoreUpAtLogin(dir)
	if _, err := os.Stat(filepath.Join(dir, mcpPidName)); err == nil {
		t.Fatal("the daemon started a second MCP while an up was running")
	}
}

// Not flags, so a bare `up` would drop them on the merge and silently switch
// start-at-login off.
func TestMergeUpSettingsCarriesTheAtLoginDecision(t *testing.T) {
	saved := upSettings{AtLogin: true, AtLoginAsked: true, TunnelHostname: "corgi.example.com"}
	got, _ := mergeUpSettings(upSettings{}, func(string) bool { return false }, saved)
	if !got.AtLogin || !got.AtLoginAsked {
		t.Fatalf("at-login decision lost on merge: %+v", got)
	}
}

func TestUpSettingsExist(t *testing.T) {
	dir := t.TempDir()
	if upSettingsExist(dir) {
		t.Fatal("reported settings in an empty dir")
	}
	if err := saveUpSettings(dir, upSettings{}); err != nil {
		t.Fatal(err)
	}
	if !upSettingsExist(dir) {
		t.Fatal("reported no settings after a successful up")
	}
}

func TestLoopbackAddr(t *testing.T) {
	cases := map[string]string{
		"0.0.0.0:8765":   "127.0.0.1:8765",
		"127.0.0.1:8765": "127.0.0.1:8765",
		":8765":          "127.0.0.1:8765",
		"nonsense":       "",
	}
	for in, want := range cases {
		if got := loopbackAddr(in); got != want {
			t.Errorf("loopbackAddr(%q) = %q, want %q", in, got, want)
		}
	}
}

// The toast is read at the machine corgi runs on: it opens the page served
// from localhost, not a round trip through the tunnel.
func TestPreferLocalLinkSwapsTheLauncherForLocalhost(t *testing.T) {
	data := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", data)
	dir, err := agentDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	recordPublicURL("https://corgi.example.com")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if err := os.WriteFile(filepath.Join(dir, mcpAddrName), []byte(ln.Addr().String()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := preferLocalLink("https://corgi.example.com/app")
	want := "http://" + ln.Addr().String() + "/app"
	if got != want {
		t.Fatalf("preferLocalLink = %q, want %q", got, want)
	}
}

// A session URL is the session itself — there is no local page that shows it,
// so rewriting it would send the click nowhere.
func TestPreferLocalLinkLeavesASessionURLAlone(t *testing.T) {
	data := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", data)
	session := "https://claude.ai/remote/abc123"
	if got := preferLocalLink(session); got != session {
		t.Fatalf("preferLocalLink rewrote a session URL to %q", got)
	}
}

func TestPreferLocalLinkKeepsThePublicURLWhenNothingListensLocally(t *testing.T) {
	data := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", data)
	dir, err := agentDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	recordPublicURL("https://corgi.example.com")

	link := "https://corgi.example.com/app"
	if got := preferLocalLink(link); got != link {
		t.Fatalf("preferLocalLink = %q, want the public URL back", got)
	}
}

func TestProtectedHomeFolder(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("TCC folders are macOS only")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	cases := map[string]string{
		filepath.Join(home, "Documents", "corgi"):               "Documents",
		filepath.Join(home, "Desktop"):                          "Desktop",
		filepath.Join(home, "Downloads", "a", "b"):              "Downloads",
		filepath.Join(home, "Library", "Mobile Documents", "x"): "iCloud Drive",
		filepath.Join(home, "dev", "stack"):                     "",
		filepath.Join(home, "DocumentsBackup"):                  "",
	}
	for path, want := range cases {
		if got := protectedHomeFolder(path); got != want {
			t.Errorf("protectedHomeFolder(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestProtectedWorkspaceNoteIsSilentOutsideTheGatedFolders(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if note := protectedWorkspaceNote(filepath.Join(home, "dev", "stack")); note != "" {
		t.Fatalf("warned about an unguarded directory: %q", note)
	}
}
