package cmd

import (
	"fmt"
	"os"
	"time"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

// suggestHistoryDefaultCooldown is the dedupe window for dismissed/proposed
// ideas: 30 days. Past this, a previously-rejected idea may resurface.
const suggestHistoryDefaultCooldown = 30 * 24 * time.Hour

// suggestHistoryRoot resolves the workspace root for the state file: the
// --workspace flag when set (cron passes an absolute path), else cwd.
func suggestHistoryRoot(cmd *cobra.Command) string {
	if ws, _ := cmd.Flags().GetString("workspace"); ws != "" {
		return ws
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

// failSuggestHistory reports an IO/parse error consistently and exits 1.
func failSuggestHistory(err error) {
	if utils.JSONOutput {
		utils.JSONError(utils.ErrConfig, err.Error())
	} else {
		fmt.Fprintf(os.Stderr, "suggest-history: %s\n", err)
	}
	os.Exit(1)
}

// failUsage reports a bad-usage error (JSON via JSONError, else stderr) and exits 2.
func failUsage(msg string) {
	if utils.JSONOutput {
		utils.JSONError(utils.ErrUsage, msg)
	} else {
		fmt.Fprintln(os.Stderr, msg)
	}
	os.Exit(2)
}

var suggestHistoryCmd = &cobra.Command{
	Use:   "suggest-history",
	Short: "Read/append the proactive-suggest dedupe + rate-limit state (corgi_services/suggest-history.json)",
	Long: `Thin state helper for the proactive-suggest skill. The state file is
per-developer audit data (gitignored) the skill reads to avoid re-filing an
idea that is already open or recently dismissed, and to enforce the weekly
filing cap. The skill never hand-rolls this JSON — it calls these subcommands.

  corgi suggest-history list   [--workspace <p>] [--json]
  corgi suggest-history check  --slug <s> [--cooldown 720h] [--max <n>] [--workspace <p>] [--json]
  corgi suggest-history record --slug <s> --status <filed|dismissed|proposed|skipped> [--ticket <K>] [--title <t>] [--lens <l>] [--workspace <p>] [--json]
  corgi suggest-history config [--json]`,
}

var suggestHistoryListCmd = &cobra.Command{
	Use:   "list",
	Short: "List recorded proactive-suggest entries",
	Run: func(cmd *cobra.Command, _ []string) {
		root := suggestHistoryRoot(cmd)
		h, err := utils.LoadSuggestHistory(root)
		if err != nil {
			failSuggestHistory(err)
			return
		}
		if h.Entries == nil {
			h.Entries = []utils.SuggestEntry{}
		}
		if utils.JSONOutput {
			utils.PrintJSON(h)
			return
		}
		if len(h.Entries) == 0 {
			utils.Info("No proactive-suggest history (corgi_services/suggest-history.json absent or empty).")
			return
		}
		for _, e := range h.Entries {
			ticket := e.Ticket
			if ticket == "" {
				ticket = "-"
			}
			utils.Infof("[%s] %s — %s (%s) %s\n", e.Status, e.Slug, e.Title, ticket, e.Ts.Format(time.RFC3339))
		}
	},
}

// suggestCheckResult is the dedupe/rate-limit verdict for one candidate slug.
type suggestCheckResult struct {
	Skip   bool   `json:"skip"`
	Reason string `json:"reason"`
	Slug   string `json:"slug"`
}

var suggestHistoryCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Decide whether a candidate slug should be skipped (deduped or rate-limited)",
	Run: func(cmd *cobra.Command, _ []string) {
		slug, _ := cmd.Flags().GetString("slug")
		if slug == "" {
			failUsage("suggest-history check requires --slug")
		}
		root := suggestHistoryRoot(cmd)
		h, err := utils.LoadSuggestHistory(root)
		if err != nil {
			failSuggestHistory(err)
			return
		}

		cooldown, _ := cmd.Flags().GetDuration("cooldown")
		if cooldown <= 0 {
			cooldown = suggestHistoryDefaultCooldown
		}
		// --max overrides config; else fall back to the user config (0 → helper default).
		maxPerWeek, _ := cmd.Flags().GetInt("max")
		if !cmd.Flags().Changed("max") {
			if cfg, cerr := utils.LoadUserConfig(); cerr == nil {
				maxPerWeek = cfg.Suggest.MaxPerWeek
			}
		}

		now := time.Now().UTC()
		res := suggestCheckResult{Slug: slug}
		if skip, reason := utils.ShouldSkip(h, slug, now, cooldown); skip {
			res.Skip = true
			res.Reason = reason
		} else if utils.RateLimited(h, now, maxPerWeek) {
			res.Skip = true
			res.Reason = "rate-limit"
		}

		if utils.JSONOutput {
			utils.PrintJSON(res)
			return
		}
		if res.Skip {
			utils.Infof("skip %s — reason=%s\n", slug, res.Reason)
			return
		}
		utils.Infof("ok %s — no dedupe/rate-limit hit\n", slug)
	},
}

var suggestHistoryRecordCmd = &cobra.Command{
	Use:   "record",
	Short: "Append one proactive-suggest outcome entry",
	Run: func(cmd *cobra.Command, _ []string) {
		slug, _ := cmd.Flags().GetString("slug")
		status, _ := cmd.Flags().GetString("status")
		if slug == "" || status == "" {
			failUsage("suggest-history record requires --slug and --status")
		}
		switch status {
		case "filed", "dismissed", "proposed", "skipped":
		default:
			failUsage("status must be one of: filed|dismissed|proposed|skipped")
		}
		ticket, _ := cmd.Flags().GetString("ticket")
		title, _ := cmd.Flags().GetString("title")
		lens, _ := cmd.Flags().GetString("lens")

		entry := utils.SuggestEntry{
			Slug: slug, Title: title, Lens: lens, Status: status, Ticket: ticket,
			Ts: time.Now().UTC(),
		}
		root := suggestHistoryRoot(cmd)
		if err := utils.AppendSuggestEntry(root, entry); err != nil {
			failSuggestHistory(err)
			return
		}
		if utils.JSONOutput {
			utils.PrintJSON(entry)
			return
		}
		utils.Infof("recorded %s (%s) at %s\n", slug, status, entry.Ts.Format(time.RFC3339))
	},
}

// suggestConfigView is the effective proactive-suggest mode.
type suggestConfigView struct {
	AutoFileDrafts bool `json:"autoFileDrafts"`
	MaxPerWeek     int  `json:"maxPerWeek"`
}

var suggestHistoryConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "Echo the effective proactive-suggest mode (autoFileDrafts, maxPerWeek)",
	Run: func(cmd *cobra.Command, _ []string) {
		cfg, err := utils.LoadUserConfig()
		if err != nil {
			failSuggestHistory(err)
			return
		}
		view := suggestConfigView{
			AutoFileDrafts: cfg.Suggest.AutoFileDrafts,
			MaxPerWeek:     cfg.Suggest.MaxPerWeek,
		}
		if utils.JSONOutput {
			utils.PrintJSON(view)
			return
		}
		mode := "propose"
		if view.AutoFileDrafts {
			mode = "auto-file-drafts"
		}
		perWeek := view.MaxPerWeek
		if perWeek <= 0 {
			perWeek = 1
		}
		utils.Infof("proactive suggest · mode=%s · cap=%d/week\n", mode, perWeek)
	},
}

func init() {
	suggestHistoryCheckCmd.Flags().String("slug", "", "candidate slug to check")
	suggestHistoryCheckCmd.Flags().Duration("cooldown", suggestHistoryDefaultCooldown, "dismissed/proposed dedupe window")
	suggestHistoryCheckCmd.Flags().Int("max", 0, "per-week filing cap override (0 = use config / default 1)")

	suggestHistoryRecordCmd.Flags().String("slug", "", "slug (derive via the skill's Slugify)")
	suggestHistoryRecordCmd.Flags().String("status", "", "filed|dismissed|proposed|skipped")
	suggestHistoryRecordCmd.Flags().String("ticket", "", "tracker key when status=filed")
	suggestHistoryRecordCmd.Flags().String("title", "", "suggestion title")
	suggestHistoryRecordCmd.Flags().String("lens", "", "eng|product")

	// --workspace applies to every subcommand that touches the state file.
	suggestHistoryCmd.PersistentFlags().String("workspace", "", "workspace root (default: cwd); cron passes an absolute path")

	suggestHistoryCmd.AddCommand(
		suggestHistoryListCmd,
		suggestHistoryCheckCmd,
		suggestHistoryRecordCmd,
		suggestHistoryConfigCmd,
	)
	rootCmd.AddCommand(suggestHistoryCmd)
}
