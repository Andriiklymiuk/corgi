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
	"andriiklymiuk/corgi/utils/agent/watch"
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
	// What the watch did on its own, which no prompt and no local commit
	// records: the unattended runs, what arrived, and what is still waiting.
	Fixes    []standupFix   `json:"fixes,omitempty"`
	Arrived  []standupEvent `json:"arrived,omitempty"`
	Deferred []string       `json:"deferred,omitempty"`
}

type standupFix struct {
	Ref       string    `json:"ref"`
	Kind      string    `json:"kind,omitempty"`
	Title     string    `json:"title,omitempty"`
	URL       string    `json:"url,omitempty"`
	StartedAt time.Time `json:"startedAt"`
	Running   bool      `json:"running,omitempty"`
	Outcome   string    `json:"outcome"`
	PRs       []string  `json:"prs,omitempty"`
}

type standupEvent struct {
	Ref    string    `json:"ref"`
	Source string    `json:"source,omitempty"`
	Kind   string    `json:"kind,omitempty"`
	Title  string    `json:"title,omitempty"`
	URL    string    `json:"url,omitempty"`
	At     time.Time `json:"at"`
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
	addWatchActions(since, byDir, entryFor)
	var out []standupEntry
	for _, e := range byDir {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Workspace < out[j].Workspace })
	return out
}

// arrivedCap keeps a busy tracker from burying the day's own work.
const arrivedCap = 10

// addWatchActions folds in what the watch did while nobody was looking. A
// fix runs headless and its notification is gone in a second, so without
// this a day of unattended pull requests leaves no trace in the standup.
func addWatchActions(since time.Time, byKey map[string]*standupEntry, entryFor func(string) *standupEntry) {
	dir, err := agentDir()
	if err != nil {
		return
	}
	paths := map[string]string{}
	if registry, _, err := agentRegistry(); err == nil {
		for _, ws := range registry.Sorted() {
			paths[ws.ID] = ws.AbsPath
		}
	}
	forWorkspace := func(id string) *standupEntry {
		if id == "" {
			id = "watch"
		}
		if p := paths[id]; p != "" {
			return entryFor(p)
		}
		e := byKey[id]
		if e == nil {
			e = &standupEntry{Workspace: id}
			byKey[id] = e
		}
		return e
	}

	log := watch.LoadFixLog(dir)
	for _, r := range log.FixesSince(since) {
		e := forWorkspace(r.Workspace)
		e.Fixes = append(e.Fixes, standupFix{
			Ref: r.Ref, Kind: r.Kind, Title: r.Title, URL: r.URL,
			StartedAt: r.StartedAt, Running: !r.Done(), Outcome: r.Outcome(), PRs: r.PRs,
		})
	}
	for _, ev := range watch.EventsSince(dir, since) {
		e := forWorkspace(ev.Workspace)
		if len(e.Arrived) >= arrivedCap {
			continue
		}
		e.Arrived = append(e.Arrived, standupEvent{
			Ref: ev.Ref, Source: ev.Source, Kind: string(ev.Kind), Title: ev.Title, URL: ev.URL, At: ev.At,
		})
	}
	for _, ev := range log.DeferredEvents() {
		e := forWorkspace(ev.Workspace)
		e.Deferred = append(e.Deferred, ev.Ref)
	}
	// The logs are newest first; a day reads oldest first.
	for _, e := range byKey {
		sort.Slice(e.Fixes, func(i, j int) bool { return e.Fixes[i].StartedAt.Before(e.Fixes[j].StartedAt) })
		sort.Slice(e.Arrived, func(i, j int) bool { return e.Arrived[i].At.Before(e.Arrived[j].At) })
	}
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
	writeStandupBody(&b, entries)
	return b.String()
}

func writeStandupBody(b *strings.Builder, entries []standupEntry) {
	if len(entries) == 0 {
		b.WriteString("  nothing on record — no prompts, no commits, nothing the watch did\n")
		return
	}
	for _, e := range entries {
		if e.Dir == "" {
			fmt.Fprintf(b, "\n%s\n", e.Workspace)
		} else {
			fmt.Fprintf(b, "\n%s  (%s)\n", e.Workspace, e.Dir)
		}
		if len(e.Commits) > 0 {
			b.WriteString("  committed:\n")
			for _, c := range e.Commits {
				fmt.Fprintf(b, "    - %s\n", c)
			}
		}
		if len(e.Prompts) > 0 {
			fmt.Fprintf(b, "  asked Claude (%d prompts):\n", len(e.Prompts))
			for _, p := range e.Prompts {
				fmt.Fprintf(b, "    · %s\n", p)
			}
		}
		writeWatchActions(b, e)
	}
}

func writeWatchActions(b *strings.Builder, e standupEntry) {
	if len(e.Fixes) > 0 {
		fmt.Fprintf(b, "  corgi worked on %s unattended:\n", plural(len(e.Fixes), "thing", "things"))
		for _, f := range e.Fixes {
			mark := "✓"
			switch {
			case f.Running:
				mark = "…"
			case f.Outcome != "" && len(f.PRs) == 0 && f.Outcome != "nothing opened":
				mark = "✗"
			}
			fmt.Fprintf(b, "    %s %s — %s\n", mark, fixLabel(f), f.Outcome)
			for _, pr := range f.PRs {
				fmt.Fprintf(b, "        %s\n", pr)
			}
		}
	}
	if len(e.Arrived) > 0 {
		fmt.Fprintf(b, "  the watch saw %s:\n", plural(len(e.Arrived), "thing", "things"))
		for _, a := range e.Arrived {
			fmt.Fprintf(b, "    · %s %s  %s\n", a.Source, a.Ref, a.Title)
		}
	}
	if len(e.Deferred) > 0 {
		fmt.Fprintf(b, "  waiting for a free slot: %s\n", strings.Join(e.Deferred, ", "))
	}
}

func fixLabel(f standupFix) string {
	if f.Ref != "" {
		return f.Ref
	}
	if f.Title != "" {
		return f.Title
	}
	return f.Kind
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// summarizeWithClaude runs the list through a headless claude. The prompt
// asks for prose, not a table; the output is what gets pasted somewhere.
func summarizeWithClaude(ctx context.Context, text string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	prompt := "Below is a list of what I asked an AI coding agent, what got committed, and what corgi's watch did unattended, per project. " +
		"Write my standup update: three to five plain sentences, past tense, grouped by project, no headings, no bullet points, no preamble. " +
		"Name concrete outcomes, say plainly when the watch opened a pull request on its own, skip the housekeeping.\n\n" + text
	cmd := exec.CommandContext(ctx, "claude", "-p", prompt)
	cmd.Stdin = nil
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("claude -p: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func init() {
	agentStandupCmd.Flags().String("since", "24h", "How far back to look")
	agentStandupCmd.Flags().Bool("write", false, "Have claude -p turn the list into a few sentences")
	agentCmd.AddCommand(agentStandupCmd)
}
