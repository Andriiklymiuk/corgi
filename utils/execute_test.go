package utils

import (
	"os"
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

func TestResolveEnvFile_NoPathDisables(t *testing.T) {
	if got := resolveEnvFile("", nil); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
	if got := resolveEnvFile("", []string{".env.local"}); got != "" {
		t.Fatalf("expected empty when path empty, got %q", got)
	}
}
