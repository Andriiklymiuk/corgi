package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

var worktreeCmd = &cobra.Command{
	Use:     "worktree",
	Aliases: []string{"wt"},
	Short:   "Manage worktrees corgi created for --service-branch and isolated sessions",
}

// worktreeBaseHere is where this folder's corgi worktrees live: under the
// compose file's corgi_services, or a bare repository's own when a session
// was isolated without a stack.
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
	worktreePruneCmd.Flags().BoolVar(&worktreePruneForce, "force", false, "Also remove worktrees with uncommitted or untracked changes (discards that work)")
}
