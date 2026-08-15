package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

	var out []brief.RepoState
	if corgi, err := loadComposeAtDir(dir); err == nil && corgi != nil {
		for service, path := range utils.ServiceDirs(corgi, nil) {
			if state, ok := repoState(service, path, false); ok {
				out = append(out, state)
			}
		}
	}
	return append(out, probeWorktreeRepos(dir)...)
}

// probeWorktreeRepos scans the agent worktree directory rather than asking for
// a branch, because the point is to report a branch nobody remembered.
func probeWorktreeRepos(dir string) []brief.RepoState {
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
		// Directories are named <service>@<branch-with-slashes-flattened>. The
		// branch is read from git rather than parsed back out of the name.
		service, _, _ := strings.Cut(e.Name(), "@")
		if service == "" {
			service = e.Name()
		}
		if state, ok := repoState(service, filepath.Join(base, e.Name()), true); ok {
			out = append(out, state)
		}
	}
	return out
}

func repoState(service, path string, worktree bool) (brief.RepoState, bool) {
	work := utils.ProbeAgentWork(path)
	if work == nil {
		return brief.RepoState{}, false
	}
	branch := work.Branch
	if branch == "HEAD" {
		branch = "" // detached; a name here would be a lie
	}
	return brief.RepoState{
		Service: service,
		Dir:     path,
		Branch:  branch,
		// Not work.Dirty: that one ignores untracked files, and a session's
		// newly created files are the work most easily lost.
		Dirty:    utils.HasUncommittedWork(path),
		Worktree: worktree,
	}, true
}

// loadComposeAtDir parses the compose file in dir without disturbing the
// caller's compose context.
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
