package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/timers"
)

var (
	laterSession   string
	laterWorkspace string
)

var agentLaterCmd = &cobra.Command{
	Use:   "later [when] [what...]",
	Short: "A one-off reminder: ring the phone, or type into a session, at a time",
	Long: `Sets a one-off timer. When it is due the daemon rings the phone and the
desktop, or with --session types the text into that session as if you had.
A sleeping laptop fires it on waking. Recurring jobs are routines.

  corgi agent later 2h "check whether PR #12 got a review"
  corgi agent later 17:30 "send the release notes"
  corgi agent later tomorrow 9:00 "standup: the migration"
  corgi agent later 45m "look at the CI run again" --session api
  corgi agent later                  what is waiting
  corgi agent later cancel 3fa2c1    drop one`,
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		if err := runAgentLater(dir, args, time.Now()); err != nil {
			exitWithError("agent_later", err, 2)
		}
	},
}

func init() {
	agentLaterCmd.Flags().StringVar(&laterSession, "session", "", "type the text into this session (id, label or number) instead of ringing")
	agentLaterCmd.Flags().StringVar(&laterWorkspace, "workspace", "", "the workspace the reminder belongs to")
	agentCmd.AddCommand(agentLaterCmd)
}

func runAgentLater(dir string, args []string, now time.Time) error {
	if len(args) == 0 {
		return printTimers(dir, now)
	}
	if args[0] == "cancel" || args[0] == "rm" {
		if len(args) != 2 {
			return fmt.Errorf("cancel takes the timer id: corgi agent later cancel <id>")
		}
		t, err := timers.Cancel(dir, args[1])
		if err != nil {
			return err
		}
		if utils.JSONOutput {
			utils.PrintJSON(t)
			return nil
		}
		utils.Infof("✓ cancelled: %s\n", t.Text)
		return nil
	}
	at, used, err := timers.ParseWhen(args, now)
	if err != nil {
		return err
	}
	text := strings.Join(args[used:], " ")
	t, err := timers.Add(dir, timers.Timer{At: at, Text: text, Session: laterSession, Workspace: laterWorkspace, By: "cli"}, now)
	if err != nil {
		return err
	}
	if utils.JSONOutput {
		utils.PrintJSON(t)
		return nil
	}
	where := "rings the phone"
	if t.Session != "" {
		where = "types into " + t.Session
	}
	utils.Infof("✓ %s %s, %s (%s) · id %s\n", where, timers.Words(at.Sub(now)), at.Local().Format("Mon 15:04"), t.Text, t.ID)
	return nil
}

func printTimers(dir string, now time.Time) error {
	list, err := timers.List(dir)
	if err != nil {
		return err
	}
	if utils.JSONOutput {
		if list == nil {
			list = []timers.Timer{}
		}
		utils.PrintJSON(list)
		return nil
	}
	if len(list) == 0 {
		fmt.Println("No timers. corgi agent later 2h \"what to check\"")
		return nil
	}
	for _, t := range list {
		target := "phone"
		if t.Session != "" {
			target = "→ " + t.Session
		}
		fmt.Printf("  %s  %-16s %-12s %s\n", t.ID, t.At.Local().Format("Mon Jan 2 15:04"), target, t.Text)
	}
	return nil
}
