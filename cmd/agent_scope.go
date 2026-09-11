package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/scope"
	"github.com/spf13/cobra"
)

var agentScopeCmd = &cobra.Command{
	Use:   "scope",
	Short: "The contract a ticket's change stays inside: paths, a diff budget, done-when",
	Long: `A scope is written once the spec is agreed and enforced while the session
runs: a write outside its paths is refused with the way to widen; a diff over
its budget is reported once at the end of the turn, not found at review.
Both hooks are installed by ` + "`corgi agent track enable`" + ` and silent on a branch
with no scope.

  corgi agent scope set ABC-123 --path "api/limits/**" --path "web/src/limits/**" \\
    --lines 400 --tests 2 --done "429 carries Retry-After" --done "banner on the web"
  corgi agent scope add ABC-123 --path "shared/types.ts"     # widen, on the record
  corgi agent scope show [ABC-123]
  corgi agent scope clear ABC-123`,
}

var agentScopeSetCmd = &cobra.Command{
	Use:   "set <ref>",
	Short: "Write a ticket's scope",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := scopeDir(cmd)
		f := cmd.Flags()
		paths, _ := f.GetStringArray("path")
		lines, _ := f.GetInt("lines")
		tests, _ := f.GetInt("tests")
		done, _ := f.GetStringArray("done")
		s := scope.Scope{Ref: strings.ToUpper(strings.TrimSpace(args[0])), Paths: paths, Lines: lines, Tests: tests, Done: done, SetAt: time.Now()}
		if old, err := scope.Read(dir, s.Ref); err == nil {
			// A budget raised on the record keeps the paths it had, and the
			// paths it was given keep the budget it had.
			if len(s.Paths) == 0 {
				s.Paths = old.Paths
			}
			if s.Lines == 0 {
				s.Lines = old.Lines
			}
			if s.Tests == 0 {
				s.Tests = old.Tests
			}
			if len(s.Done) == 0 {
				s.Done = old.Done
			}
			s.Widenings = old.Widenings
		}
		if err := scope.Write(dir, s); err != nil {
			exitWithError("agent_scope", err, 1)
		}
		if utils.JSONOutput {
			utils.PrintJSON(s)
			return
		}
		fmt.Printf("✓ scope for %s: %s\n", s.Ref, scopeLine(s))
	},
}

var agentScopeAddCmd = &cobra.Command{
	Use:   "add <ref> --path <glob>",
	Short: "Widen a ticket's scope by one path, on the record",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := scopeDir(cmd)
		paths, _ := cmd.Flags().GetStringArray("path")
		if len(paths) == 0 {
			exitWithError("agent_scope", fmt.Errorf("--path is required"), 2)
		}
		var s scope.Scope
		var err error
		for _, p := range paths {
			if s, err = scope.Widen(dir, strings.ToUpper(strings.TrimSpace(args[0])), p, "session"); err != nil {
				exitWithError("agent_scope", err, 1)
			}
		}
		fmt.Printf("✓ %s now also covers %s (%d widening%s on the record)\n", s.Ref, strings.Join(paths, ", "), len(s.Widenings), plural2(len(s.Widenings)))
	},
}

var agentScopeShowCmd = &cobra.Command{
	Use:   "show [ref]",
	Short: "Print a ticket's scope, or every scope here",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := scopeDir(cmd)
		if len(args) == 1 {
			s, err := scope.Read(dir, strings.ToUpper(strings.TrimSpace(args[0])))
			if err != nil {
				exitWithError("agent_scope", fmt.Errorf("no scope for %s in %s", args[0], dir), 2)
			}
			if utils.JSONOutput {
				utils.PrintJSON(s)
				return
			}
			fmt.Printf("%s: %s\n", s.Ref, scopeLine(s))
			for _, d := range s.Done {
				fmt.Printf("  done when: %s\n", d)
			}
			for _, w := range s.Widenings {
				fmt.Printf("  widened: %s (%s)\n", w.Path, w.At.Local().Format("Jan 2 15:04"))
			}
			return
		}
		list := scope.List(dir)
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"workspace": dir, "scopes": list})
			return
		}
		if len(list) == 0 {
			fmt.Printf("No scopes in %s\n", scope.Dir(dir))
			return
		}
		for _, s := range list {
			fmt.Printf("%-12s %s\n", s.Ref, scopeLine(s))
		}
	},
}

var agentScopeClearCmd = &cobra.Command{
	Use:   "clear <ref>",
	Short: "Remove a ticket's scope",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := scope.Remove(scopeDir(cmd), strings.ToUpper(strings.TrimSpace(args[0]))); err != nil {
			exitWithError("agent_scope", err, 1)
		}
		fmt.Printf("cleared %s\n", args[0])
	},
}

func scopeLine(s scope.Scope) string {
	var parts []string
	if len(s.Paths) > 0 {
		parts = append(parts, strings.Join(s.Paths, ", "))
	} else {
		parts = append(parts, "any path")
	}
	if s.Lines > 0 {
		parts = append(parts, fmt.Sprintf("≤ %d lines", s.Lines))
	}
	if s.Tests > 0 {
		parts = append(parts, fmt.Sprintf("≤ %d new test files", s.Tests))
	}
	return strings.Join(parts, " · ")
}

func scopeDir(cmd *cobra.Command) string {
	if d, _ := cmd.Flags().GetString("dir"); d != "" {
		return d
	}
	cwd, _ := os.Getwd()
	if root := scopeWorkspaceRoot(cwd); root != "" {
		return root
	}
	return cwd
}

func plural2(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func init() {
	for _, c := range []*cobra.Command{agentScopeSetCmd, agentScopeAddCmd, agentScopeShowCmd, agentScopeClearCmd} {
		c.Flags().String("dir", "", "the workspace (default: the one containing the current directory)")
	}
	agentScopeSetCmd.Flags().StringArray("path", nil, "a glob the change may touch, relative to the workspace (repeatable; ** crosses directories)")
	agentScopeSetCmd.Flags().Int("lines", 0, "diff budget: added plus removed lines")
	agentScopeSetCmd.Flags().Int("tests", 0, "how many new test files at most")
	agentScopeSetCmd.Flags().StringArray("done", nil, "what finished means, one line (repeatable)")
	agentScopeAddCmd.Flags().StringArray("path", nil, "a glob to add (repeatable)")
	agentScopeCmd.AddCommand(agentScopeSetCmd, agentScopeAddCmd, agentScopeShowCmd, agentScopeClearCmd)
	agentCmd.AddCommand(agentScopeCmd)
}
