package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveEnvSourceFile_ExplicitPathWins(t *testing.T) {
	dir := t.TempDir()
	prev := CorgiComposePathDir
	CorgiComposePathDir = dir
	t.Cleanup(func() { CorgiComposePathDir = prev })

	writeFile(t, filepath.Join(dir, "api.env"), "A=1")
	svc := Service{AbsolutePath: dir + "/", CopyEnvFromFilePath: "api.env"}

	got := resolveEnvSourceFile(CorgiComposePathDir, svc, "")
	want := filepath.Join(dir, "api.env")
	if got != want {
		t.Fatalf("want explicit path %q, got %q", want, got)
	}
}

func TestResolveEnvSourceFile_FallsBackToEnvExample(t *testing.T) {
	dir := t.TempDir()
	prev := CorgiComposePathDir
	CorgiComposePathDir = dir
	t.Cleanup(func() { CorgiComposePathDir = prev })

	// explicit source missing; .env-example present in the service dir
	writeFile(t, filepath.Join(dir, ".env-example"), "A=1")
	svc := Service{AbsolutePath: dir + "/", CopyEnvFromFilePath: "missing.env"}

	got := resolveEnvSourceFile(CorgiComposePathDir, svc, "")
	want := filepath.Join(dir, ".env-example")
	if got != want {
		t.Fatalf("want .env-example fallback %q, got %q", want, got)
	}
}

func TestResolveEnvSourceFile_FallsBackToDotEnvExample(t *testing.T) {
	dir := t.TempDir()
	prev := CorgiComposePathDir
	CorgiComposePathDir = dir
	t.Cleanup(func() { CorgiComposePathDir = prev })

	// only .env.example variant present, no explicit source
	writeFile(t, filepath.Join(dir, ".env.example"), "A=1")
	svc := Service{AbsolutePath: dir + "/"}

	got := resolveEnvSourceFile(CorgiComposePathDir, svc, "")
	want := filepath.Join(dir, ".env.example")
	if got != want {
		t.Fatalf("want .env.example fallback %q, got %q", want, got)
	}
}

func TestBuildServiceEnvBody_UsesEnvExampleFallback(t *testing.T) {
	dir := t.TempDir()
	prev := CorgiComposePathDir
	CorgiComposePathDir = dir
	t.Cleanup(func() { CorgiComposePathDir = prev })

	writeFile(t, filepath.Join(dir, ".env-example"), "FOO=bar")
	svc := Service{ServiceName: "api", AbsolutePath: dir + "/", CopyEnvFromFilePath: "missing.env"}

	body, err := buildServiceEnvBody(svc, &CorgiCompose{}, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "FOO=bar") {
		t.Fatalf("want env body from .env-example fallback, got %q", body)
	}
}

func TestResolveEnvSourceFile_NoneExist(t *testing.T) {
	dir := t.TempDir()
	prev := CorgiComposePathDir
	CorgiComposePathDir = dir
	t.Cleanup(func() { CorgiComposePathDir = prev })

	svc := Service{AbsolutePath: dir + "/", CopyEnvFromFilePath: "missing.env"}

	if got := resolveEnvSourceFile(CorgiComposePathDir, svc, ""); got != "" {
		t.Fatalf("want empty when nothing exists, got %q", got)
	}
}
