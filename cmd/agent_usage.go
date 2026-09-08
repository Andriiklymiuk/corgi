package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/usage"
)

var agentUsageCmd = &cobra.Command{
	Use:   "usage",
	Short: "Every account's limits, where they are heading, and today's numbers",
	Long: `One table for the question "can I start another task": each Claude account
the board knows, its 5-hour and 7-day windows as Claude Code last fetched
them, the pace the daemon has measured and when the window runs out at that
pace, today's tokens by model, and how long sessions waited on you.

The limits are the numbers /usage shows; Claude Code refreshes them when a
session asks, so run /usage once under an account that shows nothing.
--json prints it all; --watch redraws every minute.`,
	Run: runAgentUsage,
}

type usageReport struct {
	At       time.Time          `json:"at"`
	Accounts []sessions.Account `json:"accounts"`
	Today    []accountDayJSON   `json:"today,omitempty"`
	Waits    usage.WaitSummary  `json:"waitsToday"`
	Limited  usage.WaitSummary  `json:"limitedToday"`
}

type accountDayJSON struct {
	Profile string `json:"profile"`
	usage.DayStats
}

func runAgentUsage(cmd *cobra.Command, _ []string) {
	watch, _ := cmd.Flags().GetBool("watch")
	dir := mustAgentDir()
	print := func() {
		rep := buildUsageReport(dir, time.Now())
		if utils.JSONOutput {
			utils.PrintJSON(rep)
			return
		}
		if watch {
			fmt.Print("\033[H\033[2J")
		}
		printUsageReport(rep)
	}
	print()
	if !watch || utils.JSONOutput {
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			print()
		}
	}
}

// buildUsageReport prefers the board's accounts (the daemon samples them
// every minute) and falls back to reading the caches itself.
func buildUsageReport(dir string, now time.Time) usageReport {
	rep := usageReport{At: now}
	if board, err := readBoard(dir); err == nil && len(board.Accounts) > 0 {
		rep.Accounts = board.Accounts
	} else {
		for _, a := range accountLimits(nil) {
			rep.Accounts = append(rep.Accounts, sessions.Account{Profile: a.Profile, ConfigDir: a.ConfigDir, Limits: a.Limits, Forecast: a.Forecast})
		}
	}
	for _, a := range rep.Accounts {
		if day, ok := usage.ReadDayStats(a.ConfigDir, usage.Today(now)); ok {
			rep.Today = append(rep.Today, accountDayJSON{Profile: a.Profile, DayStats: day})
		}
	}
	waits := usage.LoadWaits(dir, startOfDay(now))
	rep.Waits = usage.Summarize(waits, "wait")
	rep.Limited = usage.Summarize(waits, "limited")
	return rep
}

func startOfDay(now time.Time) time.Time {
	y, m, d := now.Local().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, now.Location())
}

func printUsageReport(rep usageReport) {
	fmt.Println("Accounts")
	for _, a := range rep.Accounts {
		name := a.Profile
		if a.ConfigDir != "" {
			name += " (" + strings.Replace(a.ConfigDir, os.Getenv("HOME"), "~", 1) + ")"
		}
		if a.Limits == nil {
			fmt.Printf("  %-28s no usage snapshot yet — run /usage once in a session under it\n", name)
			continue
		}
		fmt.Printf("  %-28s 5h %s  week %s  · %d session(s) · as of %s ago\n", name,
			limitBar(a.Limits.FiveHour), limitBar(a.Limits.SevenDay), a.Sessions, agoText(time.Since(a.Limits.FetchedAt)))
		if line := forecastLine(a.Limits, a.Forecast); line != "" {
			fmt.Printf("  %-28s %s\n", "", line)
		}
	}
	if len(rep.Today) > 0 {
		fmt.Println("\nToday (Claude Code's own stats cache; lags by hours)")
		for _, d := range rep.Today {
			var models []string
			for _, m := range d.Models {
				models = append(models, shortModel(m.Model)+" "+formatTokens(m.Tokens))
			}
			fmt.Printf("  %-28s %d sessions · %d messages · %d tool calls", d.Profile, d.Sessions, d.Messages, d.ToolCalls)
			if len(models) > 0 {
				fmt.Printf(" · %s", strings.Join(models, ", "))
			}
			fmt.Println()
		}
	}
	fmt.Println("\nWaiting on you today")
	if rep.Waits.Count == 0 {
		fmt.Println("  nothing waited yet")
	} else {
		fmt.Printf("  %d waits · median %s · longest %s (%s) · total %s\n", rep.Waits.Count,
			shortDuration(time.Duration(rep.Waits.Median)*time.Second), shortDuration(time.Duration(rep.Waits.Longest)*time.Second),
			rep.Waits.LongestLabel, shortDuration(time.Duration(rep.Waits.Total)*time.Second))
	}
	if rep.Limited.Count > 0 {
		fmt.Printf("  limits cost %s across %d session(s)\n", shortDuration(time.Duration(rep.Limited.Total)*time.Second), rep.Limited.Count)
	}
}

// limitBar is "62% ▓▓▓▓▓▓░░░░ resets 4:10pm".
func limitBar(w usage.Window) string {
	filled := w.Percent / 10
	if filled > 10 {
		filled = 10
	}
	bar := strings.Repeat("▓", filled) + strings.Repeat("░", 10-filled)
	out := fmt.Sprintf("%3d%% %s", w.Percent, bar)
	if !w.ResetsAt.IsZero() {
		out += " resets " + resetText(w.ResetsAt)
	}
	return out
}

func resetText(at time.Time) string {
	at = at.Local()
	if time.Until(at) > 24*time.Hour {
		return at.Format("Mon 3:04pm")
	}
	return at.Format("3:04pm")
}

// forecastLine turns the slope into the sentence that decides things.
func forecastLine(l *usage.Limits, f *usage.Forecast) string {
	if f == nil {
		return ""
	}
	var parts []string
	if w := f.FiveHour; w != nil {
		switch {
		case w.ExhaustAt.IsZero():
			parts = append(parts, "5h: pace flat")
		case w.Safe:
			parts = append(parts, fmt.Sprintf("5h: %.0f%%/h, lasts until the reset", w.PercentPerHour))
		default:
			parts = append(parts, fmt.Sprintf("5h: %.0f%%/h, RUNS OUT %s (before the %s reset)", w.PercentPerHour, resetText(w.ExhaustAt), resetText(l.FiveHour.ResetsAt)))
		}
	}
	if w := f.SevenDay; w != nil && !w.ExhaustAt.IsZero() {
		if w.Safe {
			parts = append(parts, "week: fine")
		} else {
			parts = append(parts, fmt.Sprintf("week: RUNS OUT %s", resetText(w.ExhaustAt)))
		}
	}
	return strings.Join(parts, " · ")
}

// forecastSuffix is the short form for `corgi agent status`.
func forecastSuffix(f *usage.Forecast) string {
	if f == nil || f.FiveHour == nil || f.FiveHour.ExhaustAt.IsZero() || f.FiveHour.Safe {
		return ""
	}
	return " · runs out " + resetText(f.FiveHour.ExhaustAt)
}

func shortModel(model string) string {
	model = strings.TrimPrefix(model, "claude-")
	if i := strings.Index(model, "-20"); i > 0 {
		model = model[:i]
	}
	return model
}

func init() {
	agentUsageCmd.Flags().Bool("watch", false, "Redraw every minute")
	agentCmd.AddCommand(agentUsageCmd)
}

// digestText is the one message a day: what ran, what it cost in waiting,
// where the limits stand. Plain lines, short enough for a phone.
func digestText(dir string, now time.Time) string {
	rep := buildUsageReport(dir, now)
	var lines []string
	for _, d := range rep.Today {
		if d.Sessions == 0 && d.Messages == 0 {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: %d sessions, %d messages, %d tool calls", d.Profile, d.Sessions, d.Messages, d.ToolCalls))
	}
	if rep.Waits.Count > 0 {
		lines = append(lines, fmt.Sprintf("waited on you %d× — median %s, longest %s (%s)", rep.Waits.Count,
			shortDuration(time.Duration(rep.Waits.Median)*time.Second), shortDuration(time.Duration(rep.Waits.Longest)*time.Second), rep.Waits.LongestLabel))
	}
	if rep.Limited.Count > 0 {
		lines = append(lines, fmt.Sprintf("limits cost %s", shortDuration(time.Duration(rep.Limited.Total)*time.Second)))
	}
	for _, a := range rep.Accounts {
		if a.Limits == nil {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: 5h %d%%, week %d%%", a.Profile, a.Limits.FiveHour.Percent, a.Limits.SevenDay.Percent))
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

var agentDigestCmd = &cobra.Command{
	Use:   "digest",
	Short: "Today's one-message summary, as the daily digest would send it",
	Long: `Prints what the daily digest says: sessions and messages per account, how
long sessions waited on you, what limits cost, where each account stands.
Set digestAt: "20:00" in the agent config and the daemon sends it once a
day wherever notifications go (desktop, Telegram, webhook). --send sends it now.`,
	Run: func(cmd *cobra.Command, _ []string) {
		send, _ := cmd.Flags().GetBool("send")
		text := digestText(mustAgentDir(), time.Now())
		if text == "" {
			text = "nothing on record today"
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"text": text, "sent": send})
		} else {
			fmt.Println(text)
		}
		if send {
			utils.Notify("corgi agent · today", text)
		}
	},
}

func init() {
	agentDigestCmd.Flags().Bool("send", false, "Send it now, the way the daemon would")
	agentCmd.AddCommand(agentDigestCmd)
}
