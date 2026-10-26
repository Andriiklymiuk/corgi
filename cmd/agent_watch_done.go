package cmd

import (
	"fmt"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/watch"
	"github.com/spf13/cobra"
)

var agentWatchDoneCmd = &cobra.Command{
	Use:   "done <REF|key|link>...",
	Short: "A ticket or pull request is handled on this side: its inbox rows so far leave, later ones still come",
	Long: `Says you (or the session you are in) dealt with what the inbox holds for a
ticket or pull request: you replied, pushed, reviewed, read it. Every row
about it from before now leaves the inbox on every surface - the phone, the
menu bar, the editor, watch status. A row that arrives afterwards comes in
as usual; this is not ignore, which drops a ticket for good.

  corgi agent watch done HUM-1746
  corgi agent watch done acme/api#42 https://github.com/acme/api/pull/43
  corgi agent watch done linear:HUM-1746:comment:…     one row, by its key

corgi marks this itself when you write through it: corgi agent watch
comment, corgi agent watch pr reply|approve|request|comment|merge|close|ready.
A skill that worked a tracked ticket runs it when the work is pushed.`,
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		var marked []string
		for _, a := range args {
			if ref := markInboxDone(dir, a, "you"); ref != "" {
				marked = append(marked, ref)
			}
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"done": marked})
			return
		}
		fmt.Printf("handled on this side: %s\n", strings.Join(marked, ", "))
	},
}

// markInboxDone marks a ticket ref, a pull request link or ref, or one row's
// key as handled now, and says what it marked.
func markInboxDone(dir, arg, by string) string {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return ""
	}
	log := watch.LoadDoneLog(dir)
	now := time.Now()
	if e, ok := watch.FindEvent(dir, arg); ok && strings.Contains(arg, ":") {
		_ = log.MarkKey(e.Key, now, by)
		return e.Key
	}
	ref := arg
	if strings.HasPrefix(arg, "http") {
		if ref = watch.PullRef(arg); ref == "" {
			return ""
		}
	}
	_ = log.MarkRef(ref, now, by)
	return ref
}

func init() {
	agentWatchCmd.AddCommand(agentWatchDoneCmd)
}
