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

// A pull request of mine, changed from wherever I am: out of draft, merged,
// closed. The same three the phone and the page offer on a row.
var agentWatchPRCmd = &cobra.Command{
	Use:   "pr <ready|merge|close> <REF|key|url>",
	Short: "Mark a pull request of yours ready for review, merge it, or close it",
	Long: `Acts on a pull request of yours: the one corgi's run opened for a ticket, the
one a session on the ticket opened, or the one an inbox row is about. A link
works too.

  corgi agent watch pr ready ABC-123        the draft is ready for review
  corgi agent watch pr merge ABC-123
  corgi agent watch pr close acme/api#42
  corgi agent watch pr ready https://github.com/acme/api/pull/42

Never automatic: this is a person's call, and it is refused on a pull request
that is not yours.`,
	Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		verb := strings.ToLower(strings.TrimSpace(args[0]))
		if verb != "ready" && verb != "merge" && verb != "close" {
			exitWithError("agent_watch_pr", fmt.Errorf("the verb is ready, merge or close, not %q", args[0]), 2)
		}
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
				if link = prLinkFor(dir, e, nil); link != "" {
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
		fmt.Printf("%s: %s\n", map[string]string{"ready": "ready for review", "merge": "merged", "close": "closed"}[verb], link)
	},
}

func init() {
	agentWatchPRCmd.Flags().String("workspace", "", "the workspace whose token to use, when a ref exists in two")
	agentWatchCmd.AddCommand(agentWatchPRCmd)
}
