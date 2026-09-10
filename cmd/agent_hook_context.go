package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils/agent/brief"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/workspace"
)

// The SessionStart context hook: the one synchronous hook, and the one that
// talks back. Everything the daemon already knows and a fresh session does
// not — who else is working in this repo, how much budget is left, what the
// last session here was doing, whether the workspace has a memory — goes in
// as a few lines of additionalContext. Nothing is read that is not already
// on disk, so the hook costs one file read per line and never blocks long:
// Claude Code gives it five seconds and drops it after that.

type contextHookOutput struct {
	HookSpecificOutput contextHookSpecific `json:"hookSpecificOutput"`
}

type contextHookSpecific struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext"`
}

type contextHookInput struct {
	SessionID string `json:"session_id"`
	Event     string `json:"hook_event_name"`
	Cwd       string `json:"cwd"`
	Source    string `json:"source"`
}

func runContextHook(stdin io.Reader, stdout io.Writer, getenv func(string) string, now time.Time) {
	if stdin == nil {
		return
	}
	data, err := io.ReadAll(io.LimitReader(stdin, 64<<10))
	if err != nil {
		return
	}
	var in contextHookInput
	if json.Unmarshal(data, &in) != nil || in.Cwd == "" || in.Source == "compact" {
		return
	}
	dir := agentDirOrEmpty()
	if dir == "" {
		return
	}
	text := sessionContext(dir, in, getenv("CLAUDE_CONFIG_DIR"), now)
	if text == "" {
		return
	}
	_ = json.NewEncoder(stdout).Encode(contextHookOutput{HookSpecificOutput: contextHookSpecific{
		HookEventName: "SessionStart", AdditionalContext: text,
	}})
}

// sessionContext builds the lines. Empty when nothing is worth saying.
func sessionContext(dir string, in contextHookInput, configDir string, now time.Time) string {
	registry, _ := workspace.Load(agentRegistryPath(dir))
	wsID, root := workspaceLabel(registry, in.Cwd)
	if root == "" {
		root = sessions.RepoRoot(in.Cwd)
	}
	var lines []string

	head := "corgi · " + wsID
	if b := sessions.Branch(in.Cwd); b != "" {
		head += " on " + b
	}

	if rep, err := readBoard(dir); err == nil {
		if others := otherSessionsHere(rep.State, in.SessionID, root, now); others != "" {
			lines = append(lines, others)
		}
		if budget := budgetLine(rep.State, configDir, now); budget != "" {
			lines = append(lines, budget)
		}
	}
	if b, err := brief.Read(dir, wsID); err == nil && b != nil && !b.Empty() && in.Source != "resume" {
		lines = append(lines, fmt.Sprintf("last session here ended %s ago: %s", roughAge(now.Sub(b.EndedAt)), b.Summary()))
	}
	if path, facts := memoryIndex(in.Cwd, root); facts > 0 {
		lines = append(lines, fmt.Sprintf("workspace memory: %d facts in %s — read it before changing code", facts, path))
	}
	if len(lines) == 0 {
		return ""
	}
	return head + "\n" + strings.Join(lines, "\n")
}

// otherSessionsHere names the live sessions in the same workspace, so two
// chats do not edit one file without knowing of each other.
func otherSessionsHere(st sessions.State, self, root string, now time.Time) string {
	var parts []string
	for _, s := range st.Sessions {
		if s.ID == self || s.Status == sessions.StatusGone || s.Status == sessions.StatusUnknown {
			continue
		}
		if root == "" || !(samePath(s.Folder, root) || sessions.Within(s.Cwd, root)) {
			continue
		}
		name := s.Title
		if name == "" {
			name = firstNonEmpty(s.Display, s.Label)
		}
		part := fmt.Sprintf("%s %q %s", statusGlyph(s.Status), name, strings.ToLower(statusWord(s.Status)))
		if s.Branch != "" {
			part += " on " + s.Branch
		}
		if s.Detail != "" && s.Status != sessions.StatusDone {
			part += " (" + s.Detail + ")"
		}
		if !s.StatusSince.IsZero() {
			part += " " + roughAge(now.Sub(s.StatusSince))
		}
		parts = append(parts, part)
		if len(parts) == 4 {
			break
		}
	}
	if len(parts) == 0 {
		return ""
	}
	label := "other sessions in this workspace"
	if len(parts) == 1 {
		label = "another session in this workspace"
	}
	return label + ": " + strings.Join(parts, " · ")
}

// budgetLine is the account's two limits and, when the pace says so, the
// warning that the five-hour window runs out before it resets.
func budgetLine(st sessions.State, configDir string, now time.Time) string {
	for _, a := range st.Accounts {
		if a.ConfigDir != configDir || a.Limits == nil {
			continue
		}
		l := a.Limits
		line := fmt.Sprintf("budget: 5h %d%%", l.FiveHour.Percent)
		if !l.FiveHour.ResetsAt.IsZero() {
			line += " (resets " + clock(l.FiveHour.ResetsAt) + ")"
		}
		line += fmt.Sprintf(" · week %d%%", l.SevenDay.Percent)
		if !l.SevenDay.ResetsAt.IsZero() {
			line += " (resets " + weekday(l.SevenDay.ResetsAt) + ")"
		}
		if l.FiveHour.Percent >= 100 || l.SevenDay.Percent >= 100 {
			line += " — limit reached, keep this turn short or `corgi agent carry`"
		} else if a.Forecast != nil && a.Forecast.FiveHour != nil && !a.Forecast.FiveHour.Safe && !a.Forecast.FiveHour.ExhaustAt.IsZero() {
			line += " — at this pace the 5h window runs out at " + clock(a.Forecast.FiveHour.ExhaustAt)
		} else if l.FiveHour.Percent >= 85 {
			line += " — nearly out, prefer small turns"
		}
		return line
	}
	return ""
}

func clock(t time.Time) string   { return t.Local().Format("15:04") }
func weekday(t time.Time) string { return t.Local().Format("Mon 15:04") }
func roughAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// memoryIndex finds .corgi/memory/index.md between cwd and the workspace
// root and counts its facts (the "- " lines).
func memoryIndex(cwd, root string) (string, int) {
	dir := cwd
	for i := 0; i < 16 && dir != ""; i++ {
		path := filepath.Join(dir, ".corgi", "memory", "index.md")
		if f, err := os.Open(path); err == nil {
			n := 0
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				if strings.HasPrefix(strings.TrimSpace(sc.Text()), "- ") {
					n++
				}
			}
			f.Close()
			rel, err := filepath.Rel(cwd, path)
			if err != nil || strings.HasPrefix(rel, "..") {
				rel = path
			}
			return rel, n
		}
		if root != "" && samePath(dir, root) {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", 0
}
