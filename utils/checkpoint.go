package utils

import (
	"fmt"
	"path/filepath"
	"strings"
)

const checkpointRefPrefix = "refs/corgi/checkpoints/"

func CaptureWorkTree(dir, checkpoint, label string) (string, error) {
	if !isGitRepo(dir) {
		return "", fmt.Errorf("%s is not a git repository", dir)
	}
	sha, err := gitOut(dir, "stash", "create")
	if err != nil {
		return "", fmt.Errorf("git stash create: %v", err)
	}
	sha = strings.TrimSpace(sha)
	if sha == "" {
		return "", nil
	}
	if _, err := gitOut(dir, "update-ref", checkpointRef(checkpoint, label), sha); err != nil {
		return "", fmt.Errorf("anchor the captured work: %v", err)
	}
	return sha, nil
}

func RestoreWorkTree(dir, branch, head, stashSha string) error {
	if !isGitRepo(dir) {
		return fmt.Errorf("%s is not a git repository", dir)
	}
	target := head
	if branch != "" {
		if _, err := gitOut(dir, gitRevParse, "--verify", "--quiet", "refs/heads/"+branch); err == nil {
			target = branch
		}
	}
	if _, err := gitOut(dir, "checkout", target); err != nil {
		return fmt.Errorf("checkout %s: %v", target, err)
	}
	if _, err := gitOut(dir, "reset", "--hard", head); err != nil {
		return fmt.Errorf("reset to %s: %v", head, err)
	}
	if stashSha == "" {
		return nil
	}
	if _, err := gitOut(dir, "stash", "apply", "--index", stashSha); err != nil {
		return fmt.Errorf("re-apply the captured work: %v", err)
	}
	return nil
}

func DropCheckpointRefs(dir, checkpoint string) {
	out, err := gitOut(dir, "for-each-ref", "--format=%(refname)", checkpointRefPrefix+checkpoint)
	if err != nil {
		return
	}
	for _, ref := range strings.Fields(out) {
		_, _ = gitOut(dir, "update-ref", "-d", ref)
	}
}

func checkpointRef(checkpoint, label string) string {
	return checkpointRefPrefix + checkpoint + "/" + branchSlug(label)
}

func CheckpointsDir(composeDir string) string {
	return filepath.Join(composeDir, "corgi_services", ".checkpoints")
}
