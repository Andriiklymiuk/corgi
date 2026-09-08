package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/usage"
)

var agentStandupCmd = &cobra.Command{
	Use:   "standup",
	Short: "What you and Claude did, per workspace, since yesterday",
	Long: `Reads what every account's Claude Code was asked (its prompt history) and
what landed in git under each registered workspace, and prints it grouped by
workspace: the prompts as headlines, the commits as facts. Nothing leaves the
machine unless --write is given, which hands the raw list to ` + "`claude -p`" + `
for three plain sentences you can paste into a standup.

  corgi agent standup              # since 24h ago
  corgi agent standup --since 48h  # a long weekend
  corgi agent standup --write      # summarised by Claude`,
	Run: runAgentStandup,
}

type standupEntry struct {
	Workspace string    `json:"workspace"`
	Dir       string    `json:"dir"`
	Prompts   []string  `json:"prompts,omitempty"`
	Commits   []string  `json:"commits,omitempty"`
	First     time.Time `json:"first,omitempty"`
}

func runAgentStandup(cmd *cobra.Command, _ []string) {
	sinceFlag, _ := cmd.Flags().GetString("since")
	write, _ := cmd.Flags().GetBool("write")
	dur, err := time.ParseDuration(sinceFlag)
	if err != nil {
		exitWithError("agent_standup", fmt.Errorf("--since takes a duration like 24h, not %q", sinceFlag), 2)
	}
	since := time.Now().Add(-dur)
	entries := collectStandup(since)
	if utils.JSONOutput && !write {
		utils.PrintJSON(map[string]any{"since": since, "workspaces": entries})
		return
	}
	text := formatStandup(entries, since)
	if !write {
		fmt.Print(text)
		return
	}
	summary, err := summarizeWithClaude(context.Background(), text)
	if err != nil {
		exitWithError("agent_standup", err, 1)
	}
	if utils.JSONOutput {
		utils.PrintJSON(map[string]any{"since": since, "summary": summary})
		return
	}
	fmt.Println(summary)
}

// collectStandup groups prompts and commits by registered workspace. Prompts
// under a directory no workspace covers are grouped by that directory.
func collectStandup(since time.Time) []standupEntry {
	byDir := map[string]*standupEntry{}
	entryFor := func(dir string) *standupEntry {
		root, id := workspaceRootFor(dir)
		key := root
		if key == "" {
			key, id = dir, filepath.Base(dir)
		}
		e := byDir[key]
		if e == nil {
			e = &standupEntry{Workspace: id, Dir: key}
			byDir[key] = e
		}
		return e
	}
	for _, configDir := range historyConfigDirs() {
		for _, p := range readPromptHistory(filepath.Join(configDir, "history.jsonl"), since) {
			e := entryFor(p.Project)
			e.Prompts = append(e.Prompts, p.Display)
			if e.First.IsZero() || p.At.Before(e.First) {
				e.First = p.At
			}
		}
	}
	if registry, _, err := agentRegistry(); err == nil {
		for _, ws := range registry.Sorted() {
			commits := gitCommitsSince(ws.AbsPath, since)
			if len(commits) == 0 {
				continue
			}
			e := entryFor(ws.AbsPath)
			e.Commits = commits
		}
	}
	var out []standupEntry
	for _, e := range byDir {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Workspace < out[j].Workspace })
	return out
}

// workspaceRootFor is the deepest registered workspace containing dir.
func workspaceRootFor(dir string) (root, id string) {
	registry, _, err := agentRegistry()
	if err != nil {
		return "", ""
	}
	dir = cleanPath(dir)
	for _, ws := range registry.Sorted() {
		r := cleanPath(ws.AbsPath)
		if r != "" && (dir == r || strings.HasPrefix(dir, r+string(filepath.Separator))) && len(r) > len(root) {
			root, id = r, ws.ID
		}
	}
	return root, id
}

// historyConfigDirs is every Claude config dir with a prompt history: the
// default one and each profile's.
func historyConfigDirs() []string {
	home, _ := os.UserHomeDir()
	dirs := []string{filepath.Join(home, ".claude")}
	if agentD, err := agentDir(); err == nil {
		if profiles, err := loadProfiles(agentD); err == nil {
			for _, name := range sortedProfileNames(profiles) {
				if cfg := strings.TrimSpace(profiles[name].ConfigDir); cfg != "" {
					dirs = append(dirs, expandTilde(cfg))
				}
			}
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, d := range dirs {
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return out
}

type promptRecord struct {
	Display string
	Project string
	At      time.Time
}

// readPromptHistory reads Claude Code's history.jsonl: one line per prompt,
// with the project directory and a millisecond timestamp.
func readPromptHistory(path string, since time.Time) []promptRecord {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []promptRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for sc.Scan() {
		var row struct {
			Display   string `json:"display"`
			Project   string `json:"project"`
			Timestamp int64  `json:"timestamp"`
		}
		if json.Unmarshal(sc.Bytes(), &row) != nil || row.Project == "" {
			continue
		}
		at := time.UnixMilli(row.Timestamp)
		if at.Before(since) {
			continue
		}
		display := strings.TrimSpace(strings.SplitN(row.Display, "\n", 2)[0])
		if display == "" || strings.HasPrefix(display, "/") {
			// Slash commands are housekeeping, not work.
			continue
		}
		if r := []rune(display); len(r) > 100 {
			display = string(r[:99]) + "…"
		}
		out = append(out, promptRecord{Display: display, Project: row.Project, At: at})
	}
	return out
}

func gitCommitsSince(dir string, since time.Time) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "log", "--since="+since.Format(time.RFC3339), "--pretty=format:%s", "--no-merges").Output()
	if err != nil {
		return nil
	}
	var commits []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			commits = append(commits, line)
		}
	}
	return commits
}

func formatStandup(entries []standupEntry, since time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Since %s\n", since.Local().Format("Mon 2 Jan 15:04"))
	if len(entries) == 0 {
		b.WriteString("  nothing on record — no prompts, no commits\n")
		return b.String()
	}
	for _, e := range entries {
		fmt.Fprintf(&b, "\n%s  (%s)\n", e.Workspace, e.Dir)
		if len(e.Commits) > 0 {
			b.WriteString("  committed:\n")
			for _, c := range e.Commits {
				fmt.Fprintf(&b, "    - %s\n", c)
			}
		}
		if len(e.Prompts) > 0 {
			fmt.Fprintf(&b, "  asked Claude (%d prompts):\n", len(e.Prompts))
			for _, p := range e.Prompts {
				fmt.Fprintf(&b, "    · %s\n", p)
			}
		}
	}
	return b.String()
}

// summarizeWithClaude runs the list through a headless claude. The prompt
// asks for prose, not a table; the output is what gets pasted somewhere.
func summarizeWithClaude(ctx context.Context, text string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	prompt := "Below is a list of what I asked an AI coding agent and what got committed, per project. " +
		"Write my standup update: three to five plain sentences, past tense, grouped by project, no headings, no bullet points, no preamble. " +
		"Name concrete outcomes, skip the housekeeping.\n\n" + text
	cmd := exec.CommandContext(ctx, "claude", "-p", prompt)
	cmd.Stdin = nil
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("claude -p: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

var _ = usage.Today

func init() {
	agentStandupCmd.Flags().String("since", "24h", "How far back to look")
	agentStandupCmd.Flags().Bool("write", false, "Have claude -p turn the list into a few sentences")
	agentCmd.AddCommand(agentStandupCmd)
}
