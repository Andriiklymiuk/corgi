package cmd

import (
	"andriiklymiuk/corgi/utils"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddFileToGitignoreNew(t *testing.T) {
	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	if err := addFileToGitignore("foo/*"); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(utils.CorgiComposePathDir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "foo/*") {
		t.Errorf("missing entry: %s", body)
	}
}

func TestAddFileToGitignoreIdempotent(t *testing.T) {
	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	if err := addFileToGitignore(".env"); err != nil {
		t.Fatal(err)
	}
	if err := addFileToGitignore(".env"); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(utils.CorgiComposePathDir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(body), ".env") != 1 {
		t.Errorf("duplicate entry, got %s", body)
	}
}

func TestCloneMissingServiceDirNoCloneFrom(t *testing.T) {
	if cloneMissingServiceDir(utils.Service{ServiceName: "x", AbsolutePath: t.TempDir()}) {
		t.Error("expected false")
	}
}

func TestCreateDatabaseServicesAddsFiles(t *testing.T) {
	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	CreateDatabaseServices([]utils.DatabaseService{
		{ServiceName: "db1", Driver: "postgres", Host: "localhost", User: "u", Password: "p", DatabaseName: "d", Port: 5432},
	})
	dest := filepath.Join(utils.CorgiComposePathDir, utils.RootDbServicesFolder, "db1", "docker-compose.yml")
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestWriteServiceFiles(t *testing.T) {
	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM alpine\nEXPOSE 80\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := writeServiceFiles(utils.Service{
		ServiceName:  "api",
		Runner:       utils.Runner{Name: "docker"},
		Port:         80,
		AbsolutePath: dir,
	})
	if !got {
		t.Error("expected true")
	}
}

func TestCloneServicesEmpty(t *testing.T) {
	CloneServices(nil)
}

func TestCreateServicesIterates(t *testing.T) {
	CreateServices([]utils.Service{
		{ServiceName: "x", Runner: utils.Runner{Name: ""}},
	})
}

func TestRunBranchCheckoutPathMissing(t *testing.T) {
	runBranchCheckout(utils.Service{
		ServiceName:  "x",
		AbsolutePath: "/nonexistent/zzz",
		Branch:       "main",
	})
}

func TestHandleExistingServiceDirNoCloneFrom(t *testing.T) {
	handleExistingServiceDir(utils.Service{ServiceName: "x"})
}

func TestHandleExistingServiceDirNoBranch(t *testing.T) {
	handleExistingServiceDir(utils.Service{ServiceName: "x", CloneFrom: "g"})
}
