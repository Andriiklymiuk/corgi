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
	Short:   "Manage worktrees corgi created for --service-branch",
}

var worktreeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List corgi-created service worktrees",
	Run: func(cmd *cobra.Command, _ []string) {
		if _, err := utils.GetCorgiServices(cmd); err != nil {
			fmt.Fprintln(os.Stderr, err)
			exitProcess(1)
		}
		base := filepath.Join(utils.CorgiComposePathDir, "corgi_services", ".worktrees")
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
		if _, err := utils.GetCorgiServices(cmd); err != nil {
			fmt.Fprintln(os.Stderr, err)
			exitProcess(1)
		}
		skipped, err := utils.CleanCorgiWorktrees(worktreePruneForce)
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
