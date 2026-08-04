package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"andriiklymiuk/corgi/utils"
)

func TestCopyEnvFileCopiesExistingContent(t *testing.T) {
	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("PORT=8472\nX=localhost\n"), 0644); err != nil {
		t.Fatal(err)
	}
	err := copyEnvFileWithSubstitutions(utils.Service{ServiceName: "api", AbsolutePath: dir})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(utils.CorgiComposePathDir, utils.RootServicesFolder, "api", ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "PORT=8472\nX=host.docker.internal\n" {
		t.Fatalf("got %q", raw)
	}
}
