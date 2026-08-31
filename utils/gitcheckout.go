package utils

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

const (
	CheckoutUpdated  = "updated"
	CheckoutUpToDate = "up-to-date"
	CheckoutSkipped  = "skipped"
	CheckoutFailed   = "failed"
)

type RepoCheckout struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Branch   string `json:"branch,omitempty"`
	Status   string `json:"status"`
	Fallback bool   `json:"usedDefaultBranch,omitempty"`
	Message  string `json:"message,omitempty"`
}

func (r RepoCheckout) failed(message string) RepoCheckout {
	r.Status = CheckoutFailed
	r.Message = message
	return r
}

func (r RepoCheckout) skipped(message string) RepoCheckout {
	r.Status = CheckoutSkipped
	r.Message = message
	return r
}

func CheckoutRepo(name, dir, branch string, allowDirty bool) RepoCheckout {
	result := RepoCheckout{Name: name, Path: dir}
	if !isGitRepo(dir) {
		return result.skipped("not a git repository")
	}
	if !allowDirty {
		dirty, err := isTreeDirty(dir)
		if err != nil {
			return result.failed(err.Error())
		}
		if dirty {
			return result.skipped("uncommitted changes; commit or stash them, or pass --allow-dirty")
		}
	}

	target, err := resolveCheckoutBranch(dir, branch)
	if err != nil {
		return result.failed(err.Error())
	}
	result.Branch, result.Fallback = target.name, target.fallback

	before, _ := gitOut(dir, gitRevParse, "HEAD")
	if current, _ := gitOut(dir, gitRevParse, gitAbbrevRef, "HEAD"); current != target.name {
		if err := checkoutKnownBranch(dir, target.name, target.local); err != nil {
			return result.failed(err.Error())
		}
	}
	note, err := pullCurrentBranch(dir)
	if err != nil {
		return result.failed(err.Error())
	}
	after, _ := gitOut(dir, gitRevParse, "HEAD")

	result.Status = CheckoutUpToDate
	if before != after {
		result.Status = CheckoutUpdated
	}
	result.Message = note
	return result
}

type checkoutBranch struct {
	name     string
	local    bool
	fallback bool
}

func resolveCheckoutBranch(dir, branch string) (checkoutBranch, error) {
	if branch != "" {
		if local, remote := branchIsKnown(dir, branch); local || remote {
			return checkoutBranch{name: branch, local: local}, nil
		}
	}
	fallbackTo := DefaultBranchOf(dir)
	if fallbackTo == "" {
		if branch == "" {
			return checkoutBranch{}, fmt.Errorf("could not resolve the default branch")
		}
		return checkoutBranch{}, fmt.Errorf("no %s branch here, and the default branch could not be resolved either", branch)
	}
	local, remote := branchIsKnown(dir, fallbackTo)
	if !local && !remote {
		return checkoutBranch{}, fmt.Errorf("default branch %s is not in this repo", fallbackTo)
	}
	return checkoutBranch{
		name:     fallbackTo,
		local:    local,
		fallback: branch != "" && branch != fallbackTo,
	}, nil
}

func pullCurrentBranch(dir string) (string, error) {
	if _, err := gitOut(dir, gitRevParse, gitAbbrevRef, "--symbolic-full-name", "@{u}"); err != nil {
		return "no upstream, nothing to pull", nil
	}
	if err := gitRunNoPrompt(dir, "pull", "--ff-only"); err != nil {
		return "", fmt.Errorf("git pull --ff-only: %v", err)
	}
	return "", nil
}

func DefaultBranchOf(dir string) string {
	if branch := originHeadBranch(dir); branch != "" {
		return branch
	}
	if _, err := gitOutNoPrompt(dir, "remote", "set-head", "origin", "--auto"); err == nil {
		if branch := originHeadBranch(dir); branch != "" {
			return branch
		}
	}
	for _, candidate := range []string{"main", "master"} {
		if _, err := gitOut(dir, gitRevParse, "--verify", "--quiet", "refs/heads/"+candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func originHeadBranch(dir string) string {
	out, err := gitOut(dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(out, "origin/")
}

func gitOutNoPrompt(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), remoteProbeTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	c.Env = noPromptEnv()
	out, err := c.Output()
	return strings.TrimSpace(string(out)), err
}
