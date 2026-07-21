package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils"
)

func TestE2ESuiteRunsFromItsWorkdir(t *testing.T) {
	root := t.TempDir()
	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = root
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	suiteDir := filepath.Join(root, "e2e")
	if err := os.MkdirAll(suiteDir, 0o755); err != nil {
		t.Fatal(err)
	}

	suite := &utils.E2ESuite{Workdir: "e2e", Run: "pwd > ran.txt"}
	workdir := filepath.Join(utils.CorgiComposePathDir, suite.Workdir)
	if err := utils.RunServiceCmd("e2e", suite.Run, workdir, false, utils.SkipAutoSourceEnv); err != nil {
		t.Fatalf("run: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(suiteDir, "ran.txt"))
	if err != nil {
		t.Fatalf("the suite must run inside its workdir: %v", err)
	}
	if !strings.Contains(string(got), "e2e") {
		t.Errorf("ran in %q, expected the e2e workdir", strings.TrimSpace(string(got)))
	}
}

func TestE2ESuiteFailsWhenCommandFails(t *testing.T) {
	dir := t.TempDir()
	if err := utils.RunServiceCmd("e2e", "exit 3", dir, false, utils.SkipAutoSourceEnv); err == nil {
		t.Error("a non-zero suite must be reported as a failure")
	}
}
