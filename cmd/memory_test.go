package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"andriiklymiuk/corgi/utils"
)

func runMemory(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs(append([]string{"memory"}, args...))
	err := rootCmd.Execute()
	return out.String(), err
}

func TestMemoryListAbsentStoreIsEmptyJSON(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	_ = os.Chdir(dir)
	utils.JSONOutput = true
	t.Cleanup(func() { utils.JSONOutput = false })

	// capture stdout (PrintJSON writes os.Stdout)
	out := captureStdout(t, func() { _, _ = runMemory(t, "list") })
	var facts []utils.Fact
	if err := json.Unmarshal([]byte(out), &facts); err != nil {
		t.Fatalf("absent store must emit valid JSON array, got %q (%v)", out, err)
	}
	if len(facts) != 0 {
		t.Fatalf("absent store must list empty, got %d", len(facts))
	}
}

func TestMemoryAddThenIndexThenLint(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	_ = os.Chdir(dir)

	if _, err := runMemory(t, "add", "--type", "decision", "--name", "pg-choice", "--desc", "Postgres for JSONB"); err != nil {
		t.Fatalf("add failed: %v", err)
	}
	if _, err := runMemory(t, "index"); err != nil {
		t.Fatalf("index failed: %v", err)
	}
	idx, _ := os.ReadFile(filepath.Join(dir, utils.MemoryDirName, "index.md"))
	if !bytes.Contains(idx, []byte("pg-choice")) {
		t.Fatalf("index.md missing the fact:\n%s", idx)
	}
	if _, err := runMemory(t, "lint"); err != nil {
		t.Fatalf("clean store must lint clean, got %v", err)
	}
}
