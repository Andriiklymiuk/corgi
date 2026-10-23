package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils"
)

var isolateMu sync.Mutex

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
	for _, seeded := range utils.SeedWorktreeDeps(corgi, set) {
		utils.Infof("agent: %s copied from the main checkout (same lockfile)\n", seeded)
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

func composeIn(dir string) string {
	for _, name := range []string{utils.CorgiComposeDefaultName, "corgi-compose.yaml"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return filepath.Join(dir, name)
		}
	}
	return ""
}

func runLease(branch string) string {
	name := "run-" + strings.TrimPrefix(branch, "corgi/")
	name = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.', r == '_':
			return r
		}
		return '-'
	}, name)
	if len(name) > 40 {
		name = name[:40]
	}
	return strings.TrimRight(name, "-._")
}

func runStackNote(dir, branch, mode string) string {
	if mode == "off" {
		return ""
	}
	path := composeIn(dir)
	if path == "" {
		return ""
	}
	corgi, _, err := loadComposeForAgent(path)
	if err != nil || len(corgi.DatabaseServices) == 0 {
		return ""
	}
	var dbs []string
	for _, db := range corgi.DatabaseServices {
		name := db.ServiceName
		if db.Port != 0 {
			name += " on :" + strconv.Itoa(db.Port)
		}
		dbs = append(dbs, name)
	}
	if mode == "shared" {
		return "\n\nThe databases this run may use are the workspace's own (" + strings.Join(dbs, ", ") + "), shared with the person and every other run. " +
			"Read and query them as much as a check needs; never run a migration, a seed, a restore, a dump load or any write against them. " +
			"A check that needs one goes under `## Left for you` with the exact command.\n"
	}
	lease := runLease(branch)
	return "\n\nThis run has a stack of its own, so a check that needs a database is never skipped for want of one. From `" + dir + "`:\n" +
		"- `corgi --isolate " + lease + " db -u --wait` starts this workspace's databases (" + strings.Join(dbs, ", ") + ") empty, on ports and database names nobody else uses.\n" +
		"- `corgi --isolate " + lease + " exec <service> --feature " + branch + " -- <command>` runs a migration, a schema load, a SQL check or a spec in your worktree against them, with that service's env.\n" +
		"- `corgi --isolate " + lease + " test --feature " + branch + " --changed` runs the tests of what you changed.\n" +
		"They start empty: build what a check needs with the schema and the specs' own fixtures. Never restore, copy or load a dump of another database into them - a production-sized copy takes hours. " +
		"A check that needs real data goes under `## Left for you`. corgi stops these databases and frees the lease when the run ends. Your report names the checks you ran this way."
}

func releaseRunStack(dir, branch string) {
	lease := runLease(branch)
	if _, err := os.Stat(filepath.Join(utils.LeasesDir(dir), lease+".json")); err != nil {
		return
	}
	self, err := os.Executable()
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	for _, args := range [][]string{{"--isolate", lease, "db", "--downAll"}, {"leases", "release", lease}} {
		c := exec.CommandContext(ctx, self, args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			utils.Infof("agent: run stack %s: corgi %s: %v %s\n", lease, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
			return
		}
	}
	utils.Infof("agent: run stack %s stopped and released\n", lease)
}

func releaseFixWorktrees(dir, branch string) (removed, kept []string, err error) {
	if strings.TrimSpace(branch) == "" {
		return nil, nil, nil
	}
	return utils.ReleaseBranchWorktreesReport(dir, branch)
}
