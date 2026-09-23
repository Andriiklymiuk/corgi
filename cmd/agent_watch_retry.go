package cmd

import (
	"fmt"
	"strings"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/watch"

	"github.com/spf13/cobra"
)

var agentWatchRetryCmd = &cobra.Command{
	Use:   "retry <REF|key>",
	Short: "Run the unattended fix for an inbox row again, now",
	Long: `Hands the newest inbox row of a ticket or pull request back to the daemon as
if it had just arrived, past the once-only guard: for a run that was killed, a
run that went wrong, or a comment the watch already answered but you want
worked again. Caps and quiet hours still apply.

  corgi agent watch retry acme/api#42
  corgi agent watch retry ABC-123 --workspace api

A REF is what the inbox shows; a key (github:acme/api#42:…) is what --json
prints. The daemon must be running.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		workspace, _ := cmd.Flags().GetString("workspace")
		e, ok := retryEventFor(dir, args[0], workspace)
		if !ok {
			exitWithError("agent_watch_retry", fmt.Errorf("nothing in the inbox matches %q", args[0]), 2)
		}
		info, err := daemon.ReadInfo(dir)
		if err != nil || info == nil || !info.Commands {
			exitWithError("agent_watch_retry", fmt.Errorf("corgi agent is not running - `corgi agent serve`, or `corgi agent install` to start it at login"), 1)
		}
		if _, err := command.Write(dir, command.Command{Action: command.ActionWatch, Source: "watch retry", WatchEvent: &e, Retry: true}); err != nil {
			exitWithError("agent_watch_retry", err, 1)
		}
		daemon.Nudge(info)
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"ok": true, "key": e.Key, "ref": e.Ref, "kind": e.Kind})
			return
		}
		fmt.Printf("%s %s handed to the daemon again\n", e.Kind, e.Ref)
	},
}

func retryEventFor(dir, arg, workspace string) (watch.Event, bool) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return watch.Event{}, false
	}
	if e, ok := watch.FindEvent(dir, arg); ok && (workspace == "" || e.Workspace == workspace) {
		return e, true
	}
	for _, e := range watch.RecentEvents(dir, 500) {
		if !strings.EqualFold(e.Ref, arg) || (workspace != "" && e.Workspace != workspace) {
			continue
		}
		return e, true
	}
	return watch.Event{}, false
}

func init() {
	agentWatchRetryCmd.Flags().String("workspace", "", "only this workspace's rows, when a ref exists in two")
	agentWatchCmd.AddCommand(agentWatchRetryCmd)
}
