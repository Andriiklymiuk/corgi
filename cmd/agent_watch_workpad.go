package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"andriiklymiuk/corgi/utils/agent/watch"
	"github.com/spf13/cobra"
)

var agentWatchWorkpadCmd = &cobra.Command{
	Use:   "workpad <REF> <section> [text]",
	Short: "Write one section of the ticket's corgi comment",
	Long: `A ticket gets one corgi comment — the workpad — with a section per thing:
the spec, the pull requests, the latest handoff, a blocker. Writing a section
rewrites it in place; the comment is created the first time. Empty text
removes the section.

  corgi agent watch workpad ABC-123 Spec - < docs/stories/ABC-123.md
  corgi agent watch workpad ABC-123 Blocked "no GITLAB_TOKEN for the web repo"
  corgi agent watch workpad ABC-123 Blocked ""

Text is the third argument, or stdin when it is "-".`,
	Args: cobra.RangeArgs(2, 3),
	Run: func(cmd *cobra.Command, args []string) {
		text := ""
		if len(args) == 3 {
			text = args[2]
		}
		if text == "-" {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				exitWithError("agent_watch_workpad", err, 1)
			}
			text = string(data)
		}
		section := strings.TrimSpace(args[1])
		runTrackerWrite(cmd, "agent_watch_workpad", args[0], func(ctx context.Context, w watch.Writer, ref string) (string, error) {
			if err := watch.UpsertWorkpad(ctx, w, ref, section, text); err != nil {
				return "", err
			}
			if strings.TrimSpace(text) == "" {
				return fmt.Sprintf("%s: section %q removed from the workpad", ref, section), nil
			}
			return fmt.Sprintf("%s: workpad section %q written", ref, section), nil
		})
	},
}

func init() {
	agentWatchWorkpadCmd.Flags().String("workspace", "", "the workspace whose tracker token to use (default: the one you are in)")
	agentWatchCmd.AddCommand(agentWatchWorkpadCmd)
}
