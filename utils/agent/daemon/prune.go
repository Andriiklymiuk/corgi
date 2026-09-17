package daemon

import (
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/watch"
)

// pruneWorktrees releases the worktrees of isolated runs older than the
// workspace's pruneAfter. Dirty ones stay, and the branch always does, so a
// later run on the same ticket checks it out again.
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
			key := spec.Workspace + "/" + r.Branch
			if d.pruned[key] {
				continue
			}
			removed, kept, err := utils.ReleaseBranchWorktreesReport(spec.Dir, r.Branch)
			if err != nil {
				utils.Infof("agent: prune %s: %v\n", r.Branch, err)
				continue
			}
			if len(kept) == 0 {
				d.pruned[key] = true
			}
			if len(removed) > 0 {
				utils.Infof("agent: pruned worktrees of %s in %s: %s\n", r.Branch, spec.Workspace, strings.Join(removed, ", "))
			}
		}
	}
}
