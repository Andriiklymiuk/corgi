package cmd

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/workspace"

	"github.com/spf13/cobra"
)

// stubSupervisor replaces launchctl/systemctl so the install paths can be
// exercised without loading a real job into the test runner's login session.
func stubSupervisor(t *testing.T, err error) *[][]string {
	t.Helper()
	var calls [][]string
	original := runSupervisorCommand
	runSupervisorCommand = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		return []byte("stub"), err
	}
	t.Cleanup(func() { runSupervisorCommand = original })
	return &calls
}

// tempAgentHome isolates both the data dir and the login-service path.
func tempAgentHome(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	dir, err := agentDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestInstallLoginServiceWritesTheServiceFile(t *testing.T) {
	if !installSupported() {
		t.Skip("no login service on " + runtime.GOOS)
	}
	tempAgentHome(t)
	calls := stubSupervisor(t, nil)

	if err := installLoginService(); err != nil {
		t.Fatal(err)
	}
	if !loginServiceInstalled() {
		t.Fatal("no service file after a successful install")
	}
	body, err := os.ReadFile(loginServicePath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "agent") {
		t.Fatalf("service file does not run the agent:\n%s", body)
	}
	if len(*calls) == 0 {
		t.Fatal("the supervisor was never told to load the new file")
	}
}

// A supervisor that refuses must surface as an error, not a silent half-install.
func TestInstallLoginServiceReportsASupervisorFailure(t *testing.T) {
	if !installSupported() {
		t.Skip("no login service on " + runtime.GOOS)
	}
	tempAgentHome(t)
	stubSupervisor(t, errors.New("boom"))

	if err := installLoginService(); err == nil {
		t.Fatal("a failing launchctl/systemctl was reported as success")
	}
}

func TestRunAgentUninstallRemovesTheFileAndClearsRestore(t *testing.T) {
	if !installSupported() {
		t.Skip("no login service on " + runtime.GOOS)
	}
	dir := tempAgentHome(t)
	stubSupervisor(t, nil)
	if err := installLoginService(); err != nil {
		t.Fatal(err)
	}
	if err := saveUpSettings(dir, upSettings{AtLogin: true, AtLoginAsked: true}); err != nil {
		t.Fatal(err)
	}

	runAgentUninstall(nil, nil)

	if loginServiceInstalled() {
		t.Fatal("the service file survived uninstall")
	}
	if loadUpSettings(dir).AtLogin {
		t.Fatal("uninstall left restore on, promising something that cannot happen")
	}
}

// `install` after an `up` should bring back what that up started, not half of it.
func TestAdoptSavedUpAtLogin(t *testing.T) {
	dir := tempAgentHome(t)
	if adoptSavedUpAtLogin() {
		t.Fatal("adopted an up that never happened")
	}
	if err := saveUpSettings(dir, upSettings{TunnelHostname: "corgi.example.com"}); err != nil {
		t.Fatal(err)
	}
	if !adoptSavedUpAtLogin() {
		t.Fatal("did not adopt a previous up")
	}
	if !loadUpSettings(dir).AtLogin {
		t.Fatal("adoption did not persist")
	}
	// Idempotent: a second install must not report a change it did not make.
	if !adoptSavedUpAtLogin() {
		t.Fatal("a second adopt disagreed with the first")
	}
}

func TestClearAtLoginLeavesTheTunnelSettingsAlone(t *testing.T) {
	dir := tempAgentHome(t)
	if err := saveUpSettings(dir, upSettings{AtLogin: true, TunnelHostname: "corgi.example.com"}); err != nil {
		t.Fatal(err)
	}
	clearAtLogin()
	got := loadUpSettings(dir)
	if got.AtLogin {
		t.Fatal("restore stayed on")
	}
	if got.TunnelHostname != "corgi.example.com" {
		t.Fatalf("the named tunnel was collateral damage: %+v", got)
	}
}

func TestReportLoginInstallSaysWhatComesBack(t *testing.T) {
	// Both branches print; the point is that neither panics and both are honest
	// about whether the tunnel is included.
	reportLoginInstall(true)
	reportLoginInstall(false)
}

func TestUnsupportedInstallErrorNamesThePlatform(t *testing.T) {
	if !strings.Contains(unsupportedInstallError().Error(), runtime.GOOS) {
		t.Fatal("the error should say which platform it is refusing")
	}
}

func TestCurrentUIDIsNumeric(t *testing.T) {
	if uid := currentUID(); uid == "" || strings.ContainsAny(uid, "abcdef") {
		t.Fatalf("currentUID = %q", uid)
	}
}

func TestEnableAtLoginInstallsAndRecords(t *testing.T) {
	if !installSupported() {
		t.Skip("no login service on " + runtime.GOOS)
	}
	dir := tempAgentHome(t)
	stubSupervisor(t, nil)

	settings := upSettings{}
	enableAtLogin(dir, &settings)

	if !settings.AtLogin || !settings.AtLoginAsked {
		t.Fatalf("settings = %+v", settings)
	}
	if !loginServiceInstalled() {
		t.Fatal("no service file")
	}
	if !loadUpSettings(dir).AtLogin {
		t.Fatal("the decision was not persisted")
	}
}

// The flag path must install even when a previous run said no.
func TestEnsureAtLoginFlagOverridesAPreviousNo(t *testing.T) {
	if !installSupported() {
		t.Skip("no login service on " + runtime.GOOS)
	}
	dir := tempAgentHome(t)
	stubSupervisor(t, nil)

	cmd := atLoginCmd(t)
	if err := cmd.Flags().Set(atLoginFlag, "true"); err != nil {
		t.Fatal(err)
	}
	settings := upSettings{AtLoginAsked: true}
	ensureAtLogin(dir, cmd, &settings)

	if !settings.AtLogin {
		t.Fatal("--at-login did not win over a remembered no")
	}
}

// A service file removed behind corgi's back is put back, not silently ignored.
func TestEnsureAtLoginReinstallsAMissingService(t *testing.T) {
	if !installSupported() {
		t.Skip("no login service on " + runtime.GOOS)
	}
	dir := tempAgentHome(t)
	calls := stubSupervisor(t, nil)

	settings := upSettings{AtLogin: true, AtLoginAsked: true}
	ensureAtLogin(dir, atLoginCmd(t), &settings)

	if !loginServiceInstalled() {
		t.Fatal("a missing service file was left missing")
	}
	if len(*calls) == 0 {
		t.Fatal("nothing was reloaded")
	}
}

func TestHintAtLoginDoesNotPanic(t *testing.T) {
	hintAtLogin()
}

func TestAwaitMCPBoundReturnsWhenThePortIsTaken(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	start := time.Now()
	awaitMCPBound(ln.Addr().String(), 5*time.Second)
	if time.Since(start) > 2*time.Second {
		t.Fatal("waited for a port that was already bound")
	}
}

func TestAwaitMCPBoundGivesUpQuietly(t *testing.T) {
	// A server that never binds logs its own failure; this must not hang.
	awaitMCPBound("127.0.0.1:1", 300*time.Millisecond)
}

// A named tunnel with no hostname cannot produce a usable URL, so the daemon
// must say so rather than spawn a server nobody can reach.
func TestRestoreUpAtLoginRefusesAnUnusableTunnel(t *testing.T) {
	dir := t.TempDir()
	if err := saveUpSettings(dir, upSettings{AtLogin: true, TunnelName: "corgi"}); err != nil {
		t.Fatal(err)
	}
	restoreUpAtLogin(dir)
	if _, err := os.Stat(filepath.Join(dir, mcpPidName)); err == nil {
		t.Fatal("spawned a server for a tunnel that cannot resolve")
	}
}

// Already listening: nothing to restore, and nothing must be started on top.
func TestRestoreUpAtLoginLeavesARunningEndpointAlone(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	dir := t.TempDir()
	if err := saveUpSettings(dir, upSettings{AtLogin: true, HTTP: ln.Addr().String()}); err != nil {
		t.Fatal(err)
	}
	restoreUpAtLogin(dir)
	if _, err := os.Stat(filepath.Join(dir, mcpPidName)); err == nil {
		t.Fatal("started a second server against a live port")
	}
}

func TestRunAgentAwakeTurnsItOnAndOff(t *testing.T) {
	dir := tempAgentHome(t)
	cmd := &cobra.Command{}

	runAgentAwake(cmd, []string{"on"})
	if !stayAwakeEnabled(dir) {
		t.Fatal("`awake on` did not turn it on")
	}
	printAwakeState(agentUserConfigPath(dir))

	runAgentAwake(cmd, []string{"off"})
	if stayAwakeEnabled(dir) {
		t.Fatal("`awake off` did not turn it off")
	}
	runAgentAwake(cmd, nil)
}

func TestRunAgentAwakeRejectsATypo(t *testing.T) {
	tempAgentHome(t)
	original := osExit
	var code int
	osExit = func(c int) { code = c }
	defer func() { osExit = original }()

	runAgentAwake(&cobra.Command{}, []string{"maybe"})
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 (usage)", code)
	}
}

func TestPrintAwakeStateWithNoConfig(t *testing.T) {
	printAwakeState(filepath.Join(t.TempDir(), "config.yml"))
}

func TestCorgiIsAdhocSignedReadsTheCodesignReport(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("codesign is macOS only")
	}
	original := codesignTeamProbe
	defer func() { codesignTeamProbe = original }()

	codesignTeamProbe = func(string) string { return "Signature=adhoc\nTeamIdentifier=not set\n" }
	if !corgiIsAdhocSigned() {
		t.Error("an ad-hoc signature was read as a real identity")
	}
	codesignTeamProbe = func(string) string { return "Authority=Developer ID Application: Someone\nTeamIdentifier=ABCDE12345\n" }
	if corgiIsAdhocSigned() {
		t.Error("a Developer ID signature was read as ad-hoc")
	}
	// Unreadable counts as signed: a wrong warning is worse than a missing one.
	codesignTeamProbe = func(string) string { return "" }
	if corgiIsAdhocSigned() {
		t.Error("an unreadable report should not produce a warning")
	}
}

func TestProtectedWorkspaceNoteExplainsTheRepeat(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("TCC folders are macOS only")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	original := codesignTeamProbe
	defer func() { codesignTeamProbe = original }()
	codesignTeamProbe = func(string) string { return "Signature=adhoc\n" }

	note := protectedWorkspaceNote(filepath.Join(home, "Documents", "stack"))
	if !strings.Contains(note, "Documents") || !strings.Contains(note, "upgrade") {
		t.Fatalf("note does not explain the repeat: %q", note)
	}
}

func TestCheckMacOSFileAccessGroupsWorkspacesByFolder(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("TCC folders are macOS only")
	}
	dir := tempAgentHome(t)
	home, _ := os.UserHomeDir()
	original := codesignTeamProbe
	defer func() { codesignTeamProbe = original }()
	codesignTeamProbe = func(string) string { return "Signature=adhoc\n" }

	registry := &workspace.Registry{}
	registry.Upsert(workspace.Workspace{ID: "api", AbsPath: filepath.Join(home, "Documents", "api"), Status: workspace.StatusOK})
	registry.Upsert(workspace.Workspace{ID: "web", AbsPath: filepath.Join(home, "Documents", "web"), Status: workspace.StatusOK})
	registry.Upsert(workspace.Workspace{ID: "safe", AbsPath: filepath.Join(home, "dev", "safe"), Status: workspace.StatusOK})
	if err := workspace.Save(agentRegistryPath(dir), registry); err != nil {
		t.Fatal(err)
	}

	check, ok := checkMacOSFileAccess()
	if !ok {
		t.Fatal("gated workspaces were not reported")
	}
	if !strings.Contains(check.Detail, "~/Documents (api, web)") {
		t.Fatalf("workspaces not grouped by folder: %q", check.Detail)
	}
	if strings.Contains(check.Detail, "safe") {
		t.Fatalf("a workspace outside the gated folders was reported: %q", check.Detail)
	}
}

func TestCheckMacOSFileAccessSilentWithNothingGated(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("TCC folders are macOS only")
	}
	dir := tempAgentHome(t)
	home, _ := os.UserHomeDir()
	registry := &workspace.Registry{}
	registry.Upsert(workspace.Workspace{ID: "safe", AbsPath: filepath.Join(home, "dev", "safe"), Status: workspace.StatusOK})
	if err := workspace.Save(agentRegistryPath(dir), registry); err != nil {
		t.Fatal(err)
	}
	if _, ok := checkMacOSFileAccess(); ok {
		t.Fatal("reported a dialog nobody will ever see")
	}
}

func TestCheckInstallSupportReportsWhatIsInstalled(t *testing.T) {
	if !installSupported() {
		t.Skip("no login service on " + runtime.GOOS)
	}
	dir := tempAgentHome(t)
	stubSupervisor(t, nil)

	if c := checkInstallSupport(); !strings.Contains(c.Detail, "not installed") {
		t.Fatalf("a machine with no service file read as installed: %q", c.Detail)
	}
	if err := installLoginService(); err != nil {
		t.Fatal(err)
	}
	if c := checkInstallSupport(); !strings.Contains(c.Detail, "daemon only") {
		t.Fatalf("detail = %q, want the daemon-only scope", c.Detail)
	}
	if err := saveUpSettings(dir, upSettings{AtLogin: true}); err != nil {
		t.Fatal(err)
	}
	if c := checkInstallSupport(); !strings.Contains(c.Detail, "tunnel") {
		t.Fatalf("detail = %q, want the endpoint and tunnel named", c.Detail)
	}
}

func TestWakeLockScopeNamesTheGap(t *testing.T) {
	dir := tempAgentHome(t)
	if !strings.Contains(wakeLockScope(), "per session") {
		t.Fatal("the default scope should say the machine may sleep")
	}
	if err := writeStayAwake(agentUserConfigPath(dir), true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(wakeLockScope(), "daemon") {
		t.Fatalf("scope = %q", wakeLockScope())
	}
}

func TestPreferLocalLinkPassesAnEmptyLinkThrough(t *testing.T) {
	tempAgentHome(t)
	if got := preferLocalLink(""); got != "" {
		t.Fatalf("preferLocalLink(\"\") = %q", got)
	}
}
