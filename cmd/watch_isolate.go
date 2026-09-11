package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"andriiklymiuk/corgi/utils"
)

// The daemon asks for worktrees by workspace directory and branch; loading a
// compose file goes through globals, so one at a time.
var isolateMu sync.Mutex

// isolateFixWorktrees gives every repository of the stack in dir a worktree
// on branch and returns the directories, for an unattended run to work in.
func isolateFixWorktrees(dir, branch string) ([]string, error) {
	isolateMu.Lock()
	defer isolateMu.Unlock()
	composePath := ""
	for _, name := range []string{utils.CorgiComposeDefaultName, "corgi-compose.yaml"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			composePath = filepath.Join(dir, name)
			break
		}
	}
	if composePath == "" {
		return nil, fmt.Errorf("no corgi-compose.yml in %s", dir)
	}
	corgi, _, err := loadComposeForAgent(composePath)
	if err != nil {
		return nil, err
	}
	set, err := utils.MaterializeBranchAcrossRepos(corgi, dir, branch, nil)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var dirs []string
	for _, w := range set.Worktrees {
		if w.Skipped != "" || w.Dir == "" || seen[w.Dir] {
			continue
		}
		seen[w.Dir] = true
		dirs = append(dirs, w.Dir)
	}
	if len(dirs) == 0 {
		return nil, fmt.Errorf("no repository in %s could take a worktree", dir)
	}
	return dirs, nil
}

// releaseFixWorktrees removes an isolated run's worktrees, keeping any with
// uncommitted work and saying so.
func releaseFixWorktrees(dir, branch string) (removed, kept []string, err error) {
	if strings.TrimSpace(branch) == "" {
		return nil, nil, nil
	}
	return utils.ReleaseBranchWorktreesReport(dir, branch)
}
