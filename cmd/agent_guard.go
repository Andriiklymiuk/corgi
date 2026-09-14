package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"github.com/spf13/cobra"
)

// The guard: a git pre-push hook that asks the board about the branch
// being pushed. A session on it whose done-when gate is red, or whose last
// test run went red, means the push is refused with the command that
// failed — so an agent's red branch does not become a red pull request.
// CORGI_FORCE=1 pushes anyway; a branch no session sits on is not the
// guard's business.

const guardMarker = "# corgi agent guard"

// guardVerdict says whether a push of branch from repo may go, and why not.
func guardVerdict(st sessions.State, repo, branch string) (bool, string) {
	repo = filepath.Clean(repo)
	for _, s := range st.Sessions {
		if s.Branch != branch || s.Cwd == "" {
			continue
		}
		cwd := filepath.Clean(s.Cwd)
		if cwd != repo && !strings.HasPrefix(cwd, repo+string(filepath.Separator)) && !strings.HasPrefix(repo, cwd+string(filepath.Separator)) {
			continue
		}
		who := firstNonEmpty(s.Display, s.Label)
		if s.Gate != nil && !s.Gate.OK {
			return false, fmt.Sprintf("%s's gate is red: %s (failed %d×) — fix it, or CORGI_FORCE=1 git push", who, s.Gate.Cmd, max(1, s.Gate.Fails))
		}
		if s.Tests != nil && !s.Tests.OK {
			return false, fmt.Sprintf("%s's last test run went red: %s — fix it, or CORGI_FORCE=1 git push", who, s.Tests.Cmd)
		}
	}
	return true, ""
}

func hooksDir(repo string) (string, error) {
	out, err := exec.Command("git", "-C", repo, "rev-parse", "--git-path", "hooks").Output()
	if err != nil {
		return "", fmt.Errorf("%s is not a git checkout", repo)
	}
	dir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(repo, dir)
	}
	return dir, nil
}

const guardHook = `#!/bin/sh
` + guardMarker + ` — refuses a push of a branch whose session's gate or tests are red
# (corgi agent guard uninstall removes it; CORGI_FORCE=1 pushes anyway)
[ -n "$CORGI_FORCE" ] && exit 0
command -v corgi >/dev/null 2>&1 || exit 0
exec corgi agent guard check
`

// installGuard writes the pre-push hook; another hook already there is
// left alone unless force.
func installGuard(repo string, force bool) (string, error) {
	dir, err := hooksDir(repo)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "pre-push")
	if data, err := os.ReadFile(path); err == nil && !strings.Contains(string(data), guardMarker) && !force {
		return "", fmt.Errorf("%s already has a pre-push hook that is not corgi's — --force replaces it", repo)
	}
	return path, os.WriteFile(path, []byte(guardHook), 0o755)
}

// uninstallGuard removes the hook when it is corgi's.
func uninstallGuard(repo string) error {
	dir, err := hooksDir(repo)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "pre-push")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !strings.Contains(string(data), guardMarker) {
		return fmt.Errorf("%s is not corgi's hook; left alone", path)
	}
	return os.Remove(path)
}

var agentGuardCmd = &cobra.Command{
	Use:   "guard [install|uninstall|check]",
	Short: "A pre-push hook that refuses a branch whose session's gate or tests are red",
	Long: `Installs a git pre-push hook in this checkout. On every push the hook asks
the board about the branch: a session on it whose done-when gate is red, or
whose last test run went red, stops the push and says which command failed.
A branch with no session on it is not the guard's business. CORGI_FORCE=1
git push goes through anyway.

  corgi agent guard install          # this checkout
  corgi agent guard uninstall
  corgi agent guard check            # what the hook runs; exit 1 = refused`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		what := "check"
		if len(args) == 1 {
			what = args[0]
		}
		repo, err := os.Getwd()
		if err != nil {
			exitWithError("agent_guard", err, 1)
		}
		force, _ := cmd.Flags().GetBool("force")
		switch what {
		case "install":
			path, err := installGuard(repo, force)
			if err != nil {
				exitWithError("agent_guard", err, 1)
			}
			utils.Infof("guard installed: %s\n", path)
		case "uninstall":
			if err := uninstallGuard(repo); err != nil {
				exitWithError("agent_guard", err, 1)
			}
			utils.Info("guard removed")
		case "check":
			out, err := exec.Command("git", "-C", repo, "rev-parse", "--abbrev-ref", "HEAD").Output()
			if err != nil {
				return // not a checkout: nothing to guard
			}
			branch := strings.TrimSpace(string(out))
			board, err := readBoard(mustAgentDir())
			if err != nil {
				return // no board, no opinion
			}
			ok, why := guardVerdict(board.State, repo, branch)
			if utils.JSONOutput {
				utils.PrintJSON(map[string]any{"branch": branch, "ok": ok, "why": why})
			}
			if !ok {
				fmt.Fprintln(os.Stderr, "corgi agent guard: "+why)
				os.Exit(1)
			}
		default:
			exitWithError(utils.ErrUsage, fmt.Errorf("install, uninstall or check"), 2)
		}
	},
}

func init() {
	agentGuardCmd.Flags().Bool("force", false, "Replace a pre-push hook that is not corgi's")
	agentCmd.AddCommand(agentGuardCmd)
}
