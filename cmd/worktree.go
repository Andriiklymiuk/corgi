package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

var worktreeCmd = &cobra.Command{
	Use:     "worktree",
	Aliases: []string{"wt"},
	Short:   "Manage worktrees corgi created for --service-branch and isolated sessions",
}

func worktreeBaseHere(cmd *cobra.Command) (string, error) {
	if _, err := utils.GetCorgiServices(cmd); err == nil {
		return filepath.Join(utils.CorgiServicesDir(), ".worktrees"), nil
	}
	cwd, _ := os.Getwd()
	if root, ok := utils.RepoRootOf(cwd); ok && root != "" {
		return utils.AgentWorktreeBase(root), nil
	}
	return "", fmt.Errorf("no corgi-compose.yml here and not inside a git repository")
}

var worktreeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List corgi-created service worktrees",
	Run: func(cmd *cobra.Command, _ []string) {
		base, err := worktreeBaseHere(cmd)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			exitProcess(1)
		}
		entries, err := os.ReadDir(base)
		if err != nil || len(entries) == 0 {
			fmt.Println("no corgi worktrees")
			return
		}
		for _, e := range entries {
			fmt.Println(filepath.Join(base, e.Name()))
		}
	},
}

var worktreeAddCmd = &cobra.Command{
	Use:   "add <service>...",
	Short: "Make a worktree for each service inside the workspace, so its CLAUDE.md still loads",
	Long: `Makes the worktree under .corgi/corgi_services/.worktrees/, inside the workspace,
not in /tmp: an agent reading files there still gets the repo's CLAUDE.md, and
corgi worktree prune cleans it up. Prints one directory per service on stdout.

  corgi worktree add api web --branch feature/ABC-1/limits --base main
  W=$(corgi worktree add api --branch feature/ABC-1/limits --base main)
  corgi worktree add api --detach FETCH_HEAD     # read-only, for a review

An existing branch is reused as it is; --base only applies to a new one.`,
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		branch, _ := cmd.Flags().GetString("branch")
		baseBranch, _ := cmd.Flags().GetString("base")
		detach, _ := cmd.Flags().GetString("detach")
		utils.PayloadOnStdout = true
		corgi, err := utils.GetCorgiServices(cmd)
		if err != nil {
			exitWithError("worktree_add", err, 1)
		}
		services, err := servicesByName(corgi, args)
		if err != nil {
			exitWithError("worktree_add", err, 2)
		}
		composeDir := utils.CorgiComposePathDir
		switch {
		case detach != "" && branch != "":
			exitWithError("worktree_add", fmt.Errorf("pass --branch or --detach, not both"), 2)
		case detach != "":
			addDetachedWorktrees(services, composeDir, detach)
		case branch != "":
			addBranchWorktrees(corgi, composeDir, branch, baseBranch, args)
		default:
			exitWithError("worktree_add", fmt.Errorf("--branch or --detach is required"), 2)
		}
	},
}

func servicesByName(corgi *utils.CorgiCompose, names []string) ([]utils.Service, error) {
	byName := map[string]utils.Service{}
	known := make([]string, 0, len(corgi.Services))
	for _, svc := range corgi.Services {
		byName[svc.ServiceName] = svc
		known = append(known, svc.ServiceName)
	}
	out := make([]utils.Service, 0, len(names))
	for _, n := range names {
		svc, ok := byName[n]
		if !ok {
			return nil, fmt.Errorf("no service %q in corgi-compose.yml (have: %s)", n, strings.Join(known, ", "))
		}
		out = append(out, svc)
	}
	return out, nil
}

func addBranchWorktrees(corgi *utils.CorgiCompose, composeDir, branch, baseBranch string, names []string) {
	set, err := utils.MaterializeBranchFromBase(corgi, composeDir, branch, baseBranch, names)
	if err != nil {
		exitWithError("worktree_add", err, 1)
	}
	byService := map[string]utils.RepoWorktree{}
	failed := false
	for _, w := range set.Worktrees {
		byService[w.Service] = w
		if w.Skipped != "" {
			failed = true
		}
	}
	if utils.JSONOutput {
		utils.PrintJSON(set)
	} else {
		for _, n := range names {
			printWorktreeLine(byService[n])
		}
	}
	if failed {
		exitProcess(1)
	}
}

func printWorktreeLine(w utils.RepoWorktree) {
	if w.Skipped != "" {
		fmt.Fprintf(os.Stderr, "%s: %s\n", w.Service, w.Skipped)
		return
	}
	note := "reused"
	if w.Created {
		note = "new branch"
	}
	fmt.Fprintf(os.Stderr, "%s → %s (%s)\n", w.Service, w.Branch, note)
	fmt.Println(w.Dir)
}

func addDetachedWorktrees(services []utils.Service, composeDir, ref string) {
	set := utils.WorktreeSet{}
	failed := false
	for _, svc := range services {
		w := utils.RepoWorktree{Service: svc.ServiceName, Repo: svc.AbsolutePath}
		dir, err := utils.MaterializeDetachedWorktree(svc.AbsolutePath, composeDir, ref)
		if err != nil {
			w.Skipped, failed = err.Error(), true
		} else {
			w.Dir = dir
		}
		set.Worktrees = append(set.Worktrees, w)
	}
	if utils.JSONOutput {
		utils.PrintJSON(set)
	} else {
		for _, w := range set.Worktrees {
			if w.Skipped != "" {
				fmt.Fprintf(os.Stderr, "%s: %s\n", w.Service, w.Skipped)
				continue
			}
			fmt.Fprintf(os.Stderr, "%s → %s (detached)\n", w.Service, ref)
			fmt.Println(w.Dir)
		}
	}
	if failed {
		exitProcess(1)
	}
}

var worktreePruneForce bool

var worktreePruneCmd = &cobra.Command{
	Use:     "prune",
	Aliases: []string{"clean"},
	Short:   "Remove corgi-created service worktrees (keeps ones with uncommitted work)",
	Run: func(cmd *cobra.Command, _ []string) {
		base, err := worktreeBaseHere(cmd)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			exitProcess(1)
		}
		skipped, err := utils.CleanWorktreesUnder(base, worktreePruneForce)
		if err != nil {
			fmt.Fprintln(os.Stderr, "couldn't prune worktrees:", err)
			exitProcess(1)
		}
		fmt.Println("🗑️ pruned corgi worktrees")
		if len(skipped) > 0 {
			fmt.Printf("kept %d with uncommitted changes (--force to drop):\n", len(skipped))
			for _, d := range skipped {
				fmt.Println(" ", d)
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(worktreeCmd)
	worktreeCmd.AddCommand(worktreeListCmd)
	worktreeCmd.AddCommand(worktreePruneCmd)
	worktreeCmd.AddCommand(worktreeAddCmd)
	worktreeAddCmd.Flags().String("branch", "", "branch to work on; made when it does not exist yet")
	worktreeAddCmd.Flags().String("base", "", "start a new branch from origin/<base> (fetched first) instead of the checkout's HEAD")
	worktreeAddCmd.Flags().String("detach", "", "check out this commit or ref read-only instead of a branch (e.g. FETCH_HEAD)")
	worktreePruneCmd.Flags().BoolVar(&worktreePruneForce, "force", false, "Also remove worktrees with uncommitted or untracked changes (discards that work)")
}
