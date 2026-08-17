package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func artifactsCmd(dest string) *cobra.Command {
	c := &cobra.Command{}
	c.Flags().String("artifacts-dir", dest, "")
	return c
}

func TestCopyTreeCopiesNestedFiles(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "out")
	writeFile(t, filepath.Join(src, "a.png"), "a")
	writeFile(t, filepath.Join(src, "nested", "b.png"), "b")

	if err := copyTree(src, dst); err != nil {
		t.Fatalf("copyTree: %v", err)
	}

	for _, rel := range []string{"a.png", filepath.Join("nested", "b.png")} {
		if _, err := os.Stat(filepath.Join(dst, rel)); err != nil {
			t.Errorf("expected %s to be copied: %v", rel, err)
		}
	}
}

// The whole point of the field: a declared directory is collected, so a red
// suite leaves its screenshots behind.
func TestCollectE2EArtifactsCopiesDeclaredDir(t *testing.T) {
	workdir := t.TempDir()
	dest := filepath.Join(t.TempDir(), "collected")
	writeFile(t, filepath.Join(workdir, "artifacts", "shot.png"), "png")

	collectE2EArtifacts(
		artifactsCmd(dest),
		&utils.E2ESuite{Artifacts: []string{"artifacts"}},
		workdir,
	)

	if _, err := os.Stat(filepath.Join(dest, "artifacts", "shot.png")); err != nil {
		t.Fatalf("declared artifacts dir was not collected: %v", err)
	}
}

// Paths are relative to workdir. A path that does not resolve must not abort the
// run or invent an empty directory — it warns and moves on.
func TestCollectE2EArtifactsSkipsMissingPath(t *testing.T) {
	workdir := t.TempDir()
	dest := filepath.Join(t.TempDir(), "collected")

	collectE2EArtifacts(
		artifactsCmd(dest),
		&utils.E2ESuite{Artifacts: []string{"flows/artifacts"}},
		workdir,
	)

	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("expected no destination dir for a missing artifacts path, got err=%v", err)
	}
}

func TestCollectE2EArtifactsNoopWithoutDeclaration(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "collected")

	collectE2EArtifacts(artifactsCmd(dest), &utils.E2ESuite{}, t.TempDir())

	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("expected no destination dir when nothing is declared, got err=%v", err)
	}
}
