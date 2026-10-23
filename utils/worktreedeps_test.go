package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAWorktreeGetsTheMainCheckoutsPackagesWhenTheLockfileMatches(t *testing.T) {
	corgi, dir := stack(t, "api", "web")
	api, web := corgi.Services[0].AbsolutePath, corgi.Services[1].AbsolutePath
	for _, repo := range []string{api, web} {
		writeRepoFile(t, filepath.Join(repo, ".gitignore"), "node_modules/\n")
		writeRepoFile(t, filepath.Join(repo, "package-lock.json"), `{"lockfileVersion":3}`)
		commitAll(t, repo, "lock")
		writeRepoFile(t, filepath.Join(repo, "node_modules", "left-pad", "index.js"), "module.exports = 1\n")
	}
	set, err := MaterializeBranchAcrossRepos(corgi, dir, "corgi/abc-7", nil)
	if err != nil {
		t.Fatal(err)
	}
	trees := map[string]string{}
	for _, w := range set.Worktrees {
		trees[w.Service] = w.Dir
	}
	writeRepoFile(t, filepath.Join(trees["web"], "package-lock.json"), `{"lockfileVersion":3,"changed":true}`)

	seeded := SeedWorktreeDeps(corgi, set)
	if len(seeded) != 1 || seeded[0] != filepath.Join(trees["api"], "node_modules") {
		t.Fatalf("only the worktree whose lockfile matches gets the packages: %v", seeded)
	}
	if _, err := os.Stat(filepath.Join(trees["api"], "node_modules", "left-pad", "index.js")); err != nil {
		t.Fatalf("the packages are there: %v", err)
	}
	if _, err := os.Stat(filepath.Join(trees["web"], "node_modules")); !os.IsNotExist(err) {
		t.Fatalf("a different lockfile gets no copy: %v", err)
	}
	if again := SeedWorktreeDeps(corgi, set); len(again) != 0 {
		t.Fatalf("packages already there are left alone: %v", again)
	}
	if info, err := os.Lstat(filepath.Join(trees["api"], "node_modules")); err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("a copy, not a link into the main checkout: %v %v", info, err)
	}
}
