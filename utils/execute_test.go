package utils

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWithEnvSource_WhenFileExists(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	if err := os.WriteFile(envFile, []byte("FOO=bar\n"), 0644); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	got := withEnvSource("npx vite --port $PORT", envFile)
	if !strings.HasPrefix(got, "set -a; .") {
		t.Fatalf("expected prefix, got: %q", got)
	}
	if !strings.HasSuffix(got, "npx vite --port $PORT") {
		t.Fatalf("expected original command preserved, got: %q", got)
	}
	if !strings.Contains(got, envFile) {
		t.Fatalf("expected env file path in command, got: %q", got)
	}
}

func TestWithEnvSource_WhenFileMissing(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist.env")
	cmd := "npm install"
	got := withEnvSource(cmd, missing)
	if got != cmd {
		t.Fatalf("expected unchanged, got: %q", got)
	}
}

func TestWithEnvSource_WhenEnvFileEmptyString(t *testing.T) {
	cmd := "orbctl start"
	got := withEnvSource(cmd, "")
	if got != cmd {
		t.Fatalf("expected unchanged when envFile empty, got: %q", got)
	}
}

func TestResolveEnvFile_DefaultsToDotEnv(t *testing.T) {
	got := resolveEnvFile("/services/api", nil)
	want := "/services/api/.env"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveEnvFile_HonorsRelativeOverride(t *testing.T) {
	got := resolveEnvFile("/services/mobile", []string{".env.local"})
	want := "/services/mobile/.env.local"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveEnvFile_HonorsAbsoluteOverride(t *testing.T) {
	got := resolveEnvFile("/services/x", []string{"/etc/myenv"})
	want := "/etc/myenv"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveEnvFile_EmptyOverrideUsesDefault(t *testing.T) {
	got := resolveEnvFile("/services/x", []string{""})
	want := "/services/x/.env"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveEnvFile_OffSentinelDisables(t *testing.T) {
	got := resolveEnvFile("/services/api", []string{SkipAutoSourceEnv})
	if got != "" {
		t.Fatalf("expected empty when SkipAutoSourceEnv sentinel passed, got %q", got)
	}
}

func TestResolveEnvFile_NoPathDisables(t *testing.T) {
	if got := resolveEnvFile("", nil); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
	if got := resolveEnvFile("", []string{".env.local"}); got != "" {
		t.Fatalf("expected empty when path empty, got %q", got)
	}
}

func TestWithEnvSourceEmptyEnvFile(t *testing.T) {
	if got := withEnvSource("echo hi", ""); got != "echo hi" {
		t.Errorf("got %q", got)
	}
}

func TestWithEnvSourceMissingFile(t *testing.T) {
	if got := withEnvSource("echo hi", "/nonexistent/file/zzz"); got != "echo hi" {
		t.Errorf("got %q", got)
	}
}

func TestWithEnvSourceFileExists(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	if err := os.WriteFile(envFile, []byte("FOO=1"), 0644); err != nil {
		t.Fatal(err)
	}
	got := withEnvSource("echo hi", envFile)
	if !strings.Contains(got, "set -a") || !strings.Contains(got, "set +a") || !strings.Contains(got, "echo hi") {
		t.Errorf("got %q", got)
	}
}

func TestResolveEnvFileEmptyPath(t *testing.T) {
	if got := resolveEnvFile("", nil); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestResolveEnvFileNoOverride(t *testing.T) {
	got := resolveEnvFile("/srv/api", nil)
	if got != "/srv/api/.env" {
		t.Errorf("got %q", got)
	}
}

func TestResolveEnvFileSkipSentinel(t *testing.T) {
	got := resolveEnvFile("/srv/api", []string{SkipAutoSourceEnv})
	if got != "" {
		t.Errorf("got %q", got)
	}
}

func TestResolveEnvFileAbsoluteOverride(t *testing.T) {
	got := resolveEnvFile("/srv/api", []string{"/etc/.env.prod"})
	if got != "/etc/.env.prod" {
		t.Errorf("got %q", got)
	}
}

func TestResolveEnvFileRelativeOverride(t *testing.T) {
	got := resolveEnvFile("/srv/api", []string{".env.local"})
	if got != "/srv/api/.env.local" {
		t.Errorf("got %q", got)
	}
}

func TestResolveEnvFileEmptyOverride(t *testing.T) {
	got := resolveEnvFile("/srv/api", []string{""})
	if got != "/srv/api/.env" {
		t.Errorf("got %q want default fallback", got)
	}
}

func TestRunCombinedCmdEcho(t *testing.T) {
	if err := RunCombinedCmd("echo hi", ""); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunCombinedCmdMissing(t *testing.T) {
	err := RunCombinedCmd("cmd-not-found-zzz arg", "")
	if err == nil {
		t.Error("expected err")
	}
}

func TestKillAllStoredProcessesNoOp(t *testing.T) {
	prev := ProcessHandles
	ProcessHandles = nil
	t.Cleanup(func() { ProcessHandles = prev })
	KillAllStoredProcesses()
}

func TestRunServiceCmdEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := RunServiceCmd("svc", "", dir, false); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunServiceCmdEcho(t *testing.T) {
	dir := t.TempDir()
	if err := RunServiceCmd("svc", "echo hello", dir, false); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunServiceCmdAccumulatesBackslashLines(t *testing.T) {
	dir := t.TempDir()
	if err := RunServiceCmd("svc", "echo \\\nhi", dir, false); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRunServiceCommandsParallel(t *testing.T) {
	dir := t.TempDir()
	RunServiceCommands("test", "svc", []string{"echo a", "echo b"}, dir, true, false)
}

func TestRunServiceCommandsSequentialEmpty(t *testing.T) {
	RunServiceCommands("test", "svc", nil, "", false, false)
}

func TestRunServiceCommandsSequentialEcho(t *testing.T) {
	dir := t.TempDir()
	RunServiceCommands("test", "svc", []string{"echo a"}, dir, false, false)
}

func TestGetPathToDbService(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = "/proj"
	t.Cleanup(func() { CorgiComposePathDir = prev })
	got, err := GetPathToDbService("db1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "db1") {
		t.Errorf("got %q", got)
	}
}

func TestGetMakefileCommandsInDirectoryNoMakefile(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })

	dir := filepath.Join(CorgiComposePathDir, RootDbServicesFolder, "x")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}

	_, err := GetMakefileCommandsInDirectory("x")
	if err == nil || !strings.Contains(err.Error(), "no makefile") {
		t.Errorf("got %v", err)
	}
}

func TestGetMakefileCommandsInDirectoryDirMissing(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = "/nonexistent/zzz"
	t.Cleanup(func() { CorgiComposePathDir = prev })

	_, err := GetMakefileCommandsInDirectory("x")
	if err == nil {
		t.Error("expected err")
	}
}

func TestExecuteCommandRunMissing(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })

	dir := filepath.Join(CorgiComposePathDir, RootDbServicesFolder, "x")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := ExecuteCommandRun("x", "command-not-exists-zzz"); err == nil {
		t.Error("expected err")
	}
}

func TestExecuteServiceCommandRunMissing(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })

	dir := filepath.Join(CorgiComposePathDir, RootServicesFolder, "x")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := ExecuteServiceCommandRun("x", "command-not-exists-zzz"); err == nil {
		t.Error("expected err")
	}
}

func TestExecuteCommandRunEcho(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })

	dir := filepath.Join(CorgiComposePathDir, RootDbServicesFolder, "x")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := ExecuteCommandRun("x", "echo", "hi"); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestExecuteMakeCommandFails(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })

	dir := filepath.Join(CorgiComposePathDir, RootDbServicesFolder, "x")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}

	_, err := ExecuteMakeCommand("x", "noTarget")
	if err == nil {
		t.Error("expected err")
	}
}

func TestCheckCommandExistsTrueCommand(t *testing.T) {
	if err := CheckCommandExists("true"); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestCheckCommandExistsMissingCommand(t *testing.T) {
	err := CheckCommandExists("totally-nonexistent-command-zzz")
	_ = err
}

func TestGetPathToService(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = "/proj"
	t.Cleanup(func() { CorgiComposePathDir = prev })
	got, err := GetPathToService("api")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "api") {
		t.Errorf("got %q", got)
	}
}

func TestRemoveProcess(t *testing.T) {
	prev := ProcessHandles
	ProcessHandles = nil
	t.Cleanup(func() { ProcessHandles = prev })

	// Use a real process so we get a valid *os.Process
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	proc := cmd.Process
	cmd.Wait()

	addProcess(proc)
	if len(ProcessHandles) != 1 {
		t.Fatalf("expected 1 handle, got %d", len(ProcessHandles))
	}
	removeProcess(proc)
	if len(ProcessHandles) != 0 {
		t.Fatalf("expected 0 handles after remove, got %d", len(ProcessHandles))
	}
}

func TestRemoveProcessNotPresent(t *testing.T) {
	prev := ProcessHandles
	ProcessHandles = nil
	t.Cleanup(func() { ProcessHandles = prev })

	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	proc := cmd.Process
	cmd.Wait()

	removeProcess(proc)
	if len(ProcessHandles) != 0 {
		t.Fatalf("expected 0, got %d", len(ProcessHandles))
	}
}

func TestGetMakefileCommandsInDirectoryWithMakefile(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })

	dir := filepath.Join(CorgiComposePathDir, RootDbServicesFolder, "svc")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	makefile := "help:\n\t@echo stop\nstop:\n\t@echo noop\n"
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(makefile), 0644); err != nil {
		t.Fatal(err)
	}
	cmds, err := GetMakefileCommandsInDirectory("svc")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	_ = cmds
}

func TestExecuteSeedMakeCommandPathNotFound(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })
	_, err := ExecuteSeedMakeCommand("nonexistent_service", "seed")
	if err == nil {
		t.Error("expected error for nonexistent service")
	}
}

func TestRunInteractiveSimple(t *testing.T) {
	cmd := exec.Command("true")
	err := runInteractive(cmd)
	if err != nil {
		t.Errorf("runInteractive(true) unexpected err: %v", err)
	}
}

func TestHandleCommandFailureUnknownCmd(t *testing.T) {
	err := handleCommandFailure(
		fmt.Errorf("executable file not found in $PATH"),
		[]string{"totally-unknown-xyz-cmd"},
		"svc", "totally-unknown-xyz-cmd", "/tmp", nil,
	)
	if err == nil || !strings.Contains(err.Error(), "no install instructions") {
		t.Errorf("unexpected err: %v", err)
	}
}

func TestHandleCommandFailureNonMissing(t *testing.T) {
	original := fmt.Errorf("some other error")
	err := handleCommandFailure(original, []string{"make"}, "svc", "make", "/tmp", nil)
	if err != original {
		t.Errorf("expected original error back, got %v", err)
	}
}

func TestKillAllStoredProcessesWithEntries(t *testing.T) {
	prev := ProcessHandles
	ProcessHandles = nil
	t.Cleanup(func() { ProcessHandles = prev })

	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	addProcess(cmd.Process)
	cmd.Wait()
	KillAllStoredProcesses()
}

func TestExecuteMakeCommandSuccess(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })

	dir := filepath.Join(CorgiComposePathDir, RootDbServicesFolder, "svc")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte("echo:\n\t@echo hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := ExecuteMakeCommand("svc", "echo")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.Contains(string(out), "hello") {
		t.Errorf("got %q", out)
	}
}

func TestExecuteSeedMakeCommandSuccess(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })

	dir := filepath.Join(CorgiComposePathDir, RootDbServicesFolder, "svc")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte("seed:\n\t@echo seeded\n"), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := ExecuteSeedMakeCommand("svc", "seed")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	_ = out
}

func TestRunCommandsParallelNoOp(t *testing.T) {
	runCommandsParallel("test", "svc", []string{"echo hi"}, t.TempDir(), false, nil)
}

func TestHandleCommandFailureKnownCmdInstallFails(t *testing.T) {
	err := handleCommandFailure(
		fmt.Errorf("executable file not found in $PATH"),
		[]string{"yarn"},
		"svc", "yarn install", t.TempDir(), nil,
	)
	if err == nil {
		t.Error("expected error when install fails in CI")
	}
}

func TestCheckCommandExistsWithOutput(t *testing.T) {
	err := CheckCommandExists("echo hello")
	if err != nil {
		t.Errorf("unexpected err: %v", err)
	}
}

func TestCheckCommandExistsNotFound(t *testing.T) {
	err := CheckCommandExists("totally-missing-cmd-xyz --version")
	_ = err
}

func TestRunCleanupCommandsEmpty(t *testing.T) {
	prev := ProcessHandles
	ProcessHandles = nil
	t.Cleanup(func() { ProcessHandles = prev })

	RunCleanupCommands("afterStart", "svc", nil, t.TempDir(), "")
	RunCleanupCommands("afterStart", "svc", []string{}, t.TempDir(), "")

	if len(ProcessHandles) != 0 {
		t.Errorf("ProcessHandles must stay empty, got %d", len(ProcessHandles))
	}
}

func TestRunCleanupCommandsSuccess(t *testing.T) {
	prev := ProcessHandles
	ProcessHandles = nil
	t.Cleanup(func() { ProcessHandles = prev })

	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	RunCleanupCommands("afterStart", "svc", []string{
		fmt.Sprintf("touch %s", marker),
	}, dir, "")

	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("marker not created: %v", err)
	}
	if len(ProcessHandles) != 0 {
		t.Errorf("cleanup commands must not be tracked, got %d handles", len(ProcessHandles))
	}
}

func TestRunCleanupCommandsTimeoutKills(t *testing.T) {
	prev := ProcessHandles
	ProcessHandles = nil
	t.Cleanup(func() { ProcessHandles = prev })

	prevTimeout := AfterStartTimeout
	AfterStartTimeout = 200 * time.Millisecond
	t.Cleanup(func() { AfterStartTimeout = prevTimeout })

	start := time.Now()
	RunCleanupCommands("afterStart", "svc", []string{"sleep 30"}, t.TempDir(), "")
	elapsed := time.Since(start)

	if elapsed > 5*time.Second {
		t.Fatalf("cleanup did not honor timeout, took %s", elapsed)
	}
	if len(ProcessHandles) != 0 {
		t.Errorf("ProcessHandles must stay empty, got %d", len(ProcessHandles))
	}
}

func TestRunCleanupCommandsStopsOnFailure(t *testing.T) {
	prev := ProcessHandles
	ProcessHandles = nil
	t.Cleanup(func() { ProcessHandles = prev })

	dir := t.TempDir()
	marker := filepath.Join(dir, "should-not-exist")
	RunCleanupCommands("afterStart", "svc", []string{
		"false",
		fmt.Sprintf("touch %s", marker),
	}, dir, "")

	if _, err := os.Stat(marker); err == nil {
		t.Errorf("second command must not run after first fails")
	}
}

func TestRunCleanupCommandsSurvivesKillAll(t *testing.T) {
	prev := ProcessHandles
	ProcessHandles = nil
	t.Cleanup(func() { ProcessHandles = prev })

	// Add a tracked dummy so KillAllStoredProcesses has something to kill.
	dummy := exec.Command("sleep", "30")
	SetProcessGroup(dummy)
	if err := dummy.Start(); err != nil {
		t.Fatal(err)
	}
	addProcess(dummy.Process)
	// Reap the SIGKILL'd dummy to avoid leaking a zombie into the test
	// runner. Wait returns an exit error after SIGKILL — ignore it.
	t.Cleanup(func() { _ = dummy.Wait() })

	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")

	done := make(chan struct{})
	go func() {
		defer close(done)
		RunCleanupCommands("afterStart", "svc", []string{
			fmt.Sprintf("sleep 0.3 && touch %s", marker),
		}, dir, "")
	}()

	// Kill tracked procs while cleanup runs — cleanup must not be affected.
	time.Sleep(50 * time.Millisecond)
	KillAllStoredProcesses()

	<-done
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("cleanup was killed by KillAllStoredProcesses: %v", err)
	}
}

// fakeLogWriter implements the optional interfaces runManaged/markServiceLogStatus
// probe (SetStatus/CurrentStatus/Path) without touching the filesystem. Writes
// can come from a detached child process goroutine, so access is mutex-guarded.
type fakeLogWriter struct {
	mu     sync.Mutex
	buf    strings.Builder
	status LogStatus
	path   string
	closed bool
}

func (f *fakeLogWriter) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.buf.Write(p)
}
func (f *fakeLogWriter) written() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.buf.String()
}
func (f *fakeLogWriter) SetStatus(s LogStatus)    { f.status = s }
func (f *fakeLogWriter) CurrentStatus() LogStatus { return f.status }
func (f *fakeLogWriter) Path() string             { return f.path }
func (f *fakeLogWriter) Close() error             { f.closed = true; return nil }

func TestSetAndCloseLogWriters(t *testing.T) {
	t.Cleanup(CloseAllLogWriters)
	CloseAllLogWriters()

	first := &fakeLogWriter{path: "/tmp/a.log"}
	SetLogWriter("svc", first)
	if got := LogFilePath("svc"); got != "/tmp/a.log" {
		t.Fatalf("LogFilePath got %q", got)
	}

	// Replacing closes the previous writer (fd-leak guard).
	second := &fakeLogWriter{path: "/tmp/b.log"}
	SetLogWriter("svc", second)
	if !first.closed {
		t.Error("previous writer should be closed on replace")
	}
	if got := LogFilePath("svc"); got != "/tmp/b.log" {
		t.Fatalf("LogFilePath after replace got %q", got)
	}

	CloseAllLogWriters()
	if !second.closed {
		t.Error("CloseAllLogWriters should close registered writer")
	}
	if got := LogFilePath("svc"); got != "" {
		t.Errorf("expected empty path after CloseAll, got %q", got)
	}
}

func TestLogFilePathNoWriter(t *testing.T) {
	t.Cleanup(CloseAllLogWriters)
	CloseAllLogWriters()
	if got := LogFilePath("absent"); got != "" {
		t.Errorf("expected empty for unregistered service, got %q", got)
	}
}

func TestMarkServiceLogStatus(t *testing.T) {
	t.Cleanup(CloseAllLogWriters)
	w := &fakeLogWriter{}
	SetLogWriter("svc", w)

	markServiceLogStatus("svc", LogStatusOK)
	if w.status != LogStatusOK {
		t.Fatalf("status got %v", w.status)
	}
}

func TestMarkServiceLogStatusIfNotCrashed_DoesNotDowngrade(t *testing.T) {
	t.Cleanup(CloseAllLogWriters)
	w := &fakeLogWriter{status: LogStatusCrashed}
	SetLogWriter("svc", w)

	// A later success must not overwrite an earlier crash.
	markServiceLogStatusIfNotCrashed("svc", LogStatusOK)
	if w.status != LogStatusCrashed {
		t.Fatalf("crashed status was downgraded to %v", w.status)
	}
}

func TestMarkServiceLogStatusIfNotCrashed_SetsWhenClean(t *testing.T) {
	t.Cleanup(CloseAllLogWriters)
	w := &fakeLogWriter{status: LogStatusUnknown}
	SetLogWriter("svc", w)

	markServiceLogStatusIfNotCrashed("svc", LogStatusOK)
	if w.status != LogStatusOK {
		t.Fatalf("expected OK, got %v", w.status)
	}
}

func TestSetOnServiceCrashStoreAndClear(t *testing.T) {
	t.Cleanup(func() { SetOnServiceCrash(nil) })
	called := make(chan string, 1)
	SetOnServiceCrash(func(name string) { called <- name })
	if fn := onServiceCrash.Load(); fn == nil {
		t.Fatal("callback not stored")
	} else {
		(*fn)("svc")
	}
	if got := <-called; got != "svc" {
		t.Errorf("callback got %q", got)
	}
	SetOnServiceCrash(nil)
	if onServiceCrash.Load() != nil {
		t.Error("callback not cleared")
	}
}

func TestRunServiceCommandExitCode_Zero(t *testing.T) {
	prev := ProcessHandles
	ProcessHandles = nil
	t.Cleanup(func() { ProcessHandles = prev })

	var out, errBuf strings.Builder
	code, err := RunServiceCommandExitCode("exit 0", t.TempDir(), false, &out, &errBuf)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code got %d", code)
	}
	if len(ProcessHandles) != 0 {
		t.Errorf("process not deregistered, got %d", len(ProcessHandles))
	}
}

func TestRunServiceCommandExitCode_NonZero(t *testing.T) {
	var out, errBuf strings.Builder
	code, err := RunServiceCommandExitCode("exit 7", t.TempDir(), false, &out, &errBuf)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if code != 7 {
		t.Fatalf("exit code got %d want 7", code)
	}
}

func TestRunServiceCommandExitCode_CapturesOutput(t *testing.T) {
	var out, errBuf strings.Builder
	code, err := RunServiceCommandExitCode("echo hi; echo boom 1>&2", t.TempDir(), false, &out, &errBuf)
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if !strings.Contains(out.String(), "hi") {
		t.Errorf("stdout got %q", out.String())
	}
	if !strings.Contains(errBuf.String(), "boom") {
		t.Errorf("stderr got %q", errBuf.String())
	}
}

func TestRunServiceCommandExitCode_SourcesEnv(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("MYVAR=present\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var out, errBuf strings.Builder
	code, err := RunServiceCommandExitCode("echo $MYVAR", dir, false, &out, &errBuf)
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if strings.TrimSpace(out.String()) != "present" {
		t.Errorf("env not sourced, got %q", out.String())
	}
}

func TestStartDetached(t *testing.T) {
	prev := ProcessHandles
	ProcessHandles = nil
	t.Cleanup(func() { ProcessHandles = prev })

	proc, err := StartDetached("svc", "sleep 0.2", t.TempDir())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if proc == nil {
		t.Fatal("expected a process handle")
	}
	if len(ProcessHandles) == 0 {
		t.Error("detached process should be tracked")
	}
	t.Cleanup(func() {
		_, _ = proc.Wait()
		removeProcess(proc)
	})
}

func TestStartDetachedWritesToLogWriter(t *testing.T) {
	t.Cleanup(CloseAllLogWriters)
	prev := ProcessHandles
	ProcessHandles = nil
	t.Cleanup(func() { ProcessHandles = prev })

	w := &fakeLogWriter{}
	SetLogWriter("svc", w)
	proc, err := StartDetached("svc", "echo detached-out", t.TempDir())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	_, _ = proc.Wait()
	removeProcess(proc)
	if got := w.written(); !strings.Contains(got, "detached-out") {
		t.Errorf("detached output not written to log writer, got %q", got)
	}
}

func TestStopDockerRunnerServicesNonexistent(t *testing.T) {
	prevTimeout := AfterStartTimeout
	AfterStartTimeout = 2 * time.Second
	t.Cleanup(func() { AfterStartTimeout = prevTimeout })

	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })

	// No service dir / no Makefile: make fails but the call must be non-fatal.
	StopDockerRunnerServices([]string{"absent-svc"})
}

func TestStopDockerRunnerServicesEmpty(t *testing.T) {
	StopDockerRunnerServices(nil)
	StopDockerRunnerServices([]string{})
}

func TestRunManagedCrashFiresCallback(t *testing.T) {
	t.Cleanup(CloseAllLogWriters)
	prev := ProcessHandles
	ProcessHandles = nil
	t.Cleanup(func() { ProcessHandles = prev })

	w := &fakeLogWriter{}
	SetLogWriter("crashy", w)

	crashed := make(chan string, 1)
	SetOnServiceCrash(func(name string) { crashed <- name })
	t.Cleanup(func() { SetOnServiceCrash(nil) })

	// Non-zero exit (not a missing executable) → crash path, callback fires.
	err := RunServiceCmd("crashy", "exit 3", t.TempDir(), false)
	if err == nil {
		t.Fatal("expected error from non-zero exit")
	}
	select {
	case name := <-crashed:
		if name != "crashy" {
			t.Errorf("callback got %q", name)
		}
	default:
		t.Fatal("crash callback was not fired")
	}
	if w.status != LogStatusCrashed {
		t.Errorf("log status got %v want Crashed", w.status)
	}
}

func TestRunCleanupCommandsSourcesEnv(t *testing.T) {
	prev := ProcessHandles
	ProcessHandles = nil
	t.Cleanup(func() { ProcessHandles = prev })

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("CLEANUP_VAR=present\n"), 0644); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "out")
	RunCleanupCommands("afterStart", "svc", []string{
		fmt.Sprintf("echo $CLEANUP_VAR > %s", marker),
	}, dir, "")

	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != "present" {
		t.Errorf("env not sourced, got %q", got)
	}
}
