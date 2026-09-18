package cmd

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/transcript"
	"andriiklymiuk/corgi/utils/agent/watch"
	"github.com/spf13/cobra"
)

func prBodyFor(ctx context.Context, s sessions.Session) (string, error) {
	dir := diffDirFor(s)
	if dir == "" {
		return "", fmt.Errorf("%s has no checkout on record", firstNonEmpty(s.Display, s.Label))
	}
	base := diffBase(ctx, dir)
	if base == "" {
		return "", fmt.Errorf("no main branch to diff against in %s", dir)
	}
	files, err := diffFiles(ctx, dir, base)
	if err != nil {
		return "", fmt.Errorf("git could not diff the branch")
	}
	var b strings.Builder
	b.WriteString("## What\n\n")
	asked := firstPromptOf(s)
	switch {
	case asked != "" && s.Ticket != "":
		b.WriteString(s.Ticket + " — " + asked + "\n")
	case asked != "":
		b.WriteString(asked + "\n")
	case s.Ticket != "":
		b.WriteString(s.Ticket + "\n")
	default:
		b.WriteString(firstNonEmpty(s.Summary, "See the changes below.") + "\n")
	}
	if out, err := exec.CommandContext(ctx, "git", "-C", dir, "log", "--format=%s", base+"..HEAD").Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) > 0 && lines[0] != "" {
			b.WriteString("\n## Commits\n\n")
			for i, l := range lines {
				if i == 12 {
					b.WriteString(fmt.Sprintf("- … and %d more\n", len(lines)-12))
					break
				}
				b.WriteString("- " + l + "\n")
			}
		}
	}
	if len(files) > 0 {
		b.WriteString("\n## Changes\n\n")
		added, deleted, generated := 0, 0, 0
		for i, f := range files {
			if f.Generated {
				generated++
				continue
			}
			added += f.Added
			deleted += f.Deleted
			if i < 30 {
				b.WriteString(fmt.Sprintf("- `%s` +%d −%d\n", f.Path, f.Added, f.Deleted))
			}
		}
		if len(files) > 30 {
			b.WriteString(fmt.Sprintf("- … %d more files\n", len(files)-30))
		}
		line := fmt.Sprintf("\n%d files, +%d −%d", len(files)-generated, added, deleted)
		if generated > 0 {
			line += fmt.Sprintf(" (%d generated left out)", generated)
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n## Tests\n\n")
	switch {
	case s.Gate != nil && s.Gate.Cmd != "":
		b.WriteString(fmt.Sprintf("`%s` %s%s\n", s.Gate.Cmd, mark(s.Gate.OK), sinceWord(s.Gate.At)))
	case s.Tests != nil:
		b.WriteString(fmt.Sprintf("`%s` %s%s\n", s.Tests.Cmd, mark(s.Tests.OK), sinceWord(s.Tests.At)))
	default:
		b.WriteString("No test run recorded for this session.\n")
	}
	b.WriteString("\n<sub>Written by corgi from the session that made this branch.</sub>\n")
	return transcript.Scrub(b.String()), nil
}

func mark(ok bool) string {
	if ok {
		return "✓"
	}
	return "✗"
}

func sinceWord(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return " · " + roughAge(time.Since(at)) + " ago"
}

func firstPromptOf(s sessions.Session) string {
	path := transcriptPathFor(s)
	if path == "" || !transcript.Exists(path) {
		return ""
	}
	entries, _, err := transcript.Read(path, 0, 40)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.Kind == "user" && strings.TrimSpace(e.Text) != "" {
			t := strings.TrimSpace(e.Text)
			if len(t) > 300 {
				t = t[:300] + "…"
			}
			return strings.ReplaceAll(t, "\n", " ")
		}
	}
	return ""
}

var agentPRBodyCmd = &cobra.Command{
	Use:   "pr-body <session> [--apply]",
	Short: "A pull request description from the session that made the branch",
	Long: `Writes the pull request body the way a person would after reading the
session: what was asked (the ticket and the first prompt), the commits, the
files with their counts, and the last test run. Secrets are scrubbed the way
the transcript is. --apply writes it onto the pull request the session
opened (the forge token the watch uses).

  corgi agent pr-body api·APP-412
  corgi agent pr-body api·APP-412 --apply`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		apply, _ := cmd.Flags().GetBool("apply")
		session, _, msg := launchSessionFor(args[0])
		if msg != "" {
			exitWithError("agent_pr_body", fmt.Errorf("%s", msg), 2)
		}
		ctx, cancel := context.WithTimeout(context.Background(), diffTimeout)
		defer cancel()
		body, err := prBodyFor(ctx, session)
		if err != nil {
			exitWithError("agent_pr_body", err, 1)
		}
		if apply {
			if session.PR == "" {
				exitWithError("agent_pr_body", fmt.Errorf("%s has no pull request on record", firstNonEmpty(session.Display, session.Label)), 2)
			}
			secrets := watch.LoadSecretsFor(mustAgentDir(), session.Label)
			if err := watch.SetPullBody(ctx, secrets, session.PR, body); err != nil {
				exitWithError("agent_pr_body", err, 1)
			}
			utils.Infof("wrote the body onto %s\n", session.PR)
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"session": session.ID, "pr": session.PR, "body": body, "applied": apply})
			return
		}
		if !apply {
			fmt.Print(body)
		}
	},
}

func init() {
	agentPRBodyCmd.Flags().Bool("apply", false, "Write the body onto the session's pull request")
	agentCmd.AddCommand(agentPRBodyCmd)
}
