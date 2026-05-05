package utils

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
