package cmd

import (
	"context"
	"encoding/json"
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

Answer one from here too:

  corgi agent reviews send <url> --verdict approve
  corgi agent reviews merge <url>
  corgi agent reviews fix <url> --notes '[...]'      hand notes to the session on that branch
  corgi agent reviews fix-all <url>                  hand every open comment, failed check and conflict to it
  corgi agent reviews phone on "<device>"            let a phone review while this Mac is off

The phone's and the Mac app's Reviews read the same from /launch/reviews.`,
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
	for _, o := range c.Open {
		where := "on the pull request"
		if o.Path != "" {
			where = fmt.Sprintf("%s:%d", o.Path, o.Line)
		}
		fmt.Fprintf(&b, "  open: %s, %s: %s\n", o.Author, where, firstLineOf(o.Body))
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

func runReviewCommand(cmd *cobra.Command, do, link string) {
	dir := mustAgentDir()
	req := reviewRequest{Link: link, Do: do}
	if f := cmd.Flags(); f.Lookup("body") != nil {
		req.Body, _ = f.GetString("body")
		raw, _ := f.GetString("notes")
		if strings.TrimSpace(raw) != "" {
			if err := json.Unmarshal([]byte(raw), &req.Notes); err != nil {
				exitWithError("agent_reviews", fmt.Errorf("--notes is a JSON list of {path, line, side, body}: %w", err), 2)
			}
		}
	}
	if f := cmd.Flags(); f.Lookup("verdict") != nil {
		req.Verdict, _ = f.GetString("verdict")
		req.Head, _ = f.GetString("head")
	}
	if f := cmd.Flags(); f.Lookup("thread") != nil {
		req.Thread, _ = f.GetString("thread")
		if f.Lookup("note") != nil {
			req.Note, _ = f.GetString("note")
			req.Body, _ = f.GetString("body")
		}
	}
	from, _ := cmd.Flags().GetString("from")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, _, why := runReviewAction(ctx, dir, req, firstNonEmpty(from, "cli"))
	if why != "" {
		exitWithError("agent_reviews", fmt.Errorf("%s", why), 1)
	}
	if utils.JSONOutput {
		utils.PrintJSON(out)
		return
	}
	fmt.Println(firstNonEmpty(fmt.Sprint(out["done"]), "sent"))
}

var agentReviewsSendCmd = &cobra.Command{
	Use:   "send <url>",
	Short: "Post a review under your name: approve, ask for changes, or comment, with notes on lines",
	Long: `  corgi agent reviews send https://github.com/acme/api/pull/42 --verdict approve
  corgi agent reviews send <url> --verdict request --notes '[{"path":"api/auth.go","line":41,"body":"keep the guard"}]'
  corgi agent reviews send <url> --verdict comment --body "one question" --head 4f2a91c

--head is the commit you read. When the pull request got commits since, nothing
is posted.`,
	Args: cobra.ExactArgs(1),
	Run:  func(cmd *cobra.Command, args []string) { runReviewCommand(cmd, "send", args[0]) },
}

var agentReviewsMergeCmd = &cobra.Command{
	Use:   "merge <url>",
	Short: "Merge a pull request of yours",
	Args:  cobra.ExactArgs(1),
	Run:   func(cmd *cobra.Command, args []string) { runReviewCommand(cmd, "merge", args[0]) },
}

var agentReviewsFixCmd = &cobra.Command{
	Use:   "fix <url>",
	Short: "Hand your notes to the live session on that pull request, as its next prompt",
	Args:  cobra.ExactArgs(1),
	Run:   func(cmd *cobra.Command, args []string) { runReviewCommand(cmd, "fix", args[0]) },
}

var agentReviewsFixAllCmd = &cobra.Command{
	Use:   "fix-all <url>",
	Short: "Hand everything open on a pull request of yours to an agent: comments, failed checks, conflicts",
	Long: `Goes to the live session on that pull request, or opens a new one in an editor
window on its workspace when none is. A comment counts while its thread is
unresolved and the last word is not yours.

  corgi agent reviews fix-all https://github.com/acme/api/pull/42`,
	Args: cobra.ExactArgs(1),
	Run:  func(cmd *cobra.Command, args []string) { runReviewCommand(cmd, "fix-all", args[0]) },
}

var agentReviewsReplyCmd = &cobra.Command{
	Use:   "reply <url>",
	Short: "Answer a comment thread on a pull request under your name, or leave a plain comment",
	Long: `The thread and note ids are in corgi agent reviews <url> --json, under threads.

  corgi agent reviews reply <url> --thread PRRT_kw --note 2148 --body "done in 4f2a91c"
  corgi agent reviews reply <url> --body "thanks, merging after the build"`,
	Args: cobra.ExactArgs(1),
	Run:  func(cmd *cobra.Command, args []string) { runReviewCommand(cmd, "reply", args[0]) },
}

var agentReviewsResolveCmd = &cobra.Command{
	Use:   "resolve <url>",
	Short: "Resolve one comment thread on a pull request",
	Args:  cobra.ExactArgs(1),
	Run:   func(cmd *cobra.Command, args []string) { runReviewCommand(cmd, "resolve", args[0]) },
}

func init() {
	agentReviewsReplyCmd.Flags().String("body", "", "The answer")
	agentReviewsReplyCmd.Flags().String("note", "", "The first comment of the thread, on GitHub")
	for _, c := range []*cobra.Command{agentReviewsReplyCmd, agentReviewsResolveCmd} {
		c.Flags().String("thread", "", "The thread to answer or resolve")
		c.Flags().String("from", "", "Which app sends it")
		_ = c.Flags().MarkHidden("from")
		agentReviewsCmd.AddCommand(c)
	}
	for _, c := range []*cobra.Command{agentReviewsSendCmd, agentReviewsFixCmd} {
		c.Flags().String("body", "", "Words for the whole review")
		c.Flags().String("notes", "", `Notes on lines, as JSON: [{"path":"a.go","line":12,"side":"+","body":"..."}]`)
	}
	agentReviewsSendCmd.Flags().String("verdict", "comment", "approve, request or comment")
	agentReviewsSendCmd.Flags().String("head", "", "The commit you read; refuses when the pull request moved on")
	for _, c := range []*cobra.Command{agentReviewsSendCmd, agentReviewsFixCmd, agentReviewsFixAllCmd, agentReviewsMergeCmd} {
		c.Flags().String("from", "", "Which app sends it")
		_ = c.Flags().MarkHidden("from")
		agentReviewsCmd.AddCommand(c)
	}
	agentCmd.AddCommand(agentReviewsCmd)
}
