package cmd

import (
	"fmt"
	"strings"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/watch"
	"github.com/spf13/cobra"
)

var agentWatchIgnoreCmd = &cobra.Command{
	Use:   "ignore <REF|key>",
	Short: "Take a ticket out of the inbox for good",
	Long: `Drops every inbox row for a ticket, and stops the unattended mode picking it
up. Writes nothing to the tracker: this is your decision, kept on this machine
and shared by the phone, the menu bar and the editor.

  corgi agent watch ignore ABC-123
  corgi agent watch ignore acme/api#42 --workspace api
  corgi agent watch unignore ABC-123

A REF is what the inbox shows; a key (linear:ABC-123:comment:…) is what --json
prints, for a surface that already has one.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runIgnore(cmd, args[0], true)
	},
}

var agentWatchUnignoreCmd = &cobra.Command{
	Use:   "unignore <REF|key>",
	Short: "Put an ignored ticket back in the inbox",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runIgnore(cmd, args[0], false)
	},
}

func runIgnore(cmd *cobra.Command, arg string, ignore bool) {
	dir := mustAgentDir()
	workspace, _ := cmd.Flags().GetString("workspace")
	keys := inboxKeysFor(dir, arg, workspace)
	if len(keys) == 0 {
		exitWithError("agent_watch_ignore", fmt.Errorf("nothing in the inbox matches %q", arg), 2)
	}
	state := watch.LoadState(dir)
	for _, k := range keys {
		var err error
		if ignore {
			err = state.Ignore(k)
		} else {
			err = state.Unignore(k)
		}
		if err != nil {
			exitWithError("agent_watch_ignore", err, 1)
		}
	}
	verb := "ignored"
	if !ignore {
		verb = "back in the inbox"
	}
	said := fmt.Sprintf("%s %s (%s)", arg, verb, plural(len(keys), "row", "rows"))
	if utils.JSONOutput {
		utils.PrintJSON(map[string]any{"ref": arg, "keys": keys, "ignored": ignore})
		return
	}
	fmt.Println(said)
}

// inboxKeysFor is every logged event key for a ticket, newest first. An
// exact key wins; otherwise every recent event whose ref matches, so
// "ignore ABC-123" covers the issue and all of its comments.
func inboxKeysFor(dir, arg, workspace string) []string {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return nil
	}
	if e, ok := watch.FindEvent(dir, arg); ok {
		return []string{e.Key}
	}
	var keys []string
	for _, e := range watch.RecentEvents(dir, 500) {
		if !strings.EqualFold(e.Ref, arg) {
			continue
		}
		if workspace != "" && e.Workspace != workspace {
			continue
		}
		keys = append(keys, e.Key)
	}
	return keys
}

func init() {
	for _, c := range []*cobra.Command{agentWatchIgnoreCmd, agentWatchUnignoreCmd} {
		c.Flags().String("workspace", "", "only this workspace's rows, when a ref exists in two")
	}
	agentWatchCmd.AddCommand(agentWatchIgnoreCmd, agentWatchUnignoreCmd)
}
