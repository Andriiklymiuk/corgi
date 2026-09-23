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

var agentWatchPRCmd = &cobra.Command{
	Use:   "pr <ready|merge|close|approve|request|comment> <REF|key|url> [words]",
	Short: "Mark a pull request of yours ready for review, merge or close it; approve, ask for changes on, or comment on any",
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

Never automatic: this is a person's call. ready, merge and close are refused
on a pull request that is not yours; approve, request and comment go on any
the inbox knows - the one somebody asked you to review first of all.`,
	Args: cobra.MinimumNArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		verb := strings.ToLower(strings.TrimSpace(args[0]))
		switch verb {
		case "ready", "merge", "close", "approve", "request", "comment":
		default:
			exitWithError("agent_watch_pr", fmt.Errorf("the verb is ready, merge, close, approve, request or comment, not %q", args[0]), 2)
		}
		review := verb == "approve" || verb == "request" || verb == "comment"
		words := strings.TrimSpace(strings.Join(args[2:], " "))
		dir := mustAgentDir()
		workspace, _ := cmd.Flags().GetString("workspace")
		link, ws := "", workspace
		arg := strings.TrimSpace(args[1])
		if strings.Contains(arg, "://") {
			link = arg
		} else {
			keys := inboxKeysFor(dir, arg, workspace)
			if len(keys) == 0 {
				exitWithError("agent_watch_pr", fmt.Errorf("nothing in the inbox matches %q", arg), 2)
			}
			for _, k := range keys {
				e, ok := watch.FindEvent(dir, k)
				if !ok {
					continue
				}
				if review && watch.PullRef(e.URL) != "" {
					link = e.URL
				} else {
					link = prLinkFor(dir, e, nil)
				}
				if link != "" {
					if ws == "" {
						ws = e.Workspace
					}
					break
				}
			}
			if link == "" {
				exitWithError("agent_watch_pr", fmt.Errorf("no pull request of yours on %s", arg), 2)
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		secrets := watch.LoadSecretsFor(dir, ws)
		var err error
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

func init() {
	agentWatchPRCmd.Flags().String("workspace", "", "the workspace whose token to use, when a ref exists in two")
	agentWatchCmd.AddCommand(agentWatchPRCmd)
}
