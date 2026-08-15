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
	"andriiklymiuk/corgi/utils/art"

	"github.com/spf13/cobra"
)

// captureWorkspaceBrief is the daemon's probe: what did the session that just
// ended leave on disk?
//
// Best-effort throughout. A workspace whose compose file has been moved or
// broken still gets a brief saying the session restarted — losing the note is
// never a reason to hold up a restart.
func captureWorkspaceBrief(p brief.Params) *brief.Brief {
	b := brief.Capture(p, probeWorkspaceRepos(p.Dir))
	return &b
}

// probeWorkspaceRepos reads every checkout in a stack: the services' own
// directories, plus any worktrees a cross-repo branch materialized.
//
// The worktrees matter most. They are the thing Remote Control cannot make and
// the thing a restarted session has no way to discover — a branch spread across
// four repositories looks like nothing at all from a fresh session's cwd.
func probeWorkspaceRepos(dir string) []brief.RepoState {
	if dir == "" {
		return nil
	}

	// Parsing a compose file mutates process-wide state (root command flags,
	// utils.CorgiComposePath*), which is why every MCP handler is serialized
	// behind this same lock. Briefs are captured from one goroutine per
	// workspace, so without it two workspaces restarting together can hand each
	// other's services back.
	mcpHandlerMu.Lock()
	corgi, err := loadComposeAtDir(dir)
	mcpHandlerMu.Unlock()

	byPrefix := map[string]string{}
	var out []brief.RepoState
	if err == nil && corgi != nil {
		for service, path := range utils.ServiceDirs(corgi, nil) {
			byPrefix[utils.WorktreeDirPrefix(path)] = service
			if state, ok := repoState(service, path, false); ok {
				out = append(out, state)
			}
		}
	}
	return append(out, probeWorktreeRepos(dir, byPrefix)...)
}

// probeWorktreeRepos scans the agent worktree directory rather than asking for
// a branch, because the point is to report a branch nobody remembered.
//
// byPrefix maps a worktree directory's prefix back to the service that owns it.
// The prefix is "<repo-basename>-<hash>", so splitting the name on "@" would
// label every service "api-3f2a1b"; when the compose file cannot be read the
// hash is trimmed instead, which at least yields the repository's name.
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
			continue // not a worktree this scheme created
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

// worktreeHashSuffix matches the "-<6 hex>" that WorktreeDirPrefix appends.
var worktreeHashSuffix = regexp.MustCompile(`-[0-9a-f]{6}$`)

func trimWorktreeHash(prefix string) string {
	return worktreeHashSuffix.ReplaceAllString(prefix, "")
}

func repoState(service, path string, worktree bool) (brief.RepoState, bool) {
	// ProbeAgentWork would additionally shell out to gh/glab with no timeout.
	// A restart caused by the network going away must not then block on GitHub
	// once per repository.
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
	}, true
}

// loadComposeAtDir parses the compose file in dir. Callers must hold
// mcpHandlerMu: the loader underneath mutates process-wide state.
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

// ---------------------------------------------------------------- brief

var agentBriefCmd = &cobra.Command{
	Use:   "brief [workspace]",
	Short: "What the last supervised session was working on before it restarted",
	Long: `A restarted session is a NEW session: the previous conversation, and
everything it had worked out, is gone.

corgi cannot restore that. What it does keep is the part that survives on disk —
which branch each repository is on, which hold uncommitted work, and which
worktrees a cross-repo branch left behind — captured at the moment the old
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
		if b == nil {
			if asJSON {
				printJSON(nil)
				return
			}
			utils.Infof("no brief for %s — it has not restarted since the daemon started\n", args[0])
			return
		}
		printBriefs([]brief.Brief{*b}, asJSON)
		return
	}

	briefs, err := brief.List(dir)
	if err != nil {
		exitWithError("agent_brief", err, 1)
	}
	printBriefs(briefs, asJSON)
}

func printBriefs(briefs []brief.Brief, asJSON bool) {
	if asJSON {
		// Never nil: docs/agents.md promises an array, and a `null` here makes
		// every consumer that iterates the result special-case the empty case.
		if briefs == nil {
			briefs = []brief.Brief{}
		}
		printJSON(briefs)
		return
	}
	if len(briefs) == 0 {
		utils.Infof("no briefs yet — nothing has restarted\n")
		return
	}
	for _, b := range briefs {
		utils.Info(art.BlueColor, b.WorkspaceID, art.WhiteColor)
		utils.Infof("  ended   %s (%s)\n", b.EndedAt.Local().Format("2006-01-02 15:04"), b.Cause)
		if b.Reason != "" {
			utils.Infof("  reason  %s\n", b.Reason)
		}
		if summary := b.Summary(); summary != "" {
			utils.Infof("  state   %s\n", summary)
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
			utils.Infof("    %-16s %s%s%s\n", r.Service, orDash(r.Branch), dirty, marker)
		}
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// printJSON writes a value as pure JSON on stdout, matching the --json contract
// the rest of the agent commands follow.
func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
