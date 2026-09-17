package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The images land on pr-assets/<key> at origin, under docs/pr-assets/<key>/,
// and the checkout the user works in is left on its own branch, clean.
func TestPushAssetsCommitsToTheAssetsBranchAndLeavesTheCheckoutAlone(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	origin, clone := originRepo(t, root, "main")
	git(t, clone, "checkout", "-b", "feature/ABC-1/thing")
	// A repo that gitignores docs/ still gets its screenshots on the assets branch.
	if err := os.WriteFile(filepath.Join(clone, ".git", "info", "exclude"), []byte("docs\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	img := filepath.Join(root, "1-card.png")
	if err := os.WriteFile(img, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := PushAssets(clone, "ABC-1", "", []string{img})
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if res.Branch != "pr-assets/ABC-1" || len(res.Files) != 1 {
		t.Fatalf("result: %+v", res)
	}
	if res.Files[0].Path != "docs/pr-assets/ABC-1/1-card.png" || res.Files[0].URL != "" {
		t.Fatalf("file: %+v", res.Files[0])
	}
	if !strings.Contains(res.Markdown, "![1-card](docs/pr-assets/ABC-1/1-card.png)") {
		t.Fatalf("markdown: %q", res.Markdown)
	}
	if out, _ := gitOut(origin, "ls-tree", "-r", "--name-only", "pr-assets/ABC-1"); !strings.Contains(out, "docs/pr-assets/ABC-1/1-card.png") {
		t.Fatalf("origin branch tree: %q", out)
	}
	if head, _ := gitOut(origin, gitRevParse, "pr-assets/ABC-1"); head != res.Head {
		t.Fatalf("origin head %s, result head %s", head, res.Head)
	}
	if cur, _ := gitOut(clone, gitRevParse, gitAbbrevRef, "HEAD"); cur != "feature/ABC-1/thing" {
		t.Fatalf("checkout moved to %s", cur)
	}
	if dirty, _ := isTreeDirty(clone); dirty {
		t.Fatal("checkout is dirty after the push")
	}
	if _, err := os.Stat(filepath.Join(os.TempDir(), "corgi-assets", "ABC-1")); !os.IsNotExist(err) {
		t.Fatal("assets worktree was left behind")
	}

	// A second push appends to the same branch instead of starting over.
	img2 := filepath.Join(root, "2-sheet.png")
	if err := os.WriteFile(img2, []byte("png2"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PushAssets(clone, "ABC-1", "", []string{img2}); err != nil {
		t.Fatalf("second push: %v", err)
	}
	out, _ := gitOut(origin, "ls-tree", "-r", "--name-only", "pr-assets/ABC-1")
	if !strings.Contains(out, "1-card.png") || !strings.Contains(out, "2-sheet.png") {
		t.Fatalf("second push tree: %q", out)
	}
}

func TestPushAssetsRefusesBadInput(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	_, clone := originRepo(t, root, "main")
	if _, err := PushAssets(clone, "", "", []string{"x.png"}); err == nil {
		t.Fatal("empty key accepted")
	}
	if _, err := PushAssets(clone, "a/b", "", []string{"x.png"}); err == nil {
		t.Fatal("key with a slash accepted")
	}
	if _, err := PushAssets(clone, "ABC-1", "", nil); err == nil {
		t.Fatal("no files accepted")
	}
	if _, err := PushAssets(clone, "ABC-1", "", []string{filepath.Join(root, "missing.png")}); err == nil {
		t.Fatal("missing file accepted")
	}
	if _, err := PushAssets(clone, "ABC-1", "", []string{root}); err == nil {
		t.Fatal("directory accepted")
	}
	if _, err := PushAssets(root, "ABC-1", "", []string{root}); err == nil {
		t.Fatal("non-repo accepted")
	}
}

// The link a private repo renders: GitHub through the blob viewer with
// ?raw=true, GitLab through /-/raw/; ssh and https remotes read the same.
func TestAssetURLPerForge(t *testing.T) {
	cases := map[string]string{
		"git@github.com:acme/api.git":           "https://github.com/acme/api/blob/pr-assets/ABC-1/docs/pr-assets/ABC-1/1.png?raw=true",
		"https://github.com/acme/api.git":       "https://github.com/acme/api/blob/pr-assets/ABC-1/docs/pr-assets/ABC-1/1.png?raw=true",
		"https://github.com/acme/api":           "https://github.com/acme/api/blob/pr-assets/ABC-1/docs/pr-assets/ABC-1/1.png?raw=true",
		"git@gitlab.com:group/sub/proj.git":     "https://gitlab.com/group/sub/proj/-/raw/pr-assets/ABC-1/docs/pr-assets/ABC-1/1.png",
		"https://gitlab.example.net/g/p.git":    "https://gitlab.example.net/g/p/-/raw/pr-assets/ABC-1/docs/pr-assets/ABC-1/1.png",
		"ssh://git@github.com/acme/api.git":     "https://github.com/acme/api/blob/pr-assets/ABC-1/docs/pr-assets/ABC-1/1.png?raw=true",
		"/tmp/some/local/origin":                "",
		"https://token@github.com/acme/api.git": "https://github.com/acme/api/blob/pr-assets/ABC-1/docs/pr-assets/ABC-1/1.png?raw=true",
	}
	for remote, want := range cases {
		if got := assetURL(remote, "pr-assets/ABC-1", "docs/pr-assets/ABC-1/1.png"); got != want {
			t.Errorf("%s: got %q want %q", remote, got, want)
		}
	}
}

func TestAssetsMarkdownIsOneRow(t *testing.T) {
	md := assetsMarkdown([]AssetFile{{Path: "d/a.png", Markdown: "![a](u1)"}, {Path: "d/b.png", Markdown: "![b](u2)"}})
	want := "| a | b |\n| --- | --- |\n| ![a](u1) | ![b](u2) |\n"
	if md != want {
		t.Fatalf("got %q want %q", md, want)
	}
	if assetsMarkdown(nil) != "" {
		t.Fatal("empty list should give no markdown")
	}
}
