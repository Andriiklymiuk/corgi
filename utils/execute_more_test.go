package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
