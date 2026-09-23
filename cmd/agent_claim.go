package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/harness"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/watch"
	"github.com/spf13/cobra"
)

func claimFor(st sessions.State, ref, cwd string) (sessions.Session, error) {
	if ref != "" {
		return findBoardSession(st, ref)
	}
	for _, name := range harness.Names() {
		env := harness.For(name, "").SessionIDEnv
		if id := strings.TrimSpace(os.Getenv(env)); env != "" && id != "" {
			if s, err := findBoardSession(st, id); err == nil {
				return s, nil
			}
		}
	}
	var best sessions.Session
	for _, s := range st.Sessions {
		if s.Status == sessions.StatusGone || s.Cwd == "" {
			continue
		}
		if samePath(s.Cwd, cwd) || sessions.Within(cwd, s.Cwd) {
			if best.ID == "" || len(s.Cwd) > len(best.Cwd) {
				best = s
			}
		}
	}
	if best.ID == "" {
		return sessions.Session{}, fmt.Errorf("no session on the board for %s - say --session <id>", cwd)
	}
	return best, nil
}

func repoRelative(repo, p string) (string, error) {
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(mustGetwd(), p)
	}
	rel, err := filepath.Rel(repo, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("%s is outside %s", p, repo)
	}
	return filepath.ToSlash(rel), nil
}

func mustGetwd() string {
	d, _ := os.Getwd()
	return d
}

var agentClaimCmd = &cobra.Command{
	Use:   "claim <file>... [--session <id>] [--release]",
	Short: "Say which files this session is on, so another session in the repository is told",
	Long: `A claim is advisory: "these files are mine for now". A session that starts
in the same repository reads the claims in its context and stays off those
files or asks first; the daemon rings once when a session edits a file
another one claimed. Nothing is locked. A claim ends with --release, when
its session leaves the board, or after a day.

Inside a session (a hook, a tool call) the session is known; elsewhere say
--session, or run it in the session's checkout.

  corgi agent claim auth/refresh.go auth/session.go
  corgi agent claim --release auth/session.go
  corgi agent claim --release                    # all of this session's
  corgi agent claims                             # who holds what`,
	Args: cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		ref, _ := cmd.Flags().GetString("session")
		release, _ := cmd.Flags().GetBool("release")
		board, err := readBoard(dir)
		if err != nil {
			exitWithError("agent_claim", err, 1)
		}
		s, err := claimFor(board.State, ref, mustGetwd())
		if err != nil {
			exitWithError("agent_claim", err, 2)
		}
		repo := workspaceRootOfDir(s.Cwd)
		var paths []string
		for _, a := range args {
			rel, err := repoRelative(repo, a)
			if err != nil {
				exitWithError("agent_claim", err, 2)
			}
			paths = append(paths, rel)
		}
		log := watch.LoadFileClaims(dir)
		if release {
			n, err := log.Release(s.ID, paths)
			if err != nil {
				exitWithError("agent_claim", err, 1)
			}
			if utils.JSONOutput {
				utils.PrintJSON(map[string]any{"session": s.ID, "released": n})
				return
			}
			utils.Infof("released %d claim(s) of %s\n", n, firstNonEmpty(s.Display, s.Label))
			return
		}
		if len(paths) == 0 {
			exitWithError(utils.ErrUsage, fmt.Errorf("which files? corgi agent claim <file>..."), 2)
		}
		taken, err := log.Set(repo, s.ID, firstNonEmpty(s.Display, s.Label), paths, time.Now())
		if err != nil {
			exitWithError("agent_claim", err, 1)
		}
		nudgeDaemon(dir)
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"session": s.ID, "repo": repo, "claimed": paths, "taken": taken})
			return
		}
		utils.Infof("%s claims %s\n", firstNonEmpty(s.Display, s.Label), strings.Join(paths, ", "))
		for _, t := range taken {
			utils.Infof("  taken over from %s: %s\n", firstNonEmpty(t.Label, t.Session), t.Path)
		}
	},
}

var agentClaimsCmd = &cobra.Command{
	Use:   "claims",
	Short: "Who holds which files, across the board",
	Run: func(cmd *cobra.Command, _ []string) {
		dir := mustAgentDir()
		board, _ := readBoard(dir)
		live := map[string]bool{}
		for _, s := range board.Sessions {
			if s.Status != sessions.StatusGone {
				live[s.ID] = true
			}
		}
		claims := watch.LoadFileClaims(dir).Live(live, time.Now())
		if utils.JSONOutput {
			if claims == nil {
				claims = []watch.FileClaim{}
			}
			utils.PrintJSON(map[string]any{"claims": claims})
			return
		}
		if len(claims) == 0 {
			fmt.Println("no claims: corgi agent claim <file> inside a session")
			return
		}
		for _, c := range claims {
			fmt.Printf("%-40s %-20s %s ago\n", c.Path, firstNonEmpty(c.Label, c.Session), roughAge(time.Since(c.At)))
		}
	},
}

func workspaceRootOfDir(dir string) string {
	if root := sessions.CommonRoot(dir); root != "" {
		return root
	}
	return dir
}

func init() {
	agentClaimCmd.Flags().String("session", "", "The session the claim is for (default: the one this runs inside, or whose checkout this is)")
	agentClaimCmd.Flags().Bool("release", false, "Give the files back - the ones named, or all of this session's")
	agentCmd.AddCommand(agentClaimCmd, agentClaimsCmd)
}
