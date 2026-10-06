package cmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/watch"
	"github.com/spf13/cobra"
)

var agentWatchPRCmd = &cobra.Command{
	Use:   "pr <ready|merge|close|approve|request|comment|threads> <REF|key|url> [words]",
	Short: "Mark a pull request of yours ready for review, merge or close it; approve, ask for changes on, comment on, or read the threads of any",
	Long: `Acts on a pull request of yours: the one corgi's run opened for a ticket, the
one a session on the ticket opened, or the one an inbox row is about. A link
works too.

  corgi agent watch pr ready ABC-123        the draft is ready for review
  corgi agent watch pr merge ABC-123
  corgi agent watch pr close acme/api#42
  corgi agent watch pr ready https://github.com/acme/api/pull/42
  corgi agent watch pr approve acme/api#42 "nice"
  corgi agent watch pr request acme/api#42 cap the retries
  corgi agent watch pr comment https://github.com/acme/api/pull/42 "one question…"
  corgi agent watch pr threads <url> <url>...   open review threads, with the ids a reply needs
  corgi agent watch pr threads ABC-123 --all    resolved ones too

Never automatic: this is a person's call. ready, merge and close are refused
on a pull request that is not yours; approve, request and comment go on any
the inbox knows - the one somebody asked you to review first of all.`,
	Args: cobra.MinimumNArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		verb := strings.ToLower(strings.TrimSpace(args[0]))
		switch verb {
		case "ready", "merge", "close", "approve", "request", "comment":
		case "threads":
			runPRThreads(cmd, args[1:])
			return
		default:
			exitWithError("agent_watch_pr", fmt.Errorf("the verb is ready, merge, close, approve, request, comment or threads, not %q", args[0]), 2)
		}
		review := verb == "approve" || verb == "request" || verb == "comment"
		words := strings.TrimSpace(strings.Join(args[2:], " "))
		dir := mustAgentDir()
		workspace, _ := cmd.Flags().GetString("workspace")
		link, ws, err := pullLinkFor(dir, strings.TrimSpace(args[1]), workspace, review)
		if err != nil {
			exitWithError("agent_watch_pr", err, 2)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		secrets := watch.LoadSecretsFor(dir, ws)
		switch verb {
		case "ready":
			err = watch.ReadyPR(ctx, secrets, link)
		case "merge":
			err = watch.MergePR(ctx, secrets, link)
		case "approve", "request", "comment":
			err = watch.ReviewPR(ctx, secrets, link, verb, words)
		default:
			err = watch.ClosePR(ctx, secrets, link)
		}
		if err != nil {
			exitWithError("agent_watch_pr", err, 1)
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"url": link, "did": verb})
			return
		}
		fmt.Printf("%s: %s\n", map[string]string{"ready": "ready for review", "merge": "merged", "close": "closed", "approve": "approved", "request": "changes requested", "comment": "commented"}[verb], link)
	},
}

// pullLinkFor turns a ticket, inbox key or link into a pull request link and
// the workspace whose token reaches it. anyones: a pull request somebody else
// opened will do (a review), not only one of yours.
func pullLinkFor(dir, arg, workspace string, anyones bool) (link, ws string, err error) {
	if strings.Contains(arg, "://") {
		return arg, workspace, nil
	}
	keys := inboxKeysFor(dir, arg, workspace)
	if len(keys) == 0 {
		return "", "", fmt.Errorf("nothing in the inbox matches %q", arg)
	}
	ws = workspace
	for _, k := range keys {
		e, ok := watch.FindEvent(dir, k)
		if !ok {
			continue
		}
		if anyones && watch.PullRef(e.URL) != "" {
			link = e.URL
		} else {
			link = prLinkFor(dir, e, nil)
		}
		if link != "" {
			if ws == "" {
				ws = e.Workspace
			}
			return link, ws, nil
		}
	}
	return "", "", fmt.Errorf("no pull request of yours on %s", arg)
}

type prThreads struct {
	URL     string               `json:"url"`
	Threads []watch.ReviewThread `json:"threads"`
	Error   string               `json:"error,omitempty"`
}

// runPRThreads reads the review threads of every pull request named, so an
// address-review pass starts from one call instead of hand-written GraphQL.
func runPRThreads(cmd *cobra.Command, refs []string) {
	dir := mustAgentDir()
	workspace, _ := cmd.Flags().GetString("workspace")
	all, _ := cmd.Flags().GetBool("all")
	var out []prThreads
	failed := false
	for _, ref := range refs {
		link, ws, err := pullLinkFor(dir, strings.TrimSpace(ref), workspace, true)
		row := prThreads{URL: firstNonEmpty(link, ref)}
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			row.Threads, err = watch.ReviewThreads(ctx, watch.LoadSecretsFor(dir, ws), link)
			cancel()
		}
		if err != nil {
			row.Error, failed = err.Error(), true
		}
		if !all {
			row.Threads = openThreads(row.Threads)
		}
		out = append(out, row)
	}
	if utils.JSONOutput {
		utils.PrintJSON(out)
	} else {
		for _, row := range out {
			fmt.Print(threadsText(row))
		}
	}
	if failed {
		exitProcess(1)
	}
}

// openThreads keeps the unresolved threads and each reviewer's latest
// summary: an older summary was answered by the round after it.
func openThreads(threads []watch.ReviewThread) []watch.ReviewThread {
	latest := map[string]int{}
	for i, t := range threads {
		if t.Kind == "review" && len(t.Notes) > 0 {
			latest[t.Notes[0].Author] = i
		}
	}
	var open []watch.ReviewThread
	for i, t := range threads {
		if t.Kind == "review" && len(t.Notes) > 0 && latest[t.Notes[0].Author] != i {
			continue
		}
		if !t.Resolved {
			open = append(open, t)
		}
	}
	return open
}

func threadsText(row prThreads) string {
	var b strings.Builder
	fmt.Fprintf(&b, "== %s\n", row.URL)
	if row.Error != "" {
		fmt.Fprintf(&b, "   error: %s\n\n", row.Error)
		return b.String()
	}
	if len(row.Threads) == 0 {
		b.WriteString("   no open threads\n\n")
		return b.String()
	}
	for _, t := range row.Threads {
		state := "open"
		if t.Resolved {
			state = "resolved"
		}
		switch t.Kind {
		case "review":
			fmt.Fprintf(&b, "-- review (%s)\n", t.State)
		case "comment":
			fmt.Fprintf(&b, "-- comment  discussion %s\n", t.ID)
		default:
			where := t.Path
			if t.Line > 0 {
				where += ":" + strconv.Itoa(t.Line)
			}
			if t.Outdated {
				state += ", outdated"
			}
			reply := ""
			if len(t.Notes) > 0 {
				reply = "  reply-to " + t.Notes[0].ID
			}
			fmt.Fprintf(&b, "-- %s  [%s]  thread %s%s\n", where, state, t.ID, reply)
		}
		for _, n := range t.Notes {
			fmt.Fprintf(&b, "   %s %s:\n", n.Author, n.At.Local().Format("2006-01-02 15:04"))
			for _, line := range strings.Split(strings.TrimSpace(n.Body), "\n") {
				fmt.Fprintf(&b, "     %s\n", line)
			}
		}
	}
	b.WriteString("\n")
	return b.String()
}

func init() {
	agentWatchPRCmd.Flags().String("workspace", "", "the workspace whose token to use, when a ref exists in two")
	agentWatchPRCmd.Flags().Bool("all", false, "threads: resolved ones too")
	agentWatchCmd.AddCommand(agentWatchPRCmd)
}
