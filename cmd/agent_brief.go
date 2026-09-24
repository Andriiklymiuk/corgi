package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/brief"
	"andriiklymiuk/corgi/utils/agent/events"
	"andriiklymiuk/corgi/utils/art"

	"github.com/spf13/cobra"
)

func captureWorkspaceBrief(p brief.Params) *brief.Brief {
	b := brief.Capture(p, probeWorkspaceRepos(p.Dir))
	return &b
}

func probeWorkspaceRepos(dir string) []brief.RepoState {
	if dir == "" {
		return nil
	}

	mcpHandlerMu.Lock()
	corgi, err := loadComposeAtDir(dir)
	mcpHandlerMu.Unlock()

	byPrefix := map[string]string{}
	var out []brief.RepoState
	if err != nil || corgi == nil {
		if state, ok := repoState(filepath.Base(dir), dir, false); ok {
			out = append(out, state)
		}
	}
	if err == nil && corgi != nil {
		for service, path := range utils.ServiceDirs(corgi, nil) {
			if root, ok := utils.RepoRootOf(path); ok {
				byPrefix[utils.WorktreeDirPrefix(root)] = service
			}
			if state, ok := repoState(service, path, false); ok {
				out = append(out, state)
			}
		}
	}
	return append(out, probeWorktreeRepos(dir, byPrefix)...)
}

func probeWorktreeRepos(dir string, byPrefix map[string]string) []brief.RepoState {
	base := utils.AgentWorktreeBase(dir)
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var out []brief.RepoState
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		prefix, _, ok := strings.Cut(e.Name(), "@")
		if !ok {
			continue
		}
		service := byPrefix[prefix]
		if service == "" {
			service = trimWorktreeHash(prefix)
		}
		if state, ok := repoState(service, filepath.Join(base, e.Name()), true); ok {
			out = append(out, state)
		}
	}
	return out
}

var worktreeHashSuffix = regexp.MustCompile(`-[0-9a-f]{6}$`)

func trimWorktreeHash(prefix string) string {
	return worktreeHashSuffix.ReplaceAllString(prefix, "")
}

func repoState(service, path string, worktree bool) (brief.RepoState, bool) {
	st, ok := utils.ProbeRepoState(path)
	if !ok {
		return brief.RepoState{}, false
	}
	return brief.RepoState{
		Service:  service,
		Dir:      path,
		Branch:   st.Branch,
		Dirty:    st.Dirty,
		Worktree: worktree,
		Default:  st.Branch != "" && st.Branch == utils.LocalDefaultBranchOf(path),
	}, true
}

func loadComposeAtDir(dir string) (*utils.CorgiCompose, error) {
	for _, name := range []string{"corgi-compose.yml", "corgi-compose.yaml"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		corgi, _, err := loadComposeForAgent(path)
		return corgi, err
	}
	return nil, fmt.Errorf("no compose file in %s", dir)
}

var agentBriefCmd = &cobra.Command{
	Use:   "brief [workspace]",
	Short: "What the last supervised session was working on before it restarted",
	Long: `A restarted session is a NEW session: the previous conversation, and
everything it had worked out, is gone.

corgi cannot restore that. What it does keep is the part that survives on disk -
which branch each repository is on, which hold uncommitted work, and which
worktrees a cross-repo branch left behind - captured at the moment the old
session ended.

With no argument, every workspace that has one, newest first.`,
	Args: cobra.MaximumNArgs(1),
	Run:  runAgentBrief,
}

func runAgentBrief(cmd *cobra.Command, args []string) {
	dir, err := agentDir()
	if err != nil {
		exitWithError("agent_data_dir", err, 1)
	}
	asJSON, _ := cmd.Flags().GetBool("json")

	if len(args) == 1 {
		b, readErr := brief.Read(dir, args[0])
		if readErr != nil {
			exitWithError("agent_brief", readErr, 1)
		}
		if asJSON {
			printJSON(b)
			return
		}
		if b == nil {
			utils.Infof("no brief for %s - it has not restarted since the daemon started\n", args[0])
			return
		}
		printBriefs([]brief.Brief{*b}, false)
		printExitOutput(dir, args[0])
		return
	}

	briefs, err := brief.List(dir)
	if err != nil {
		exitWithError("agent_brief", err, 1)
	}
	printBriefs(briefs, asJSON)
}

const exitOutputLines = 8

// printExitOutput shows the tail of what the process printed before it died -
// the reason line names one line; the rest is here for the crash nobody expected.
func printExitOutput(dir, workspaceID string) {
	lines := exitOutputTail(events.ExitOutputPath(dir, workspaceID), exitOutputLines)
	if len(lines) == 0 {
		return
	}
	utils.Infof("  output  (last %d lines before the exit, %s)\n", len(lines), events.ExitOutputPath(dir, workspaceID))
	for _, line := range lines {
		utils.Infof("    %s\n", line)
	}
}

func exitOutputTail(path string, n int) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, " ")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "# ") {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

func printBriefs(briefs []brief.Brief, asJSON bool) {
	if asJSON {
		if briefs == nil {
			briefs = []brief.Brief{}
		}
		printJSON(briefs)
		return
	}
	utils.Infof("%s", formatBriefs(briefs))
}

func formatBriefs(briefs []brief.Brief) string {
	if len(briefs) == 0 {
		return "no briefs yet - nothing has restarted\n"
	}
	var out strings.Builder
	for _, b := range briefs {
		fmt.Fprintf(&out, "%s%s%s\n", art.BlueColor, b.WorkspaceID, art.WhiteColor)
		fmt.Fprintf(&out, "  ended   %s (%s)\n", b.EndedAt.Local().Format("2006-01-02 15:04"), b.Cause)
		if b.Reason != "" {
			fmt.Fprintf(&out, "  reason  %s\n", b.Reason)
		}
		if summary := b.Summary(); summary != "" {
			fmt.Fprintf(&out, "  state   %s\n", summary)
		}
		for _, r := range b.Repos {
			marker := ""
			if r.Worktree {
				marker = " (worktree)"
			}
			dirty := ""
			if r.Dirty {
				dirty = " · uncommitted changes"
			}
			fmt.Fprintf(&out, "    %-16s %s%s%s\n", r.Service, orDash(r.Branch), dirty, marker)
		}
	}
	return out.String()
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
