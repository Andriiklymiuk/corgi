package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWithEnvSource_WhenEnvExists(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("FOO=bar\n"), 0644); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	got := withEnvSource("npx vite --port $PORT", dir)
	if !strings.HasPrefix(got, "set -a; . ./.env; set +a; ") {
		t.Fatalf("expected prefix, got: %q", got)
	}
	if !strings.HasSuffix(got, "npx vite --port $PORT") {
		t.Fatalf("expected original command preserved, got: %q", got)
	}
}

func TestWithEnvSource_WhenEnvMissing(t *testing.T) {
	dir := t.TempDir()
	cmd := "npm install"
	got := withEnvSource(cmd, dir)
	if got != cmd {
		t.Fatalf("expected unchanged, got: %q", got)
	}
}

func TestWithEnvSource_WhenPathEmpty(t *testing.T) {
	cmd := "orbctl start"
	got := withEnvSource(cmd, "")
	if got != cmd {
		t.Fatalf("expected unchanged when path empty, got: %q", got)
	}
}
