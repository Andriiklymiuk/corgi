package utils

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var seedableDeps = map[string]bool{"node_modules": true, "vendor": true, "vendor/bundle": true, "deps": true}

const seedTimeout = 5 * time.Minute

func SeedWorktreeDeps(corgi *CorgiCompose, set *WorktreeSet) []string {
	if corgi == nil || set == nil {
		return nil
	}
	trees := map[string]string{}
	for _, w := range set.Worktrees {
		if w.Skipped == "" && w.Dir != "" && w.Repo != "" {
			trees[w.Repo] = w.Dir
		}
	}
	var seeded []string
	done := map[string]bool{}
	for i := range corgi.Services {
		svc := &corgi.Services[i]
		root, ok := repoRoot(svc.AbsolutePath)
		if !ok || trees[root] == "" {
			continue
		}
		from, ok := realPath(svc.AbsolutePath)
		if !ok {
			continue
		}
		rel, err := filepath.Rel(root, from)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		to := filepath.Join(trees[root], rel)
		if done[to] {
			continue
		}
		done[to] = true
		seeded = append(seeded, seedDepsDir(from, to)...)
	}
	return seeded
}

func seedDepsDir(from, to string) []string {
	var seeded []string
	for _, eco := range ecosystems {
		lockA, errA := os.ReadFile(filepath.Join(from, eco.lockfile))
		lockB, errB := os.ReadFile(filepath.Join(to, eco.lockfile))
		if errA != nil || errB != nil || !bytes.Equal(lockA, lockB) {
			continue
		}
		for _, dep := range eco.serviceDirs {
			if !seedableDeps[dep] {
				continue
			}
			src, dst := filepath.Join(from, dep), filepath.Join(to, dep)
			if info, err := os.Stat(src); err != nil || !info.IsDir() {
				continue
			}
			if _, err := os.Lstat(dst); err == nil {
				continue
			}
			if err := cloneDir(src, dst); err != nil {
				_ = os.RemoveAll(dst)
				continue
			}
			seeded = append(seeded, dst)
		}
	}
	return seeded
}

func cloneDir(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), seedTimeout)
	defer cancel()
	args := []string{"-R", src, dst}
	switch runtime.GOOS {
	case "darwin":
		args = []string{"-c", "-R", src, dst}
	case "linux":
		args = []string{"--reflink=auto", "-R", src, dst}
	}
	return exec.CommandContext(ctx, "cp", args...).Run()
}
