package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/watch"
	"github.com/spf13/cobra"
)

var agentReviewsCmd = &cobra.Command{
	Use:   "reviews [url]",
	Short: "Your pull requests and the ones waiting for your review; with a link, only the lines of it that need a person",
	Long: `Lists the pull requests you opened (ready to merge, open, draft) and the ones somebody asked you to review, from GitHub and GitLab, with
their checks and review state. Only repos a watched workspace has are
listed: the ones in its corgi-compose.yml, or its watch --repos list. A
workspace with neither takes every repo its token can see. Merged ones are
left out.

  corgi agent reviews
  corgi agent reviews https://github.com/acme/api/pull/42
  corgi agent reviews https://github.com/acme/api/pull/42 --json

With a link it reads the change and sorts it: tests, docs, lock files,
generated code, moved and removed files are folded away, and what is left is
listed with the reason it needs eyes (changes the database, who may do what,
money, build or deploy, dependencies) and what the lines themselves show (a
guard removed, a check switched off, a test skipped, a TODO left). No model
reads it: the sorting is rules, so it costs no tokens.

The phone's Reviews tab reads the same from /launch/reviews.`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if len(args) == 1 {
			change, err := reviewChangeFor(ctx, dir, strings.TrimSpace(args[0]), true)
			if err != nil {
				exitWithError("agent_reviews", err, 1)
			}
			if utils.JSONOutput {
				utils.PrintJSON(change)
				return
			}
			fmt.Print(reviewChangeText(change))
			return
		}
		rows, problem := reviewRows(ctx, dir, true)
		if utils.JSONOutput {
			if rows == nil {
				rows = []watch.ReviewRow{}
			}
			utils.PrintJSON(map[string]any{"reviews": rows, "problem": problem})
			return
		}
		if problem != "" && len(rows) == 0 {
			exitWithError("agent_reviews", fmt.Errorf("%s", problem), 1)
		}
		fmt.Print(reviewRowsText(rows))
	},
}

var reviewGroupTitles = []struct{ key, title string }{
	{watch.GroupToReview, "Waiting for your review"},
	{watch.GroupReady, "Ready to merge"},
	{watch.GroupOpen, "Open"},
	{watch.GroupDraft, "Draft"},
	{watch.GroupAnswered, "Approved by you, still open"},
	{watch.GroupOld, "Quiet for two months"},
}

func reviewRowsText(rows []watch.ReviewRow) string {
	if len(rows) == 0 {
		return "no pull request of yours is open, and nobody waits for your review\n"
	}
	var b strings.Builder
	for _, group := range reviewGroupTitles {
		first := true
		for _, r := range rows {
			if r.Group != group.key {
				continue
			}
			if first {
				fmt.Fprintf(&b, "%s\n", group.title)
				first = false
			}
			facts := strings.Join(reviewFacts(r.Checks, r.Review, r.Session), " · ")
			fmt.Fprintf(&b, "  %-28s %s\n", r.Ref, r.Title)
			facts = strings.Join(reviewFacts(r.Workspace, facts), " · ")
			if facts != "" {
				fmt.Fprintf(&b, "  %-28s %s\n", "", facts)
			}
		}
	}
	return b.String()
}

func reviewFacts(values ...string) []string {
	var out []string
	for _, v := range values {
		if v != "" && v != "none" {
			out = append(out, v)
		}
	}
	return out
}

func reviewChangeText(c reviewChange) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s\n", c.Ref, c.Title)
	fmt.Fprintf(&b, "%d of %d changed lines need a person\n", c.Eyes, c.Total)
	if facts := strings.Join(reviewFacts(c.Checks, c.Review), " · "); facts != "" {
		fmt.Fprintf(&b, "%s\n", facts)
	}
	for _, f := range c.Files {
		if !f.Eyes {
			continue
		}
		fmt.Fprintf(&b, "\n  %s  +%d -%d", f.Path, f.Adds, f.Dels)
		if f.Why != "" {
			fmt.Fprintf(&b, "  (%s)", f.Why)
		}
		b.WriteString("\n")
		for _, h := range f.Hunks {
			for _, flag := range h.Flags {
				fmt.Fprintf(&b, "    ! %s", flag)
				if h.Header != "" {
					fmt.Fprintf(&b, " in %s", h.Header)
				}
				b.WriteString("\n")
			}
		}
	}
	if len(c.Skipped) > 0 {
		b.WriteString("\nfolded away:")
		for _, s := range c.Skipped {
			fmt.Fprintf(&b, " %s %d", s.Kind, s.Lines)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func init() {
	agentCmd.AddCommand(agentReviewsCmd)
}
