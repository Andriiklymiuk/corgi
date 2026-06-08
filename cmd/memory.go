package cmd

import (
	"andriiklymiuk/corgi/utils"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// memoryRoot resolves the workspace memory dir next to corgi-compose.yml. We use cwd
// (the workspace root) — memory is committed beside the compose file, not under the
// gitignored corgi_services/.
func memoryRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		wd = "."
	}
	return filepath.Join(wd, utils.MemoryDirName)
}

var memoryCmd = &cobra.Command{
	Use:   "memory",
	Short: "Read and maintain the committed workspace memory store (.corgi/memory)",
	Long: `Workspace memory is a committed, team-shared store of decisions, incidents,
domain facts, and recurring fixes for this corgi stack. It is opt-in: with no
.corgi/memory/ directory every subcommand is a harmless no-op.

  corgi memory list [--type <t>] [--json]   list facts (descriptions; bodies stay on disk)
  corgi memory add  --type <t> --name <n> --desc <d> [--service <s>] [--pattern <p>]
  corgi memory index                        regenerate index.md from the facts
  corgi memory lint [--json]                validate frontmatter, names, links, and NO SECRETS

Committed memory must never contain secrets — lint fails the store on a key-shaped
string.`,
}

var memoryListCmd = &cobra.Command{
	Use:   "list",
	Short: "List memory facts",
	Run: func(cmd *cobra.Command, _ []string) {
		typeFilter, _ := cmd.Flags().GetString("type")
		facts, err := utils.ReadFacts(memoryRoot())
		if err != nil {
			failMemory(err)
			return
		}
		if typeFilter != "" {
			filtered := facts[:0]
			for _, f := range facts {
				if f.Type == typeFilter {
					filtered = append(filtered, f)
				}
			}
			facts = filtered
		}
		if utils.JSONOutput {
			if facts == nil {
				facts = []utils.Fact{}
			}
			utils.PrintJSON(facts)
			return
		}
		if len(facts) == 0 {
			utils.Info("No workspace memory facts (.corgi/memory absent or empty).")
			return
		}
		for _, f := range facts {
			utils.Infof("[%s] %s — %s\n", f.Type, f.Name, f.Description)
		}
	},
}

var memoryAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Scaffold a new memory fact",
	Run: func(cmd *cobra.Command, _ []string) {
		t, _ := cmd.Flags().GetString("type")
		name, _ := cmd.Flags().GetString("name")
		desc, _ := cmd.Flags().GetString("desc")
		svc, _ := cmd.Flags().GetString("service")
		pattern, _ := cmd.Flags().GetString("pattern")
		if t == "" || name == "" || desc == "" {
			fmt.Fprintln(os.Stderr, "memory add requires --type, --name and --desc")
			os.Exit(2)
		}
		path, err := utils.AddFact(memoryRoot(), utils.Fact{
			Name: name, Description: desc, Type: t, Service: svc, Pattern: pattern,
		})
		if err != nil {
			failMemory(err)
			return
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]string{"created": path, "type": t, "name": name})
			return
		}
		utils.Infof("Wrote %s — edit the body, then run: corgi memory index\n", path)
	},
}

var memoryIndexCmd = &cobra.Command{
	Use:   "index",
	Short: "Regenerate .corgi/memory/index.md",
	Run: func(cmd *cobra.Command, _ []string) {
		root := memoryRoot()
		facts, err := utils.ReadFacts(root)
		if err != nil {
			failMemory(err)
			return
		}
		if len(facts) == 0 {
			utils.Info("No facts to index (.corgi/memory absent or empty).")
			return
		}
		idxPath := filepath.Join(root, "index.md")
		if err := os.WriteFile(idxPath, []byte(utils.RenderIndex(facts)), 0o644); err != nil {
			failMemory(err)
			return
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"index": idxPath, "facts": len(facts)})
			return
		}
		utils.Infof("Regenerated %s (%d facts)\n", idxPath, len(facts))
	},
}

var memoryLintCmd = &cobra.Command{
	Use:   "lint",
	Short: "Validate the memory store (frontmatter, names, links, no secrets)",
	Run: func(cmd *cobra.Command, _ []string) {
		errs, warns := utils.LintFacts(memoryRoot())
		if errs == nil {
			errs = []utils.MemoryIssue{}
		}
		if warns == nil {
			warns = []utils.MemoryIssue{}
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"ok": len(errs) == 0, "errors": errs, "warnings": warns})
			if len(errs) > 0 {
				os.Exit(1)
			}
			return
		}
		for _, e := range errs {
			utils.Infof("✗ [%s] %s (%s)\n", e.Code, e.Message, e.File)
		}
		for _, w := range warns {
			utils.Infof("⚠ [%s] %s (%s)\n", w.Code, w.Message, w.File)
		}
		if len(errs) == 0 {
			utils.Infof("memory ok — %d warning(s)\n", len(warns))
			return
		}
		os.Exit(1)
	},
}

// failMemory reports an unexpected IO/parse error consistently.
func failMemory(err error) {
	if utils.JSONOutput {
		utils.JSONError(utils.ErrConfig, err.Error())
	} else {
		fmt.Fprintf(os.Stderr, "memory: %s\n", err)
	}
	os.Exit(1)
}

func init() {
	memoryListCmd.Flags().String("type", "", "filter by type (decision|incident|domain|fix)")
	memoryAddCmd.Flags().String("type", "", "fact type (decision|incident|domain|fix)")
	memoryAddCmd.Flags().String("name", "", "kebab-case unique name (== filename)")
	memoryAddCmd.Flags().String("desc", "", "one-line description (shown in the index)")
	memoryAddCmd.Flags().String("service", "", "optional corgi-compose service this concerns")
	memoryAddCmd.Flags().String("pattern", "", "fix-type only: recurrence key for learned-skill detection")
	memoryCmd.AddCommand(memoryListCmd, memoryAddCmd, memoryIndexCmd, memoryLintCmd)
	rootCmd.AddCommand(memoryCmd)
}
