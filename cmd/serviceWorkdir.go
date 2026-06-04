package cmd

import "github.com/spf13/pflag"

// registerServiceWorkdirFlags adds the per-service working-dir overrides shared by
// run/exec/test: an explicit dir, a reused worktree off a branch, or an in-place
// checkout. All repointing the same AbsolutePath.
func registerServiceWorkdirFlags(fs *pflag.FlagSet) {
	fs.StringArray(
		"service-dir",
		nil,
		`Override a service's working dir: --service-dir name=/path (repeatable),
e.g. a git worktree. The dir must exist.`,
	)
	fs.StringArray(
		"service-branch",
		nil,
		`Run a service on a git branch via a reused worktree under
corgi_services/.worktrees: --service-branch name=branch (repeatable).
Non-destructive — the main checkout is untouched. Clean up with: corgi worktree prune.`,
	)
	fs.StringArray(
		"service-checkout",
		nil,
		`Run a service on a git branch by checking it out in place:
--service-checkout name=branch (repeatable). Refuses on a dirty tree; leaves the
repo on that branch afterwards.`,
	)
}
