package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"github.com/spf13/cobra"
)

type sessionUndo struct {
	Session  sessions.Session
	Dir      string
	Branch   string
	Isolated bool
	Changed  []string
	Repo     string
}

func sessionUndoPlan(s sessions.Session) (sessionUndo, error) {
	if s.Cwd == "" {
		return sessionUndo{}, fmt.Errorf("%s has no checkout on record", firstNonEmpty(s.Display, s.Label))
	}
	if s.Status == sessions.StatusWorking {
		return sessionUndo{}, fmt.Errorf("%s is still working - corgi agent interrupt %s first", firstNonEmpty(s.Display, s.Label), s.ID)
	}
	branch := s.Branch
	if branch == "" {
		branch = sessions.Branch(s.Cwd)
	}
	common, err := exec.Command("git", "-C", s.Cwd, "rev-parse", "--git-common-dir").Output()
	if err != nil {
		return sessionUndo{}, fmt.Errorf("%s is not a git checkout", s.Cwd)
	}
	repo := filepath.Dir(strings.TrimSpace(string(common)))
	if !filepath.IsAbs(repo) {
		repo = filepath.Join(s.Cwd, repo)
	}
	inTrees := strings.Contains(filepath.ToSlash(s.Cwd), "/corgi_services/.worktrees/")
	isolated := inTrees || strings.HasPrefix(branch, "corgi/")
	if !isolated {
		return sessionUndo{}, fmt.Errorf("%s shares your checkout (%s on %s) - undo works on a worktree of its own; git checkout -- <file> by hand for this one", firstNonEmpty(s.Display, s.Label), s.Cwd, branch)
	}
	out, err := exec.Command("git", "-C", s.Cwd, "status", "--porcelain").Output()
	if err != nil {
		return sessionUndo{}, fmt.Errorf("git status in %s: %v", s.Cwd, err)
	}
	var changed []string
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if len(line) > 3 {
			changed = append(changed, strings.TrimSpace(line[3:]))
		}
	}
	return sessionUndo{Session: s, Dir: s.Cwd, Branch: branch, Isolated: true, Changed: changed, Repo: repo}, nil
}

func runSessionUndo(p sessionUndo, worktree bool) error {
	if !p.Isolated {
		return fmt.Errorf("not a worktree of its own")
	}
	if len(p.Changed) > 0 {
		if out, err := exec.Command("git", "-C", p.Dir, "checkout", "--", ".").CombinedOutput(); err != nil {
			return fmt.Errorf("git checkout: %v\n%s", err, out)
		}
		if out, err := exec.Command("git", "-C", p.Dir, "clean", "-fd").CombinedOutput(); err != nil {
			return fmt.Errorf("git clean: %v\n%s", err, out)
		}
	}
	if !worktree {
		return nil
	}
	if out, err := exec.Command("git", "-C", p.Repo, "worktree", "remove", "--force", p.Dir).CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree remove: %v\n%s", err, out)
	}
	if p.Branch != "" && p.Branch != "main" && p.Branch != "master" {
		if out, err := exec.Command("git", "-C", p.Repo, "branch", "-D", p.Branch).CombinedOutput(); err != nil {
			return fmt.Errorf("git branch -D %s: %v\n%s", p.Branch, err, out)
		}
	}
	return nil
}

var agentUndoCmd = &cobra.Command{
	Use:   "undo <session> [--worktree] [--yes]",
	Short: "Drop what a session left uncommitted in its own worktree; --worktree drops the worktree and its branch too",
	Long: `A run went the wrong way. This throws away every uncommitted edit and every
new file in the session's worktree - git checkout -- . and git clean -fd -
and with --worktree removes the worktree and deletes its corgi/<ref> branch.

Only for a session with a worktree of its own (--isolate): a session on your
checkout would take your own edits with it, so it is refused. A working
session is interrupted first (corgi agent interrupt). It asks before it
acts unless --yes.

  corgi agent undo api·APP-412
  corgi agent undo api·APP-412 --worktree --yes`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		worktree, _ := cmd.Flags().GetBool("worktree")
		yes, _ := cmd.Flags().GetBool("yes")
		dir := mustAgentDir()
		board, err := readBoard(dir)
		if err != nil {
			exitWithError("agent_undo", err, 1)
		}
		s, err := findBoardSession(board.State, args[0])
		if err != nil {
			if ended, ok := endedSession(board.State, args[0]); ok {
				s = ended
			} else {
				exitWithError("agent_undo", err, 1)
			}
		}
		plan, err := sessionUndoPlan(s)
		if err != nil {
			exitWithError("agent_undo", err, 2)
		}
		what := fmt.Sprintf("%d uncommitted change(s) in %s", len(plan.Changed), plan.Dir)
		if worktree {
			what += fmt.Sprintf(", then the worktree and branch %s", plan.Branch)
		}
		if len(plan.Changed) == 0 && !worktree {
			utils.Infof("%s has nothing uncommitted in %s\n", firstNonEmpty(s.Display, s.Label), plan.Dir)
			return
		}
		if !yes && !utils.JSONOutput {
			for _, f := range plan.Changed {
				utils.Infof("  %s\n", f)
			}
			fmt.Printf("drop %s? [y/N] ", what)
			line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
				utils.Info("left as it was")
				return
			}
		}
		if err := runSessionUndo(plan, worktree); err != nil {
			exitWithError("agent_undo", err, 1)
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"session": s.ID, "dropped": plan.Changed, "worktree": worktree, "branch": plan.Branch})
			return
		}
		utils.Infof("dropped %s\n", what)
	},
}

func launchUndoHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "POST {session, worktree} to drop a session's uncommitted work")
		return
	}
	var req struct {
		Session  string `json:"session"`
		Worktree bool   `json:"worktree"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeLaunchError(w, http.StatusBadRequest, "could not read the request")
		return
	}
	s, code, msg := launchSessionFor(req.Session)
	if code != 0 {
		writeLaunchError(w, code, msg)
		return
	}
	plan, err := sessionUndoPlan(s)
	if err != nil {
		writeLaunchError(w, http.StatusConflict, err.Error())
		return
	}
	if err := runSessionUndo(plan, req.Worktree); err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeLaunchJSON(w, map[string]any{"session": s.ID, "dropped": plan.Changed, "worktree": req.Worktree, "branch": plan.Branch})
}

func endedSession(st sessions.State, ref string) (sessions.Session, bool) {
	for _, e := range st.Ended {
		if e.ID == ref || e.Display == ref || strings.HasPrefix(e.ID, ref) {
			return e, true
		}
	}
	return sessions.Session{}, false
}

func init() {
	agentUndoCmd.Flags().Bool("worktree", false, "Remove the worktree and delete its branch as well")
	agentUndoCmd.Flags().Bool("yes", false, "Do not ask")
	agentCmd.AddCommand(agentUndoCmd)
}
