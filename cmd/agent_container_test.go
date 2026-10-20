package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContainerFilesKeepTheTokenAcrossRewrites(t *testing.T) {
	dir := t.TempDir()
	if err := writeContainerFiles(dir, "git@github.com:acme/stack.git", true); err != nil {
		t.Fatal(err)
	}
	out := containerDir(dir)
	for _, name := range []string{"Dockerfile", "entrypoint.sh", "compose.yml", ".env"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	compose, _ := os.ReadFile(filepath.Join(out, "compose.yml"))
	if !strings.Contains(string(compose), "/var/run/docker.sock") {
		t.Fatal("--docker mounts the socket")
	}
	env, _ := os.ReadFile(filepath.Join(out, ".env"))
	if !strings.Contains(string(env), "CORGI_CLONE=git@github.com:acme/stack.git") {
		t.Fatalf("the clone travels in the env: %s", env)
	}
	_ = os.WriteFile(filepath.Join(out, ".env"), append(env, []byte("CLAUDE_CODE_OAUTH_TOKEN=sk-x\n")...), 0o600)
	if err := writeContainerFiles(dir, "", false); err != nil {
		t.Fatal(err)
	}
	env, _ = os.ReadFile(filepath.Join(out, ".env"))
	compose, _ = os.ReadFile(filepath.Join(out, "compose.yml"))
	if !strings.Contains(string(env), "CLAUDE_CODE_OAUTH_TOKEN=sk-x") || strings.Contains(string(env), "CORGI_CLONE") || strings.Contains(string(compose), "docker.sock") {
		t.Fatalf("a rewrite keeps the token and drops the rest: %s", env)
	}
	dockerfile, _ := os.ReadFile(filepath.Join(out, "Dockerfile"))
	if !strings.Contains(string(dockerfile), "ARG CORGI_VERSION="+APP_VERSION) {
		t.Fatal("the image pins this corgi version")
	}
	if info, _ := os.Stat(filepath.Join(out, "entrypoint.sh")); info.Mode().Perm() != 0o700 {
		t.Fatal("the entrypoint runs")
	}
}
