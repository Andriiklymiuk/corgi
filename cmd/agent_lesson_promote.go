package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/lessons"
	"andriiklymiuk/corgi/utils/gitbase"
	"github.com/spf13/cobra"
)

func promoteLesson(agentDir, workspace, root string, n int) (string, error) {
	list := lessons.List(agentDir, workspace)
	if n < 1 || n > len(list) {
		return "", fmt.Errorf("%s has %d lessons; pick 1 to %d (corgi agent lesson list)", workspace, len(list), len(list))
	}
	line := "- " + strings.TrimSpace(list[n-1].Text)
	path := filepath.Join(root, "CLAUDE.md")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	text := string(data)
	if strings.Contains(text, line+"\n") || strings.HasSuffix(text, line) {
		return line, nil
	}
	const heading = "## Lessons"
	switch {
	case text == "":
		text = "# " + workspace + "\n\n" + heading + "\n\n" + line + "\n"
	case strings.Contains(text, heading+"\n"):
		i := strings.Index(text, heading+"\n") + len(heading) + 1
		rest := text[i:]
		end := len(rest)
		if j := strings.Index(rest, "\n## "); j >= 0 {
			end = j + 1
		}
		section := strings.TrimRight(rest[:end], "\n")
		if section == "" {
			section = "\n" + line
		} else {
			section += "\n" + line
		}
		text = text[:i] + section + "\n" + rest[end:]
	default:
		text = strings.TrimRight(text, "\n") + "\n\n" + heading + "\n\n" + line + "\n"
	}
	return line, os.WriteFile(path, []byte(text), 0o644)
}

func promoteOnBranch(agentDir, workspace, root string, n int) (string, error) {
	tmp, err := os.MkdirTemp("", "corgi-lesson-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	branch := "corgi/lesson-" + time.Now().Format("0102-1504")
	tree := filepath.Join(tmp, "tree")
	base := "HEAD"
	for _, ref := range gitbase.Refs(root) {
		if err := exec.Command("git", "-C", root, "rev-parse", "--verify", "--quiet", ref).Run(); err == nil {
			base = ref
			break
		}
	}
	if out, err := exec.Command("git", "-C", root, "worktree", "add", "-q", "-b", branch, tree, base).CombinedOutput(); err != nil {
		return "", fmt.Errorf("git worktree add: %v\n%s", err, out)
	}
	defer exec.Command("git", "-C", root, "worktree", "remove", "--force", tree).Run()
	line, err := promoteLesson(agentDir, workspace, tree, n)
	if err != nil {
		return "", err
	}
	if out, err := exec.Command("git", "-C", tree, "add", "CLAUDE.md").CombinedOutput(); err != nil {
		return "", fmt.Errorf("git add: %v\n%s", err, out)
	}
	msg := "CLAUDE.md: " + strings.TrimPrefix(line, "- ")
	if out, err := exec.Command("git", "-C", tree, "commit", "-q", "-m", msg).CombinedOutput(); err != nil {
		return "", fmt.Errorf("git commit: %v\n%s", err, out)
	}
	if out, err := exec.Command("git", "-C", tree, "push", "-q", "-u", "origin", branch).CombinedOutput(); err != nil {
		return "", fmt.Errorf("git push: %v\n%s", err, out)
	}
	if _, err := exec.LookPath("gh"); err == nil {
		out, err := exec.Command("gh", "pr", "create", "--fill", "--head", branch).CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("gh pr create: %v\n%s (the branch %s is pushed)", err, out, branch)
		}
		return strings.TrimSpace(string(out)), nil
	}
	return "pushed " + branch + " - open the pull request by hand", nil
}

var agentLessonPromoteCmd = &cobra.Command{
	Use:   "promote <n> [--pr]",
	Short: "Write lesson n into the workspace's CLAUDE.md, where every session reads it",
	Long: `Takes lesson n from corgi agent lesson list and adds it as a line under a
"## Lessons" heading in the workspace's CLAUDE.md - once. Without --pr the
file in your checkout is edited and left for you to commit. With --pr the
line goes on a corgi/lesson-… branch in a worktree of its own, is pushed,
and becomes a pull request (gh); your checkout is not touched.

  corgi agent lesson list
  corgi agent lesson promote 2
  corgi agent lesson promote 2 --pr`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		n, err := strconv.Atoi(args[0])
		if err != nil {
			exitWithError(utils.ErrUsage, fmt.Errorf("which lesson? a number from corgi agent lesson list"), 2)
		}
		pr, _ := cmd.Flags().GetBool("pr")
		dir := mustAgentDir()
		ws, _ := cmd.Flags().GetString("workspace")
		if ws == "" {
			id, err := currentWorkspaceID(dir)
			if err != nil {
				exitWithError("agent_lesson", err, 1)
			}
			ws = id
		}
		root, err := workspaceRoot(ws)
		if err != nil {
			exitWithError("agent_lesson", err, 1)
		}
		if pr {
			out, err := promoteOnBranch(dir, ws, root, n)
			if err != nil {
				exitWithError("agent_lesson", err, 1)
			}
			if utils.JSONOutput {
				utils.PrintJSON(map[string]any{"workspace": ws, "pr": out})
				return
			}
			fmt.Println(out)
			return
		}
		line, err := promoteLesson(dir, ws, root, n)
		if err != nil {
			exitWithError("agent_lesson", err, 1)
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"workspace": ws, "path": filepath.Join(root, "CLAUDE.md"), "line": line})
			return
		}
		utils.Infof("%s → %s (commit it when you are happy)\n", line, filepath.Join(root, "CLAUDE.md"))
	},
}

func init() {
	agentLessonPromoteCmd.Flags().Bool("pr", false, "On a branch of its own, pushed, as a pull request")
	agentLessonPromoteCmd.Flags().String("workspace", "", "The workspace (default: the one this directory is in)")
	agentLessonCmd.AddCommand(agentLessonPromoteCmd)
}
