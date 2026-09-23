package cmd

import (
	"andriiklymiuk/corgi/utils/agent/harness"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/supervisor"
	"andriiklymiuk/corgi/utils/agent/watch"
	"andriiklymiuk/corgi/utils/agent/workspace"
)

const (
	watchFlagPruneAfter         = "prune-after"
	watchFlagMaxPerHour         = "max-per-hour"
	watchFlagMaxPerDay          = "max-per-day"
	watchFlagLimitCeiling       = "limit-ceiling"
	watchFlagMaxTotal           = "max-total"
	watchFlagReviewStatus       = "review-status"
	watchFlagNoRetry            = "no-retry"
	watchFlagAutoMerge          = "auto-merge"
	watchFlagAfterMerge         = "after-merge"
	watchFlagAfterMergeSubtasks = "after-merge-subtasks"
	watchFlagHandOver           = "hand-over"
	watchFlagAutoCarry          = "auto-carry"
	watchFlagRerunCI            = "rerun-ci"
	watchFlagCompactAt          = "compact-at"
	watchFlagDoneWhen           = "done-when"
	watchFlagAutoAllow          = "auto-allow"
	watchFlagPlanReview         = "plan-review"
	watchFlagAutoFor            = "auto-for"
	watchFlagDaysOff            = "days-off"
	watchFlagDryRun             = "dry-run"
)

var agentWatchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Watch the tracker and your pull requests: new issues, new comments, reviews — notify, or fix",
	Long: `The daemon polls Linear or Jira and GitHub or GitLab for the workspaces that
opted in, with a saved cursor so each round asks only for what changed, and
dedupes locally. Webhooks (corgi agent watch hooks) feed the same pipeline
and cost nothing between events. An event becomes a notification; with
action: fix it also starts a headless claude in the workspace with the
matching skill (draft PRs only, never a merge). Nothing runs an agent when
nothing changed.

Tokens: LINEAR_API_KEY; JIRA_URL, JIRA_EMAIL, JIRA_API_TOKEN; GITHUB_TOKEN
(or gh auth); GITLAB_TOKEN. Or store them once: corgi agent watch auth.
Workspaces at different companies get their own: add --local to that.`,
	Run: runAgentWatchStatus,
}

var agentWatchEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Watch this workspace (run inside it)",
	RunE: func(cmd *cobra.Command, _ []string) error {
		dir := mustAgentDir()
		flags := cmd.Flags()
		id, err := watchTargetWorkspace(dir, flags)
		if err != nil {
			return err
		}
		path := agentUserConfigPath(dir)
		user, err := config.LoadUser(path)
		if err != nil {
			return err
		}
		entry := user.Workspaces[id]
		wc := entry.Watch
		if wc == nil {
			wc = &config.WatchConfig{}
		}
		wc.Enabled = true
		if v, _ := flags.GetString("labels"); v != "" {
			wc.Labels = splitList(v)
		}
		if v, _ := flags.GetString("states"); v != "" {
			wc.States = splitList(v)
		}
		if v, _ := flags.GetString("assignee"); v != "" {
			wc.Assignee = v
		}
		if v, _ := flags.GetString("project"); v != "" {
			wc.Project = v
		}
		if v, _ := flags.GetString("tracker"); v != "" {
			wc.Tracker = v
		}
		if v, _ := flags.GetString("repos"); v != "" {
			wc.Repos = splitList(v)
		}
		if v, _ := flags.GetString("interval"); v != "" {
			if _, err := time.ParseDuration(v); err != nil && v != "0" {
				return fmt.Errorf("--interval: %w", err)
			}
			wc.Interval = v
		}
		if auto, _ := flags.GetBool("auto"); auto {
			wc.Action, wc.PRs, wc.Comments = "fix", true, true
		}
		if v, _ := flags.GetString("action"); v != "" {
			if v != "notify" && v != "fix" {
				return fmt.Errorf("--action must be notify or fix")
			}
			wc.Action = v
		}
		if flags.Changed("comments") {
			wc.Comments, _ = flags.GetBool("comments")
		}
		if flags.Changed("prs") {
			wc.PRs, _ = flags.GetBool("prs")
		}
		if flags.Changed(watchFlagMaxPerHour) {
			if wc.MaxFixesPerHour, _ = flags.GetInt(watchFlagMaxPerHour); wc.MaxFixesPerHour < 1 {
				return fmt.Errorf("--max-per-hour must be at least 1")
			}
		}
		if flags.Changed(watchFlagLimitCeiling) {
			v, _ := flags.GetInt(watchFlagLimitCeiling)
			if v < 0 || v > 100 {
				return fmt.Errorf("--%s is a percent, 0 to 100", watchFlagLimitCeiling)
			}
			wc.LimitCeiling = v
		}
		if flags.Changed(watchFlagMaxPerDay) {
			if wc.MaxFixesPerDay, _ = flags.GetInt(watchFlagMaxPerDay); wc.MaxFixesPerDay < 1 {
				return fmt.Errorf("--max-per-day must be at least 1")
			}
		}
		if flags.Changed(watchFlagMaxTotal) {
			if wc.MaxFixesTotal, _ = flags.GetInt(watchFlagMaxTotal); wc.MaxFixesTotal < 0 {
				return fmt.Errorf("--max-total cannot be negative (0 clears it)")
			}
			wc.CapSince = time.Now().UTC()
		}
		if flags.Changed(watchFlagReviewStatus) {
			v, _ := flags.GetString(watchFlagReviewStatus)
			wc.ReviewStatus = strings.TrimSpace(v)
		}
		if flags.Changed("isolate") {
			wc.Isolate, _ = flags.GetBool("isolate")
		}
		if flags.Changed(watchFlagPruneAfter) {
			v, _ := flags.GetString(watchFlagPruneAfter)
			if _, err := watch.ParseAge(v); err != nil {
				return fmt.Errorf("--%s: %w", watchFlagPruneAfter, err)
			}
			wc.PruneAfter = strings.TrimSpace(v)
		}
		if flags.Changed("slots") {
			if wc.Slots, _ = flags.GetInt("slots"); wc.Slots < 1 || wc.Slots > 8 {
				return fmt.Errorf("--slots is 1 to 8")
			}
			if wc.Slots > 1 && !wc.Isolate {
				wc.Isolate = true
				utils.Info("agent: --slots above 1 turns on --isolate: each run gets worktrees of its own")
			}
		}
		if flags.Changed(watchFlagNoRetry) {
			wc.NoRetry, _ = flags.GetBool(watchFlagNoRetry)
		}
		if flags.Changed("lease") {
			wc.Lease, _ = flags.GetBool("lease")
		}
		if flags.Changed("reviews") {
			wc.Reviews, _ = flags.GetBool("reviews")
		}
		if flags.Changed("ci") {
			wc.CI, _ = flags.GetBool("ci")
		}
		if flags.Changed(watchFlagAutoMerge) {
			wc.AutoMerge, _ = flags.GetBool(watchFlagAutoMerge)
		}
		if flags.Changed("batch") {
			if wc.Batch, _ = flags.GetInt("batch"); wc.Batch < 0 || wc.Batch > 5 {
				exitWithError("agent_watch_enable", fmt.Errorf("--batch is 1 (off) to 5 tickets per run"), 2)
			}
		}
		if flags.Changed(watchFlagAfterMerge) {
			wc.AfterMerge, _ = flags.GetString(watchFlagAfterMerge)
		}
		if flags.Changed(watchFlagAfterMergeSubtasks) {
			wc.AfterMergeSubtasks, _ = flags.GetString(watchFlagAfterMergeSubtasks)
		}
		if flags.Changed("approve") {
			wc.Approve, _ = flags.GetBool("approve")
		}
		if flags.Changed(watchFlagHandOver) {
			wc.HandOver, _ = flags.GetBool(watchFlagHandOver)
		}
		if flags.Changed("lessons") {
			wc.Lessons, _ = flags.GetBool("lessons")
		}
		if flags.Changed(watchFlagAutoCarry) {
			wc.AutoCarry, _ = flags.GetBool(watchFlagAutoCarry)
		}
		if flags.Changed(watchFlagRerunCI) {
			wc.RerunCI, _ = flags.GetBool(watchFlagRerunCI)
		}
		if flags.Changed("silent") {
			wc.Silent, _ = flags.GetBool("silent")
		}
		if flags.Changed("headless") {
			wc.Headless, _ = flags.GetBool("headless")
		}
		if flags.Changed("rebase") {
			wc.Rebase, _ = flags.GetBool("rebase")
		}
		if flags.Changed(watchFlagCompactAt) {
			v, _ := flags.GetInt(watchFlagCompactAt)
			if v < 0 || v > 100 {
				return fmt.Errorf("compact-at is a percent, 0 to 100, not %d", v)
			}
			wc.CompactAt = v
		}
		if flags.Changed(watchFlagDoneWhen) {
			v, _ := flags.GetString(watchFlagDoneWhen)
			wc.DoneWhen = splitList(v)
		}
		if flags.Changed(watchFlagAutoAllow) {
			v, _ := flags.GetString(watchFlagAutoAllow)
			policy, err := config.ParseAutoAllow(v)
			if err != nil {
				return err
			}
			wc.AutoAllow = policy
		}
		if flags.Changed(watchFlagPlanReview) {
			v, _ := flags.GetString(watchFlagPlanReview)
			policy, err := config.ParsePlanReview(v)
			if err != nil {
				return err
			}
			wc.PlanReview = policy
		}
		if flags.Changed("from") {
			v, _ := flags.GetString("from")
			wc.From = splitList(v)
		}
		if flags.Changed("bots") {
			wc.Bots, _ = flags.GetBool("bots")
		}
		if chat, err := chatFromFlags(flags, wc.Chat); err != nil {
			return err
		} else if chat != nil {
			wc.Chat = chat
		}
		if flags.Changed(watchFlagAutoFor) {
			v, _ := flags.GetString(watchFlagAutoFor)
			kinds, err := parseAutoFor(v)
			if err != nil {
				return err
			}
			wc.FixKinds = kinds
		}
		if flags.Changed("pickup") {
			v, _ := flags.GetString("pickup")
			wc.PickupStatus = strings.TrimSpace(v)
		}
		if flags.Changed("quiet") {
			v, _ := flags.GetString("quiet")
			if _, err := daemon.ParseQuiet(v); err != nil {
				return fmt.Errorf("--quiet: %w", err)
			}
			wc.Quiet = strings.TrimSpace(v)
		}
		if flags.Changed(watchFlagDaysOff) {
			v, _ := flags.GetString(watchFlagDaysOff)
			days, err := daemon.ParseDaysOff([]string{v})
			if err != nil {
				return fmt.Errorf("--days-off: %w", err)
			}
			wc.DaysOff = nil
			for _, d := range days {
				wc.DaysOff = append(wc.DaysOff, strings.ToLower(d.String()[:3]))
			}
		}
		if flags.Changed("kind") {
			kind, _ := flags.GetString("kind")
			if kind != "" && !harness.Known(kind) {
				return fmt.Errorf("--kind is one of %s", strings.Join(harness.Names(), ", "))
			}
			entry.Kind = kind
			entry.Agents = nil
		}
		if flags.Changed("agents") {
			raw, _ := flags.GetStringSlice("agents")
			order, err := parseAgents(raw)
			if err != nil {
				return err
			}
			entry.Agents = order
			entry.Kind = ""
		}
		entry.Watch = wc
		user.Workspaces[id] = entry
		if err := writeUserConfig(path, user); err != nil {
			return err
		}
		utils.Infof("watching %s — %s\n", id, describeWatch(wc))
		if order := entry.AgentOrder(); len(order) > 1 || order[0] != harness.Claude {
			utils.Infof("agent: unattended runs here go through %s\n", agentsWord(order))
		}
		if wc.Action == "fix" && !entry.DangerouslySkipPermissions && !user.Defaults.DangerouslySkipPermissions {
			utils.Info("note: fix runs with --permission-mode acceptEdits; a Bash step that needs approval will stall. `corgi agent init --dangerously-skip-permissions` lets it run unattended.")
		}
		utils.Info("restart the daemon to pick it up: corgi agent restart")
		return nil
	},
}

var agentWatchDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Stop watching this workspace",
	RunE: func(cmd *cobra.Command, _ []string) error {
		dir := mustAgentDir()
		id, err := watchTargetWorkspace(dir, cmd.Flags())
		if err != nil {
			return err
		}
		path := agentUserConfigPath(dir)
		user, err := config.LoadUser(path)
		if err != nil {
			return err
		}
		entry := user.Workspaces[id]
		if entry.Watch != nil {
			entry.Watch.Enabled = false
		}
		user.Workspaces[id] = entry
		if err := writeUserConfig(path, user); err != nil {
			return err
		}
		utils.Infof("%s no longer watched (after corgi agent restart)\n", id)
		return nil
	},
}

var agentWatchRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Poll every watched workspace once, right now, and print what is new",
	Long:  "Uses the same cursors the daemon uses, so a `run` here and the daemon's next round do not both report the same thing.",
	RunE: func(cmd *cobra.Command, _ []string) error {
		dir := mustAgentDir()
		specs, err := loadWatchSpecs(dir)
		if err != nil {
			return err
		}
		if len(specs) == 0 {
			utils.Info("no watched workspaces — `corgi agent watch enable` inside one")
			return nil
		}
		dryRun, _ := cmd.Flags().GetBool(watchFlagDryRun)
		state := watch.LoadState(dir)
		if dryRun {
			state = watch.LoadState(filepath.Join(dir, "watch", "dry-run"))
		}
		var found []watch.Event
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		for _, spec := range specs {
			w := &watch.Watch{Workspace: spec.Workspace, Rules: spec.Rules, Sources: spec.Sources, State: state,
				Sink: func(_ context.Context, e watch.Event) { found = append(found, e) },
				Log:  func(line string) { utils.Info(line) }}
			w.Once(ctx, time.Now())
		}
		if !dryRun {
			handBackDeferred(dir, state.Fixes)
		}
		if utils.JSONOutput {
			utils.PrintJSON(found)
			return nil
		}
		if len(found) == 0 {
			utils.Info("nothing new")
			return nil
		}
		for _, e := range found {
			utils.Infof("%-14s %-8s %s — %s\n", e.Kind, e.Workspace, e.Ref, firstLineOf(e.Title))
		}
		return nil
	},
}

func handBackDeferred(dir string, fixes *watch.FixLog) {
	deferred := fixes.DeferredEvents()
	if len(deferred) == 0 {
		return
	}
	info, _ := daemon.ReadInfo(dir)
	if info == nil || !info.Commands {
		utils.Infof("%d deferred fix(es), no daemon to hand them to:\n", len(deferred))
		for _, e := range deferred {
			utils.Infof("  %-14s %-8s %s — %s\n", e.Kind, e.Workspace, e.Ref, firstLineOf(e.Title))
		}
		return
	}
	queued := 0
	for i := range deferred {
		e := deferred[i]
		if _, err := command.Write(dir, command.Command{Action: command.ActionWatch, Source: "watch run", WatchEvent: &e}); err == nil {
			queued++
		}
	}
	daemon.Nudge(info)
	utils.Infof("%d deferred fix(es) handed back to the daemon\n", queued)
}

var agentWatchTestCmd = &cobra.Command{
	Use:   "test <issue.new|issue.comment|pr.comment|pr.review|review.requested|ci.failed>",
	Short: "Push one made-up event through the pipeline and print the claude run it would start, without starting it",
	Long: `Walks a synthesized event the way a webhook's would go: routing to a
workspace, the rules, the seen list, the fix claim and the caps, quiet hours
and the account's limits. Nothing is recorded and no claude runs; the prompt
and argv it would use are printed, so the pipeline can be proven without a
real event.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := mustAgentDir()
		specs, err := loadWatchSpecs(dir)
		if err != nil {
			return err
		}
		if len(specs) == 0 {
			return fmt.Errorf("no watched workspaces — `corgi agent watch enable` inside one")
		}
		ref, _ := cmd.Flags().GetString("ref")
		url, _ := cmd.Flags().GetString("url")
		body, _ := cmd.Flags().GetString("body")
		labels, _ := cmd.Flags().GetStringSlice("labels")
		e, err := synthesizeWatchEvent(watch.Kind(args[0]), specs, ref, url, body)
		if err != nil {
			return err
		}
		if len(labels) > 0 {
			e.Labels = labels
		} else if e.Kind == watch.KindIssueNew {
			e.Labels = specs[0].Rules.Labels
		}
		d := &daemon.Daemon{Dir: dir, Watches: specs}
		probe, routed := d.ProbeEvent(e, time.Now())
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"event": e, "routed": routed, "probe": probe})
			return nil
		}
		fmt.Printf("event      %s %s — key %s\n", e.Kind, e.Ref, e.Key)
		if !routed {
			fmt.Println("workspace  none — no watched workspace owns it and no rules take it")
			return nil
		}
		fmt.Printf("workspace  %s\n", probe.Workspace)
		if !probe.Matched {
			fmt.Printf("rules      no match — %s\n", probe.Why)
			return nil
		}
		fmt.Println("rules      match")
		if probe.Seen {
			fmt.Println("seen       yes — a duplicate, dropped before the sink")
		} else {
			fmt.Println("seen       no")
		}
		fmt.Printf("action     %s\n", probe.Action)
		if probe.Action != "fix" {
			return nil
		}
		switch {
		case probe.Deferred != "":
			fmt.Printf("fix        deferred: %s (%s)\n", probe.Deferred, probe.Budget)
		case probe.Busy:
			fmt.Println("fix        a fix for this ref is already running")
		default:
			fmt.Printf("fix        would start (%s)\n", probe.Budget)
		}
		fmt.Printf("prompt     %s\n", probe.Prompt)
		fmt.Printf("claude     %s\n", strings.Join(probe.Args, " "))
		return nil
	},
}

func synthesizeWatchEvent(kind watch.Kind, specs []daemon.WatchSpec, ref, url, body string) (watch.Event, error) {
	first := specs[0]
	e := watch.Event{Kind: kind, Ref: ref, URL: url, Body: body, Title: "watch test", Author: "watch test", Mine: true, At: time.Now()}
	switch kind {
	case watch.KindIssueNew, watch.KindIssueComment:
		synthesizeIssueEvent(&e, first)
	case watch.KindPRComment, watch.KindPRReview, watch.KindReviewRequested:
		synthesizePullEvent(&e, first)
	case watch.KindCIFailed:
		synthesizeCIEvent(&e, first)
	default:
		return e, fmt.Errorf("kind %q — want issue.new, issue.comment, pr.comment, pr.review, review.requested or ci.failed", kind)
	}
	e.Key = "test:" + string(kind) + ":" + e.Ref
	return e, nil
}

func synthesizeIssueEvent(e *watch.Event, first daemon.WatchSpec) {
	e.Source = "linear"
	for _, src := range first.Sources {
		if src.Name() == "jira" {
			e.Source = "jira"
		}
	}
	if e.Ref == "" {
		e.Ref = firstNonEmptyString(first.Project, "TEST") + "-1"
	}
	if e.Kind == watch.KindIssueComment && e.Body == "" {
		e.Body = "could you also cover the empty state?"
	}
}

func synthesizePullEvent(e *watch.Event, first daemon.WatchSpec) {
	e.Source = "github"
	if e.Ref == "" {
		e.Ref = watchFirstRepo(first, "acme/api") + "#1"
	}
	if e.URL == "" {
		e.URL = prRefURL(e.Ref)
		if strings.Contains(e.Ref, "!") {
			e.Source = "gitlab"
		}
	}
	if e.Body == "" {
		e.Body = "nit: this name reads wrong"
	}
	if e.Kind == watch.KindPRReview {
		e.State = "changes_requested"
	}
	if e.Kind == watch.KindReviewRequested {
		e.Mine = false
		e.Author = "a colleague"
		e.Title = "Retry the upload on a 502"
	}
}

func synthesizeCIEvent(e *watch.Event, first daemon.WatchSpec) {
	e.Source = "github"
	if e.Ref == "" {
		e.Ref = watchFirstRepo(first, "acme/api")
	}
	if e.URL == "" {
		e.URL = "https://github.com/" + e.Ref + "/actions"
	}
	e.Title = "e2e / checkout failed"
}

func watchFirstRepo(spec daemon.WatchSpec, fallback string) string {
	if len(spec.Repos) > 0 {
		return spec.Repos[0]
	}
	return fallback
}

func firstNonEmptyString(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

var agentWatchHooksCmd = &cobra.Command{
	Use:   "hooks",
	Short: "Webhooks for instant events: the plan per workspace, and --install for GitHub and GitLab",
	Long: `A webhook makes a comment arrive in seconds instead of on the next poll.
Polling stays on beside it whatever you set up: it catches what came while the
laptop was off or the tunnel was down, and the kinds a webhook here does not
send (review requests, red builds, a ticket assigned to you). The same comment
from both is one event — they share its key — so a mix never runs twice.

Each source of a workspace can be webhook + poll or poll alone, independently:
GitLab on webhooks while Jira polls, or GitHub on webhooks while Linear polls.
A webhook for a repo or project no watched workspace lists is dropped.

  corgi agent watch hooks                 # this workspace's plan
  corgi agent watch hooks --install       # create or update the GitHub / GitLab webhooks on its repos
  corgi agent watch hooks --rotate --install   # new secret, pushed to every repo`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		dir := mustAgentDir()
		secrets := watch.LoadSecrets(dir)
		rotate, _ := cmd.Flags().GetBool("rotate")
		install, _ := cmd.Flags().GetBool("install")
		if secrets.HookSecret == "" || rotate {
			secrets.HookSecret = randomSecret()
			if err := watch.SaveSecrets(dir, secrets); err != nil {
				return err
			}
			if rotate && !install {
				utils.Info("new secret — every webhook set up with the old one now fails until it is updated; --install does GitHub and GitLab")
			}
		}
		base := launcherURL()
		if base == "" {
			if install {
				return fmt.Errorf("no public URL for the webhooks to reach — `corgi agent up` (a named tunnel keeps the URL stable)")
			}
			utils.Info("no public URL yet — `corgi agent up` first; a named tunnel keeps these URLs stable")
			base = "https://<your-tunnel>"
		} else {
			base = strings.TrimSuffix(base, "/app")
		}
		id, err := watchTargetWorkspace(dir, cmd.Flags())
		if err != nil {
			return err
		}
		user, err := config.LoadUser(agentUserConfigPath(dir))
		if err != nil {
			return err
		}
		wc := user.Workspaces[id].Watch
		if wc == nil || !wc.Enabled {
			return fmt.Errorf("%s is not watched — `corgi agent watch enable` there first", id)
		}
		ws := watch.LoadSecretsFor(dir, id)
		plan := hookPlanFor(wc, ws)
		fmt.Printf("%s — webhooks at %s/hooks/<source>, polling every %s beside them\n\n", id, base, firstNonEmptyString(wc.Interval, "3m"))
		for _, repo := range plan.gitlab {
			line := "  gitlab  " + repo
			if install {
				line += "  " + hookInstallWord(watch.InstallGitLabHook(cmd.Context(), ws.GitLabURL, ws.GitLab, repo, base+"/hooks/gitlab", secrets.HookSecret))
			}
			fmt.Println(line)
		}
		gh := watch.NewGitHub(ws, nil)
		for _, repo := range plan.github {
			line := "  github  " + repo
			if install {
				line += "  " + hookInstallWord(watch.InstallGitHubHook(cmd.Context(), gh.Token, repo, base+"/hooks/github", secrets.HookSecret))
			}
			fmt.Println(line)
		}
		if plan.githubAny {
			fmt.Println("  github  every repo (no --repos) — --install needs a list; set --repos, or add an org webhook by hand")
		}
		if !install && (len(plan.gitlab) > 0 || len(plan.github) > 0) {
			fmt.Println("\n  --install creates or updates these (GitLab: Maintainer; GitHub: repo admin). By hand:")
			if len(plan.gitlab) > 0 {
				fmt.Printf("    GitLab  project → Settings → Webhooks: %s/hooks/gitlab, secret token below, trigger: Comments\n", base)
			}
			if len(plan.github) > 0 || plan.githubAny {
				fmt.Printf("    GitHub  repo → Settings → Webhooks: %s/hooks/github, application/json, secret below; events: Issue comments, Pull request reviews, Pull request review comments\n", base)
			}
		}
		switch plan.tracker {
		case "linear":
			fmt.Printf("\n  linear  by hand, workspace admin: Settings → API → Webhooks → %s/hooks/linear, secret below, events: Issues, Comments\n", base)
		case "jira":
			fmt.Printf("\n  jira    by hand, Jira admin: Settings → System → WebHooks → %s/hooks/jira?token=<secret>, events: Issue created, Comment created\n", base)
		}
		if plan.tracker != "" || !install {
			fmt.Printf("\nsecret: %s\n", secrets.HookSecret)
		}
		fmt.Println("\nPolling still covers review requests, red builds and tickets assigned to you; `corgi agent watch status` shows when each webhook last came in.")
		return nil
	},
}

type hookPlan struct {
	gitlab, github []string
	githubAny      bool
	tracker        string
}

// hookPlanFor sorts a workspace's repos by forge: a path deeper than
// owner/repo is GitLab (groups nest there, never on GitHub); owner/repo goes
// to GitHub when the workspace has a GitHub token, else GitLab.
func hookPlanFor(wc *config.WatchConfig, s watch.Secrets) hookPlan {
	var p hookPlan
	hasGitLab := strings.TrimSpace(s.GitLab) != ""
	hasGitHub := watch.NewGitHub(s, nil).Token != ""
	for _, repo := range wc.Repos {
		switch {
		case strings.Count(repo, "/") > 1 && hasGitLab:
			p.gitlab = append(p.gitlab, repo)
		case strings.Count(repo, "/") == 1 && hasGitHub:
			p.github = append(p.github, repo)
		case hasGitLab:
			p.gitlab = append(p.gitlab, repo)
		}
	}
	p.githubAny = len(wc.Repos) == 0 && hasGitHub && wc.PRs
	p.tracker = watchTracker(wc.Tracker, s)
	return p
}

func hookInstallWord(r watch.HookInstall) string {
	switch {
	case r.Err == nil:
		return "✓ " + r.Action
	case r.Missing != "":
		return "✗ needs " + r.Missing
	}
	return "✗ " + r.Err.Error()
}

var agentWatchAuthCmd = &cobra.Command{
	Use:   "auth <linear|jira|github|gitlab|slack>",
	Short: "Store a token for a source, for the machine or for one workspace",
	Long: `Without --local the token is the machine-wide one every watched
workspace falls back to. With --local (run inside the workspace) or
--workspace <id> it is stored for that workspace only and beats both the
machine-wide token and the environment — one Jira per client, a second
Linear key, a self-hosted GitLab.

Tokens are never written into the repository. They live in the user-level
agent directory, mode 0600, next to the machine-wide ones.

  corgi agent watch auth jira --token xxx --url https://acme.atlassian.net --email me@acme.com --local
  corgi agent watch auth linear --clear --local`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := mustAgentDir()
		flags := cmd.Flags()
		local, _ := flags.GetBool("local")
		id, _ := flags.GetString("workspace")
		drop, _ := flags.GetBool("clear")
		if local && id == "" {
			resolved, err := currentWorkspaceID(dir)
			if err != nil {
				return err
			}
			id = resolved
		}
		secrets := watch.LoadSecrets(dir)
		if id != "" {
			secrets = watch.WorkspaceSecrets(dir, id)
		}
		token, _ := flags.GetString("token")
		bot, _ := flags.GetString("bot")
		url, _ := flags.GetString("url")
		email, _ := flags.GetString("email")
		me, _ := flags.GetString("me")
		if drop {
			token, url, email = "", "", ""
		}
		switch args[0] {
		case "linear":
			secrets.Linear = token
		case "jira":
			secrets.JiraToken, secrets.JiraURL, secrets.JiraEmail = token, url, email
		case "github":
			secrets.GitHub = token
		case "gitlab":
			secrets.GitLab = token
			if url != "" || drop {
				secrets.GitLabURL = url
			}
		case "slack":
			if drop {
				secrets.SlackUser, secrets.SlackBot = "", ""
				break
			}
			if token != "" && !strings.HasPrefix(token, "xoxp-") {
				return fmt.Errorf("--token wants the user token (xoxp-…): it is what reads your channels and posts as you; a bot token goes in --bot")
			}
			if bot != "" && !strings.HasPrefix(bot, "xoxb-") {
				return fmt.Errorf("--bot wants the bot token (xoxb-…)")
			}
			if token == "" && bot == "" {
				return fmt.Errorf("slack: --token xoxp-… (read and post as you) and/or --bot xoxb-… (post as the app)")
			}
			if token != "" {
				secrets.SlackUser = token
			}
			if bot != "" {
				secrets.SlackBot = bot
			}
		default:
			return fmt.Errorf("unknown source %q", args[0])
		}
		if me != "" || drop {
			secrets.Me = me
		}
		where := "machine-wide"
		if id != "" {
			if err := watch.SaveWorkspaceSecrets(dir, id, secrets); err != nil {
				return err
			}
			where = id + " only"
		} else if err := watch.SaveSecrets(dir, secrets); err != nil {
			return err
		}
		if drop {
			utils.Infof("%s: token cleared (%s)\n", args[0], where)
		} else {
			utils.Infof("%s: token %s saved (%s)\n", args[0], watch.Fingerprint(token), where)
		}
		utils.Info("restart the daemon to pick it up: corgi agent restart")
		return nil
	},
}

func chatStatusLine(spec daemon.WatchSpec) string {
	c := spec.Chat
	if c == nil {
		return ""
	}
	var parts []string
	if c.Mentions {
		parts = append(parts, "mentions")
	}
	if len(c.Channels) > 0 {
		parts = append(parts, strings.Join(c.Channels, " "))
	}
	if len(c.ReviewChannels) > 0 {
		parts = append(parts, "reviews in "+strings.Join(c.ReviewChannels, " "))
	}
	if len(parts) == 0 {
		return ""
	}
	line := "slack: " + strings.Join(parts, " · ")
	if len(c.Trust) > 0 {
		return line + " · runs from " + strings.Join(c.Trust, " ")
	}
	return line + " · no runs from chat"
}

func withoutSource(list []string, drop string) []string {
	out := list[:0]
	for _, n := range list {
		if n != drop {
			out = append(out, n)
		}
	}
	return out
}

func chatFromFlags(flags *pflag.FlagSet, current *config.ChatConfig) (*config.ChatConfig, error) {
	touched := false
	for _, n := range []string{"mentions", "channel", "review-channel", "trust", "post-to", "reply-as"} {
		if flags.Changed(n) {
			touched = true
		}
	}
	if !touched {
		return nil, nil
	}
	out := &config.ChatConfig{Slack: &config.SlackWatch{}}
	if current != nil && current.Slack != nil {
		copied := *current.Slack
		out.Slack = &copied
	}
	if flags.Changed("mentions") {
		out.Slack.Mentions, _ = flags.GetBool("mentions")
	}
	if flags.Changed("channel") {
		out.Slack.Channels, _ = flags.GetStringSlice("channel")
	}
	if flags.Changed("review-channel") {
		out.Slack.ReviewChannels, _ = flags.GetStringSlice("review-channel")
	}
	if flags.Changed("trust") {
		out.Slack.Trust, _ = flags.GetStringSlice("trust")
	}
	if flags.Changed("post-to") {
		v, _ := flags.GetString("post-to")
		out.Slack.PostTo = strings.TrimSpace(v)
	}
	if flags.Changed("reply-as") {
		v, _ := flags.GetString("reply-as")
		if v = strings.TrimSpace(v); v != "bot" && v != "me" && v != "" {
			return nil, fmt.Errorf("--reply-as must be bot or me")
		}
		out.Slack.ReplyAs = v
	}
	return out, nil
}

func runAgentWatchStatus(_ *cobra.Command, _ []string) {
	dir := mustAgentDir()
	specs, err := loadWatchSpecs(dir)
	if err != nil {
		exitWithError("agent_watch", err, 1)
	}
	secrets := watch.LoadSecrets(dir)
	state := watch.LoadState(dir)
	now := time.Now()
	if utils.JSONOutput {
		utils.PrintJSON(watchStatusJSON(dir, specs, state, now))
		return
	}
	printWatchTokens(dir, secrets)
	if len(specs) == 0 {
		fmt.Println("\nNo watched workspaces. Inside one: corgi agent watch enable --labels bug --prs")
		return
	}
	printWatchedWorkspaces(specs, state.Fixes, now)
	printWatchPolls(state.Summaries())
	printWatchFixes(state.Fixes.RecentFixes("", 5), now)
	if n := countWatchEventsToday(dir); n > 0 {
		fmt.Printf("\n%d event(s) today — %s\n", n, filepath.Join(dir, "watch", "events.jsonl"))
	}
}

func printWatchTokens(dir string, secrets watch.Secrets) {
	fmt.Println("Tokens")
	fmt.Printf("  machine-wide  %s · webhook secret %s\n", tokenLine(secrets), watch.Fingerprint(secrets.HookSecret))
	for _, id := range watch.WorkspacesWithSecrets(dir) {
		fmt.Printf("  %-13s %s\n", id, tokenLine(watch.LoadSecretsFor(dir, id)))
	}
}

func printWatchedWorkspaces(specs []daemon.WatchSpec, fixes *watch.FixLog, now time.Time) {
	fmt.Println("\nWatched")
	for _, s := range specs {
		fmt.Println(watchedWorkspaceLine(s))
		if s.Action == "fix" {
			fmt.Printf("  %-20s %s\n", "", fixBudgetLine(s, fixes, now))
		} else if s.Quiet != "" {
			fmt.Printf("  %-20s quiet %s — held until the window opens\n", "", s.Quiet)
		}
		if line := chatStatusLine(s); line != "" {
			fmt.Printf("  %-20s %s\n", "", line)
		}
	}
}

func watchedWorkspaceLine(s daemon.WatchSpec) string {
	names := sourceNames(s)
	if len(names) == 0 {
		names = []string{"no source with a token"}
	}
	line := fmt.Sprintf("  %-20s %-8s every %-4s %s", s.Workspace, s.Action, s.Interval, strings.Join(names, ", "))
	if len(s.Skipped) > 0 {
		line += fmt.Sprintf(" (%s skipped: --prs off)", strings.Join(s.Skipped, ", "))
	}
	if len(s.DaysOff) > 0 {
		line += " · off " + daemon.DaysOffWords(s.DaysOff)
	}
	return line
}

func printWatchPolls(polls []watch.Summary) {
	if len(polls) == 0 {
		return
	}
	fmt.Println("\nLast polls")
	for _, p := range polls {
		line := fmt.Sprintf("  %-28s %s", p.Key, p.Polled)
		if p.Hooked != "" {
			line += " · webhook " + p.Hooked
		}
		if p.Error != "" {
			line += " ✗ " + p.Error
		}
		fmt.Println(line)
	}
}

func printWatchFixes(fixes []watch.FixRecord, now time.Time) {
	if len(fixes) == 0 {
		return
	}
	fmt.Println("\nLast fixes")
	for _, r := range fixes {
		fmt.Println(fixRecordLine(r, now))
	}
}

func fixRecordLine(r watch.FixRecord, now time.Time) string {
	line := fmt.Sprintf("  %-28s %s", firstNonEmptyString(r.Ref, r.Key), roughAge(now.Sub(r.StartedAt))+" ago")
	switch {
	case r.Error != "":
		line += " ✗ " + firstLineOf(r.Error)
	case len(r.PRs) > 0:
		line += " → " + strings.Join(r.PRs, " ")
	case r.Done():
		line += " → " + firstLineOf(r.Note)
	default:
		line += " · running"
	}
	return line
}

func loadWatchSpecs(dir string) ([]daemon.WatchSpec, error) {
	registry, err := workspace.Load(agentRegistryPath(dir))
	if err != nil {
		return nil, err
	}
	user, err := config.LoadUser(agentUserConfigPath(dir))
	if err != nil {
		return nil, err
	}
	var out []daemon.WatchSpec
	for _, w := range registry.Sorted() {
		repo, _ := config.LoadRepo(w.AbsPath)
		if spec, ok := watchSpecOf(dir, w, config.Resolve(w.ID, repo, user)); ok {
			out = append(out, spec)
		}
	}
	return out, nil
}

func watchSpecOf(dir string, w workspace.Workspace, resolved config.Resolved) (daemon.WatchSpec, bool) {
	wc := resolved.Watch
	if wc == nil || !wc.Enabled {
		if len(resolved.Routines) > 0 {
			return daemon.WatchSpec{Workspace: w.ID, Dir: w.AbsPath, ConfigDir: expandTilde(resolved.ConfigDir), Kind: resolved.Kind, Agents: resolved.AgentOrder(), Bin: expandTilde(resolved.Bin),
				SkipPermissions: resolved.DangerouslySkipPermissions, Models: resolved.Models, Routines: resolved.Routines}, true
		}
		return daemon.WatchSpec{}, false
	}
	secrets := watch.LoadSecretsFor(dir, w.ID)
	spec := daemon.WatchSpec{Workspace: w.ID, Dir: w.AbsPath, ConfigDir: expandTilde(resolved.ConfigDir), Kind: resolved.Kind, Agents: resolved.AgentOrder(), Bin: expandTilde(resolved.Bin), Project: wc.Project, Repos: wc.Repos,
		Rules:    watch.Rules{Enabled: true, Labels: wc.Labels, States: wc.States, Assignee: wc.Assignee, Comments: wc.Comments, PRs: wc.PRs, CI: wc.CI, Reviews: wc.Reviews, From: wc.From, Bots: wc.Bots},
		Interval: 3 * time.Minute, Action: "notify", SkipPermissions: resolved.DangerouslySkipPermissions,
		MaxFixesPerHour: wc.MaxFixesPerHour, MaxFixesPerDay: wc.MaxFixesPerDay, LimitCeiling: wc.LimitCeiling, MaxFixesTotal: wc.MaxFixesTotal, CapSince: wc.CapSince, Quiet: wc.Quiet, FixKinds: wc.FixKinds, DoneWhen: wc.DoneWhen, PlanReview: wc.PlanReview, Lease: wc.Lease, Isolate: wc.Isolate, Slots: wc.Slots, Batch: wc.Batch, RerunCI: wc.RerunCI, Silent: wc.Silent, NoRetry: wc.NoRetry, ReviewStatus: wc.ReviewStatus, Approve: wc.Approve, Models: resolved.Models, Routines: resolved.Routines}
	if wc.Action == "fix" {
		spec.Action = "fix"
	}
	spec.PruneAfter, _ = watch.ParseAge(wc.PruneAfter)
	if days, err := daemon.ParseDaysOff(wc.DaysOff); err == nil {
		spec.DaysOff = days
	} else {
		utils.Infof("agent: watch %s: daysOff ignored: %v\n", w.ID, err)
	}
	if wc.Chat != nil && wc.Chat.Slack != nil {
		spec.Chat = wc.Chat.Slack
		spec.Rules.Mentions = wc.Chat.Slack.Mentions
		spec.Rules.Channels = append(append([]string{}, wc.Chat.Slack.Channels...), wc.Chat.Slack.ReviewChannels...)
		if len(wc.Chat.Slack.ReviewChannels) > 0 {
			spec.Rules.Reviews = true
		}
	}
	spec.Skipped = spec.Rules.DeadSources()
	if wc.Chat == nil || wc.Chat.Slack == nil {
		spec.Skipped = withoutSource(spec.Skipped, "slack")
	}
	spec.Interval = watchInterval(wc.Interval, spec.Interval)
	spec.Sources = watchSources(spec.Rules, wc, secrets)
	return spec, true
}

func watchInterval(raw string, fallback time.Duration) time.Duration {
	if raw == "" {
		return fallback
	}
	if d, err := time.ParseDuration(raw); err == nil {
		return d
	}
	if raw == "0" {
		return 0
	}
	return fallback
}

func watchTracker(configured string, secrets watch.Secrets) string {
	if configured != "" {
		return configured
	}
	switch {
	case secrets.Linear != "":
		return "linear"
	case secrets.JiraToken != "":
		return "jira"
	}
	return ""
}

func watchSources(rules watch.Rules, wc *config.WatchConfig, secrets watch.Secrets) []watch.Source {
	var sources []watch.Source
	switch watchTracker(wc.Tracker, secrets) {
	case "linear":
		if secrets.Linear != "" {
			sources = append(sources, watch.NewLinear(secrets, wc.Project))
		}
	case "jira":
		if secrets.JiraToken != "" {
			sources = append(sources, watch.NewJira(secrets, wc.Project))
		}
	}
	if !rules.DeadSource("github") {
		if gh := watch.NewGitHub(secrets, wc.Repos); gh.Token != "" {
			sources = append(sources, gh)
		}
	}
	if !rules.DeadSource("gitlab") && secrets.GitLab != "" {
		sources = append(sources, watch.NewGitLab(secrets))
	}
	if !rules.DeadSource("slack") && wc.Chat != nil && wc.Chat.Slack != nil {
		if sl := watch.NewSlack(secrets, watch.SlackWatchConfig{
			Mentions:       wc.Chat.Slack.Mentions,
			Channels:       wc.Chat.Slack.Channels,
			ReviewChannels: wc.Chat.Slack.ReviewChannels,
		}); sl.Token() != "" {
			sources = append(sources, sl)
		}
	}
	return sources
}

func tokenLine(s watch.Secrets) string {
	return fmt.Sprintf("linear %s · jira %s · github %s · gitlab %s · slack %s",
		watch.Fingerprint(s.Linear), watch.Fingerprint(s.JiraToken), githubTokenLabel(s),
		watch.Fingerprint(s.GitLab), slackTokenLabel(s))
}

func slackTokenLabel(s watch.Secrets) string {
	switch {
	case s.SlackUser != "" && s.SlackBot != "":
		return watch.Fingerprint(s.SlackUser) + " +bot"
	case s.SlackUser != "":
		return watch.Fingerprint(s.SlackUser)
	case s.SlackBot != "":
		return "bot only " + watch.Fingerprint(s.SlackBot)
	}
	return watch.Fingerprint("")
}

func githubTokenLabel(secrets watch.Secrets) string {
	token, source := watch.GitHubToken(secrets)
	if source == "gh-auth" {
		return "gh-auth " + watch.Fingerprint(token)
	}
	return watch.Fingerprint(token)
}

func sourceNames(s daemon.WatchSpec) []string {
	var names []string
	for _, src := range s.Sources {
		names = append(names, src.Name())
	}
	return names
}

func fixBudgetLine(s daemon.WatchSpec, fixes *watch.FixLog, now time.Time) string {
	b := daemon.BudgetFor(s, fixes, now)
	line := fmt.Sprintf("caps %d/h %d/day · quiet %s · fixes today: %d", b.PerHour, b.PerDay, firstNonEmptyString(s.Quiet, "none"), b.Today)
	if len(s.FixKinds) > 0 {
		line = "auto for " + strings.Join(s.FixKinds, ", ") + " · " + line
	}
	if !b.Last.IsZero() {
		line += " (last " + b.Last.Local().Format("15:04") + ")"
	}
	if b.Deferred > 0 {
		line += fmt.Sprintf(" · %d deferred", b.Deferred)
	}
	return line
}

func watchHookHandler(source string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "read", http.StatusBadRequest)
			return
		}
		dir, err := agentDir()
		if err != nil {
			http.Error(w, "no agent dir", http.StatusInternalServerError)
			return
		}
		secrets := watch.LoadSecrets(dir)
		if err := watch.VerifyHook(source, r, body, secrets.HookSecret); err != nil {
			http.Error(w, "signature", http.StatusUnauthorized)
			return
		}
		who := watch.HookIdentity{Me: watchIdentity(dir, source, secrets), ID: watch.LoadState(dir).SourceIdentity(source, "meId")}
		events, err := watch.ParseHookAs(source, r, body, who)
		if err != nil {
			http.Error(w, "payload", http.StatusBadRequest)
			return
		}
		info, _ := daemon.ReadInfo(dir)
		queued := 0
		for i := range events {
			e := events[i]
			if _, err := command.Write(dir, command.Command{Action: command.ActionWatch, Source: "webhook", WatchEvent: &e}); err == nil {
				queued++
			}
		}
		if info != nil && queued > 0 {
			daemon.Nudge(info)
		}
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]int{"queued": queued})
	}
}

func watchIdentity(dir, source string, secrets watch.Secrets) string {
	state := watch.LoadState(dir)
	for key, c := range state.Cursors {
		if strings.HasSuffix(key, "/"+source) && c["me"] != "" {
			return c["me"]
		}
	}
	return secrets.Me
}

func prRefURL(ref string) string {
	if repo, num, ok := strings.Cut(ref, "!"); ok {
		return "https://gitlab.com/" + repo + "/-/merge_requests/" + firstNonEmptyString(num, "1")
	}
	repo, num, _ := strings.Cut(ref, "#")
	return "https://github.com/" + repo + "/pull/" + firstNonEmptyString(num, "1")
}

func watchTargetWorkspace(dir string, flags *pflag.FlagSet) (string, error) {
	id, _ := flags.GetString("workspace")
	if id = strings.TrimSpace(id); id == "" {
		return currentWorkspaceID(dir)
	}
	registry, err := workspace.Load(agentRegistryPath(dir))
	if err != nil {
		return "", err
	}
	for _, w := range registry.Sorted() {
		if w.ID == id {
			return id, nil
		}
	}
	return "", fmt.Errorf("%q is not a registered workspace — `corgi agent init` there first", id)
}

func currentWorkspaceID(dir string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	registry, err := workspace.Load(agentRegistryPath(dir))
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	best, bestLen := "", 0
	for _, w := range registry.Sorted() {
		path := w.AbsPath
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			path = resolved
		}
		if path != "" && (cwd == path || strings.HasPrefix(cwd, path+string(os.PathSeparator))) && len(path) > bestLen {
			best, bestLen = w.ID, len(path)
		}
	}
	if best == "" {
		return "", fmt.Errorf("%s is not a registered workspace — `corgi agent init` here first", cwd)
	}
	return best, nil
}

func describeWatch(wc *config.WatchConfig) string {
	action := firstNonEmptyString(wc.Action, "notify")
	out := strings.Join(describeWatchParts(wc), " · ") + " → " + action
	if action == "fix" {
		out += describeWatchCaps(wc)
	}
	return out
}

func describeWatchParts(wc *config.WatchConfig) []string {
	var parts []string
	if len(wc.Labels) > 0 {
		parts = append(parts, "labels "+strings.Join(wc.Labels, ","))
	}
	if wc.Assignee == "any" {
		parts = append(parts, "any assignee")
	} else {
		parts = append(parts, "assigned to me")
	}
	for _, p := range []struct {
		on   bool
		text string
	}{
		{wc.Comments, "issue comments"},
		{wc.PRs, "PR reviews and comments"},
		{wc.HandOver, "handed to the session on the branch"},
		{wc.AutoMerge, "merged when green and approved"},
		{wc.AfterMerge != "", "then the ticket goes to " + wc.AfterMerge + subtaskColumn(wc.AfterMergeSubtasks)},
		{wc.Batch > 1, fmt.Sprintf("up to %d tickets arriving together share one run", wc.Batch)},
		{wc.Approve, "review requests approved when clean"},
		{wc.Bots, "bot comments count"},
		{wc.AutoAllow == config.AutoAllowReads, "reads allowed by policy"},
		{len(wc.DoneWhen) > 0, "done when " + strings.Join(wc.DoneWhen, " and ")},
		{wc.CompactAt > 0, fmt.Sprintf("/compact past %d%%", wc.CompactAt)},
		{wc.Rebase, "rebased when main moves"},
		{wc.Lessons, "lessons written down"},
		{wc.PlanReview != "", "plan reviewed " + planReviewWords(wc.PlanReview)},
	} {
		if p.on {
			parts = append(parts, p.text)
		}
	}
	return parts
}

func planReviewWords(policy string) string {
	if policy == config.PlanReviewAlways {
		return "before every story"
	}
	return "when the forecast is " + strings.TrimPrefix(policy, "risk") + " of 10"
}

func describeWatchCaps(wc *config.WatchConfig) string {
	perHour, perDay := daemon.WatchSpec{MaxFixesPerHour: wc.MaxFixesPerHour, MaxFixesPerDay: wc.MaxFixesPerDay}.FixCaps()
	out := fmt.Sprintf(", at most %d/h %d/day", perHour, perDay)
	if wc.MaxFixesTotal > 0 {
		out += fmt.Sprintf(", %d this trip", wc.MaxFixesTotal)
	}
	if len(wc.DaysOff) > 0 {
		out += ", off " + strings.Join(wc.DaysOff, "/")
	}
	if wc.Quiet != "" {
		out += ", quiet " + wc.Quiet
	}
	return out
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func randomSecret() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		exitWithError("agent_watch_secret", err, 1)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func countWatchEventsToday(dir string) int {
	data, err := os.ReadFile(filepath.Join(dir, "watch", "events.jsonl"))
	if err != nil {
		return 0
	}
	today := time.Now().Local().Format("2006-01-02")
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		var e struct {
			At time.Time `json:"at"`
		}
		if json.Unmarshal([]byte(line), &e) == nil && e.At.Local().Format("2006-01-02") == today {
			n++
		}
	}
	return n
}

func init() {
	f := agentWatchEnableCmd.Flags()
	f.String("labels", "", "Only issues with one of these labels (comma-separated), e.g. bug,defect")
	f.String("states", "", "Only issues in one of these states, e.g. Todo,Backlog")
	f.String("assignee", "", "me (default) or any")
	f.String("project", "", "Linear team key or Jira project key, e.g. ABC")
	f.String("tracker", "", "linear or jira (default: whichever has a token)")
	f.String("repos", "", "GitHub repos to watch for PR feedback, comma-separated owner/repo (default: any)")
	f.String("interval", "", "Poll interval, e.g. 3m; 0 means webhooks only")
	f.String("action", "", "notify (default) or fix — fix starts a headless claude with the matching skill, draft PRs only")
	f.String("workspace", "", "Workspace id to change; omitted means the one you are in")
	agentWatchDisableCmd.Flags().String("workspace", "", "Workspace id to stop watching; omitted means the one you are in")
	f.Bool("auto", false, "Shorthand for --action fix --prs --comments (not --reviews: reviewing someone else's PR is a separate ask): work on what arrives without being asked, draft PRs only")
	f.Bool("comments", false, "Also new comments on issues assigned to me")
	f.Bool("prs", false, "Also reviews and comments on pull requests I opened")
	f.Int(watchFlagMaxPerHour, 0, "With --action fix: at most this many fixes an hour (default 3); more are deferred")
	f.Int(watchFlagMaxPerDay, 0, "With --action fix: at most this many fixes a day (default 10)")
	f.Int(watchFlagLimitCeiling, 0, "With --action fix: the usage-window percent past which no run starts here (default 95; 100 never waits on usage, only on the caps)")
	f.Int(watchFlagMaxTotal, 0, "With --action fix: at most this many fixes in total, counted from now — a cap for a trip; run enable --max-total again to reset, 0 clears")
	f.String("quiet", "", "Local hours to stay quiet in, e.g. 23:00-07:00: no fix starts and nothing buzzes; one summary when it opens")
	f.String(watchFlagDaysOff, "", "Days the watch sleeps through — weekends, or sat,sun, or mon,fri: no polling, no fix, nothing rings until the next working day (none clears)")
	f.String("pickup", "", "Column a ticket moves to when it is picked up, e.g. \"In Progress\"; empty writes nothing")
	f.String(watchFlagReviewStatus, "", "Column a ticket moves to once a run opened a pull request for it, e.g. \"In Review\"")
	f.Bool("lease", false, "Claim a ticket on the tracker before working it, so a second machine watching the same board leaves it alone")
	f.Bool("isolate", false, "Give every unattended run its own worktrees on a corgi/<ref> branch, so it never touches your checkout")
	f.String("kind", "", "The agent that runs here: claude (default) or codex; also what corgi agent claude opens in this workspace")
	f.StringSlice("agents", nil, "The agents to try in order, e.g. claude,codex: the next takes a run when the first cannot")
	f.String(watchFlagPruneAfter, "", "Remove an isolated run's worktrees this long after it finished, e.g. 7d; the branch stays, a dirty worktree stays (empty keeps them until `watch undo` or `watch prune`)")
	f.Bool(watchFlagNoRetry, false, "Leave deferred fixes to a manual `watch run` instead of starting them when the budget returns")
	f.Bool("reviews", false, "Also pull requests someone asked me to review — theirs, not mine")
	f.Bool(watchFlagAutoMerge, false, "Merge a pull request of mine the moment its checks pass and it is approved (read from the forge once a round)")
	f.Int("batch", 1, "Tickets that arrive within 90 s of each other share one run, up to this many (1 is off); one preflight and one context for the lot")
	f.String(watchFlagAfterMerge, "", "Move the ticket to this column once every pull request of its run is merged — a name from `corgi agent watch board`; empty leaves it where it is")
	f.String(watchFlagAfterMergeSubtasks, "", "Where a subtask goes instead when its pull request merges (Done, say); empty means the same column as --after-merge")
	f.Bool("bots", false, "Comments from bot accounts count too (a review bot whose findings are to be fixed); off, a bot is not a person waiting")
	f.Bool("approve", false, "An unattended review of a pull request I was asked to review may approve it when nothing blocks and the risk card allows")
	f.Bool(watchFlagHandOver, false, "Type a review comment, a red build or an asked-for review into the session already on that branch")
	f.Bool("headless", false, "Let a message for a session whose terminal is gone run as one headless turn (claude -p --resume, or codex exec resume; acceptEdits) in its own checkout, so the phone's chat keeps working")
	f.Bool("silent", false, "Nothing about this workspace's watch rings — no toast, no phone push: fixes run, the inbox and the kanban fill, and you look when you like (--silent=false to ring again)")
	f.Bool(watchFlagRerunCI, false, "Rerun the failed jobs of a red build once before it is worked on or handed over; a second red on the same run goes the usual way (GitHub)")
	f.Bool(watchFlagAutoCarry, false, "Carry a session that hit its five-hour quota to another of the workspace's accounts with budget, once per limit (only profiles the accounts list names)")
	f.Int("slots", 1, "How many unattended runs may go at once in this workspace (1 to 8); above 1 turns on --isolate so each has worktrees of its own")
	f.Bool("lessons", false, "Write what the workspace learned the hard way — a review on a PR of mine, a check that stayed red, a bot that failed — one line each for every new session to read (corgi agent lesson list)")
	f.Bool("rebase", false, "Rebase a session's branch onto main where it sits when the session stops behind main with a clean tree and no conflicts (a branch that would conflict is typed into the session under --hand-over)")
	f.Int(watchFlagCompactAt, 0, "Type /compact into a session past this much context the next time it stops — 85 is where the board goes red; 0 is off")
	f.String(watchFlagDoneWhen, "", "What finished means here, comma separated: commands run in the session's directory when it stops with changes — `go test ./...,pnpm lint`; a red one is typed back as the next message. Empty is off")
	f.String(watchFlagPlanReview, "", "Stop for a human before code on a story: always, risk>=N (the story's forecast, 1 to 10), or off. Off means the stories skill gates as today; on, the spec waits for an answer — from the terminal or the phone — before a branch is cut")
	f.String(watchFlagAutoAllow, "", "Answer a permission prompt for a tool that only reads — Read, Grep, Glob, a web search — on the daemon's own: reads, or off (Bash always waits for a person; iTerm2 and tmux sessions only)")
	f.Bool("ci", false, "Also builds that went red on something of mine — the one kind that brings its own test for done")
	f.String("from", "", "Only comments and reviews from these people (comma separated); empty is anyone")
	f.Bool("mentions", false, "Ring when someone names you in Slack or writes to you directly (needs corgi agent watch auth slack)")
	f.StringSlice("channel", nil, "Slack channels every message of which is news, e.g. #incidents (repeatable)")
	f.StringSlice("review-channel", nil, "Slack channels where pull requests are posted for review: a post with links is one review, answered in its thread (repeatable)")
	f.StringSlice("trust", nil, "Colleagues whose Slack mention may start an unattended run — the person who WROTE the message, e.g. @teammate (repeatable). Empty means nobody: a mention only rings")
	f.String("post-to", "", "Default Slack channel for corgi agent chat post")
	f.String("reply-as", "", "Whose voice a reply speaks in: bot or me (default: the bot when a bot token is stored)")
	f.String(watchFlagAutoFor, "", "With --action fix, what to work on unattended: tickets, comments, reviews (comma separated). Empty means everything")
	agentWatchRunCmd.Flags().Bool(watchFlagDryRun, false, "Do not advance the saved cursors")
	agentWatchSweepCmd.Flags().Bool(watchFlagDryRun, false, "Print what would be handed over, hand nothing")
	agentWatchSweepCmd.Flags().String("workspace", "", "Only this workspace")
	agentWatchSweepCmd.Flags().StringSlice("states", nil, "Only tickets in these columns (comma separated); default: the workspace's watched columns")
	agentWatchCmd.AddCommand(agentWatchSweepCmd)
	tf := agentWatchTestCmd.Flags()
	tf.String("ref", "", "Issue key (ABC-12) or PR (owner/repo#12); default: one shaped for the first watched workspace")
	tf.String("url", "", "PR URL, for pr.* kinds")
	tf.String("body", "", "Comment or review text")
	tf.StringSlice("labels", nil, "Labels on the made-up issue (default: the workspace's own, so the rules take it)")
	agentWatchHooksCmd.Flags().Bool("rotate", false, "Make a new secret")
	agentWatchHooksCmd.Flags().Bool("install", false, "Create or update the webhook on every GitHub and GitLab repo the workspace watches")
	agentWatchHooksCmd.Flags().String("workspace", "", "The workspace to plan for (default: the one this folder is in)")
	a := agentWatchAuthCmd.Flags()
	a.String("token", "", "API token")
	a.String("url", "", "Jira site (https://you.atlassian.net) or self-hosted GitLab URL")
	a.String("email", "", "Jira account email")
	a.String("bot", "", "Slack bot token (xoxb-…), so replies come from the app rather than from you")
	a.String("me", "", "Your login or id on the service, for webhooks before the first poll")
	a.Bool("local", false, "Store for the workspace you are in, not the machine")
	a.String("workspace", "", "Store for this workspace id, from anywhere")
	a.Bool("clear", false, "Remove the token instead of setting one")
	agentWatchCmd.AddCommand(agentWatchEnableCmd, agentWatchDisableCmd, agentWatchRunCmd, agentWatchTestCmd, agentWatchHooksCmd, agentWatchAuthCmd)
	agentCmd.AddCommand(agentWatchCmd)
}

type watchStatusWorkspace struct {
	Workspace  string           `json:"workspace"`
	Sources    []string         `json:"sources"`
	Skipped    []string         `json:"skipped,omitempty"`
	Action     string           `json:"action"`
	AutoFor    []string         `json:"autoFor,omitempty"`
	Interval   string           `json:"interval"`
	Quiet      string           `json:"quiet,omitempty"`
	DaysOff    []string         `json:"daysOff,omitempty"`
	Asleep     bool             `json:"asleep,omitempty"`
	PlanReview string           `json:"planReview,omitempty"`
	Fixes      daemon.FixBudget `json:"fixes"`
}

type watchStatusFix struct {
	Key       string    `json:"key,omitempty"`
	Ref       string    `json:"ref"`
	Workspace string    `json:"workspace"`
	Kind      string    `json:"kind,omitempty"`
	URL       string    `json:"url,omitempty"`
	StartedAt time.Time `json:"startedAt"`
	Running   bool      `json:"running"`
	PRs       []string  `json:"prs,omitempty"`
	Note      string    `json:"note,omitempty"`
	Error     string    `json:"error,omitempty"`
}

type watchStatusEvent struct {
	Key       string            `json:"key"`
	Ref       string            `json:"ref"`
	Kind      string            `json:"kind"`
	Workspace string            `json:"workspace,omitempty"`
	Title     string            `json:"title,omitempty"`
	URL       string            `json:"url,omitempty"`
	State     string            `json:"state,omitempty"`
	At        time.Time         `json:"at"`
	Blocked   string            `json:"blocked,omitempty"`
	Session   *CardSess         `json:"session,omitempty"`
	Picked    *CardPick         `json:"picked,omitempty"`
	Columns   []string          `json:"columns,omitempty"`
	PR        string            `json:"pr,omitempty"`
	Pull      *watch.PullStatus `json:"pull,omitempty"`
	Handed    *watch.Hand       `json:"handed,omitempty"`
	Standing  sessions.Standing `json:"standing"`
}

func watchStatusJSON(dir string, specs []daemon.WatchSpec, state *watch.State, now time.Time) map[string]any {
	workspaces := watchStatusWorkspaces(specs, state, now)
	fixes := watchStatusFixes(dir, state)
	events := watchStatusEvents(dir, state, now)
	return map[string]any{"workspaces": workspaces, "polls": state.Summaries(), "fixes": fixes, "events": events}
}

func watchStatusWorkspaces(specs []daemon.WatchSpec, state *watch.State, now time.Time) []watchStatusWorkspace {
	var out []watchStatusWorkspace
	for _, s := range specs {
		row := watchStatusWorkspace{Workspace: s.Workspace, Sources: sourceNames(s), Skipped: s.Skipped, Action: s.Action, AutoFor: s.FixKinds, Interval: s.Interval.String(), Quiet: s.Quiet, PlanReview: s.PlanReview, Fixes: daemon.BudgetFor(s, state.Fixes, now)}
		for _, d := range s.DaysOff {
			row.DaysOff = append(row.DaysOff, strings.ToLower(d.String()[:3]))
		}
		row.Asleep = daemon.DayOff(s, now)
		out = append(out, row)
	}
	return out
}

func watchStatusFixes(dir string, state *watch.State) []watchStatusFix {
	fixes := []watchStatusFix{}
	for _, r := range state.Fixes.RecentFixes("", 20) {
		row := watchStatusFix{Key: r.Key, Ref: firstNonEmptyString(r.Ref, r.Key), Workspace: r.Workspace, Kind: r.Kind,
			StartedAt: r.StartedAt, Running: !r.Done(), PRs: r.PRs, Note: r.Note, Error: r.Error}
		if e, ok := watch.FindEvent(dir, r.Key); ok {
			row.URL = e.URL
		}
		fixes = append(fixes, row)
	}
	return fixes
}

type watchInbox struct {
	dir      string
	now      time.Time
	state    *watch.State
	onTicket map[string]*CardSess
	picks    *watch.PickLog
	pulls    *watch.PullLog
	hands    *watch.HandLog
	moved    *watch.StateLog
}

func watchStatusEvents(dir string, state *watch.State, now time.Time) []watchStatusEvent {
	inbox := watchInbox{dir: dir, now: now, state: state, onTicket: sessionsOnTickets(dir),
		picks: watch.LoadPicks(dir), pulls: watch.LoadPullLog(dir), hands: watch.LoadHands(dir), moved: watch.LoadStateLog(dir)}
	keeper := watch.NewInboxKeeper(now)
	events := []watchStatusEvent{}
	for _, e := range watch.RecentEvents(dir, 25) {
		if !keeper.Keep(e) || state.IsIgnored(e.Key) {
			continue
		}
		if row, ok := inbox.row(e); ok {
			events = append(events, row)
		}
	}
	return events
}

func (in watchInbox) row(e watch.Event) (watchStatusEvent, bool) {
	current := e.State
	if st, ok := in.moved.Get(e.Key); ok {
		current = st.Status
	}
	if watch.Settled(e, current) != "" {
		return watchStatusEvent{}, false
	}
	er := watchStatusEvent{Key: e.Key, Ref: e.Ref, Kind: string(e.Kind),
		Workspace: e.Workspace, Title: firstLineOf(e.Title), URL: e.URL, State: current, At: e.At}
	if b, ok := in.state.Fixes.Blocked(e.Workspace, e.Ref); ok {
		er.Blocked = b.Reason
	}
	er.Session = in.onTicket[strings.ToLower(e.Ref)]
	if p, ok := in.picks.Get(e.Key); ok && in.now.Sub(p.At) <= watch.PickFresh {
		er.Picked = &CardPick{At: p.At, By: p.By}
	}
	if e.Kind == watch.KindTask {
		er.Columns = watch.TaskColumns
	}
	er.PR = prLinkFor(in.dir, e, in.onTicket)
	if st, ok := in.pulls.Get(firstNonEmptyString(er.PR, e.Ref)); ok {
		p := st
		er.Pull = &p
	}
	if h, ok := in.hands.Get(e.Key); ok {
		hh := h
		er.Handed = &hh
	}
	er.Standing = rowStanding(er.Pull, er.PR, er.Blocked, er.Session)
	return er, true
}

func launchWatchStatusHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodGet {
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	specs, err := loadWatchSpecs(dir)
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeLaunchJSON(w, watchStatusJSON(dir, specs, watch.LoadState(dir), time.Now()))
}

func launchStatusHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodGet {
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	status, err := daemon.ReadStatus(dir)
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if status == nil {
		status = &daemon.Status{Running: false, WakeLockable: supervisor.Supported()}
	}
	writeLaunchJSON(w, statusWithUsage(dir, status))
}

func subtaskColumn(status string) string {
	if strings.TrimSpace(status) == "" {
		return ""
	}
	return " (subtasks to " + status + ")"
}
