package daemon

import (
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/watch"
)

func (d *Daemon) pruneWorktrees(now time.Time) {
	if now.Sub(d.lastPrune) < time.Hour {
		return
	}
	d.lastPrune = now
	if d.pruned == nil {
		d.pruned = map[string]bool{}
	}
	for _, spec := range d.Watches {
		if spec.PruneAfter <= 0 || spec.AgentDir == "" {
			continue
		}
		for _, r := range watch.PrunableFixes(watch.LoadFixLog(spec.AgentDir).RecentFixes(spec.Workspace, 500), spec.PruneAfter, now) {
			d.pruneBranch(spec, r.Branch)
		}
	}
}

func (d *Daemon) pruneBranch(spec WatchSpec, branch string) {
	key := spec.Workspace + "/" + branch
	if d.pruned[key] {
		return
	}
	removed, kept, err := utils.ReleaseBranchWorktreesReport(spec.Dir, branch)
	if err != nil {
		utils.Infof("agent: prune %s: %v\n", branch, err)
		return
	}
	if len(kept) == 0 {
		d.pruned[key] = true
	}
	if len(removed) > 0 {
		utils.Infof("agent: pruned worktrees of %s in %s: %s\n", branch, spec.Workspace, strings.Join(removed, ", "))
	}
}
