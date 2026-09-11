package cmd

import (
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
	"andriiklymiuk/corgi/utils/agent/watch"
	"andriiklymiuk/corgi/utils/agent/workspace"
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
		// --auto is the whole unattended mode in one flag: act on everything
		// this workspace is told about, not just be told.
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
		if flags.Changed("max-per-hour") {
			if wc.MaxFixesPerHour, _ = flags.GetInt("max-per-hour"); wc.MaxFixesPerHour < 1 {
				return fmt.Errorf("--max-per-hour must be at least 1")
			}
		}
		if flags.Changed("max-per-day") {
			if wc.MaxFixesPerDay, _ = flags.GetInt("max-per-day"); wc.MaxFixesPerDay < 1 {
				return fmt.Errorf("--max-per-day must be at least 1")
			}
		}
		if flags.Changed("review-status") {
			v, _ := flags.GetString("review-status")
			wc.ReviewStatus = strings.TrimSpace(v)
		}
		if flags.Changed("isolate") {
			wc.Isolate, _ = flags.GetBool("isolate")
		}
		if flags.Changed("no-retry") {
			wc.NoRetry, _ = flags.GetBool("no-retry")
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
		if flags.Changed("from") {
			v, _ := flags.GetString("from")
			wc.From = splitList(v)
		}
		if flags.Changed("auto-for") {
			v, _ := flags.GetString("auto-for")
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
		entry.Watch = wc
		user.Workspaces[id] = entry
		if err := writeUserConfig(path, user); err != nil {
			return err
		}
		utils.Infof("watching %s — %s\n", id, describeWatch(wc))
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
		dryRun, _ := cmd.Flags().GetBool("dry-run")
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

// handBackDeferred gives the daemon the fixes it deferred, from a person's
// hand: it decides again against the caps of the moment. Without a daemon
// they are listed, so the person can run the skill instead.
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

// synthesizeWatchEvent is a believable event of one kind, shaped for the
// first watched workspace unless flags say otherwise.
func synthesizeWatchEvent(kind watch.Kind, specs []daemon.WatchSpec, ref, url, body string) (watch.Event, error) {
	first := specs[0]
	e := watch.Event{Kind: kind, Ref: ref, URL: url, Body: body, Title: "watch test", Author: "watch test", Mine: true, At: time.Now()}
	switch kind {
	case watch.KindIssueNew, watch.KindIssueComment:
		e.Source = "linear"
		for _, src := range first.Sources {
			if src.Name() == "jira" {
				e.Source = "jira"
			}
		}
		if e.Ref == "" {
			e.Ref = firstNonEmptyString(first.Project, "TEST") + "-1"
		}
		if kind == watch.KindIssueComment && e.Body == "" {
			e.Body = "could you also cover the empty state?"
		}
	case watch.KindPRComment, watch.KindPRReview, watch.KindReviewRequested:
		e.Source = "github"
		if e.Ref == "" {
			repo := "acme/api"
			if len(first.Repos) > 0 {
				repo = first.Repos[0]
			}
			e.Ref = repo + "#1"
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
		if kind == watch.KindPRReview {
			e.State = "changes_requested"
		}
		if kind == watch.KindReviewRequested {
			// Someone else's pull request: not mine, which is the whole
			// difference between reviewing it and fixing my own.
			e.Mine = false
			e.Author = "a colleague"
			e.Title = "Retry the upload on a 502"
		}
	case watch.KindCIFailed:
		e.Source = "github"
		if e.Ref == "" {
			e.Ref = "acme/api"
			if len(first.Repos) > 0 {
				e.Ref = first.Repos[0]
			}
		}
		if e.URL == "" {
			e.URL = "https://github.com/" + e.Ref + "/actions"
		}
		e.Title = "e2e / checkout failed"
	default:
		return e, fmt.Errorf("kind %q — want issue.new, issue.comment, pr.comment, pr.review, review.requested or ci.failed", kind)
	}
	e.Key = "test:" + string(kind) + ":" + e.Ref
	return e, nil
}

func firstNonEmptyString(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

var agentWatchHooksCmd = &cobra.Command{
	Use:   "hooks",
	Short: "Webhook URLs and the shared secret, for instant events without polling",
	RunE: func(cmd *cobra.Command, _ []string) error {
		dir := mustAgentDir()
		secrets := watch.LoadSecrets(dir)
		rotate, _ := cmd.Flags().GetBool("rotate")
		if secrets.HookSecret == "" || rotate {
			secrets.HookSecret = randomSecret()
			if err := watch.SaveSecrets(dir, secrets); err != nil {
				return err
			}
		}
		base := launcherURL()
		if base == "" {
			utils.Info("no public URL yet — `corgi agent up` first; a named tunnel keeps these URLs stable")
			base = "https://<your-tunnel>"
		} else {
			base = strings.TrimSuffix(base, "/app")
		}
		fmt.Printf("secret: %s\n\n", secrets.HookSecret)
		fmt.Printf("Linear   Settings → API → Webhooks: %s/hooks/linear\n         secret above; events: Issues, Comments\n", base)
		fmt.Printf("GitHub   repo Settings → Webhooks: %s/hooks/github\n         content type json, secret above; events: Pull request reviews, Pull request review comments, Issue comments\n", base)
		fmt.Printf("GitLab   project Settings → Webhooks: %s/hooks/gitlab\n         secret token above; trigger: Comments\n", base)
		fmt.Printf("Jira     Settings → System → WebHooks: %s/hooks/jira?token=%s\n         events: Issue created, Comment created\n", base, secrets.HookSecret)
		return nil
	},
}

var agentWatchAuthCmd = &cobra.Command{
	Use:   "auth <linear|jira|github|gitlab>",
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
		type spec struct {
			Workspace string           `json:"workspace"`
			Sources   []string         `json:"sources"`
			Skipped   []string         `json:"skipped,omitempty"`
			Action    string           `json:"action"`
			AutoFor   []string         `json:"autoFor,omitempty"`
			Interval  string           `json:"interval"`
			Quiet     string           `json:"quiet,omitempty"`
			Fixes     daemon.FixBudget `json:"fixes"`
		}
		type fixRow struct {
			Ref       string    `json:"ref"`
			Workspace string    `json:"workspace"`
			Kind      string    `json:"kind,omitempty"`
			StartedAt time.Time `json:"startedAt"`
			Running   bool      `json:"running"`
			PRs       []string  `json:"prs,omitempty"`
			Note      string    `json:"note,omitempty"`
			Error     string    `json:"error,omitempty"`
		}
		var out []spec
		for _, s := range specs {
			out = append(out, spec{s.Workspace, sourceNames(s), s.Skipped, s.Action, s.FixKinds, s.Interval.String(), s.Quiet, daemon.BudgetFor(s, state.Fixes, now)})
		}
		fixes := []fixRow{}
		for _, r := range state.Fixes.RecentFixes("", 20) {
			fixes = append(fixes, fixRow{firstNonEmptyString(r.Ref, r.Key), r.Workspace, r.Kind,
				r.StartedAt, !r.Done(), r.PRs, r.Note, r.Error})
		}
		// The inbox itself, so a menu bar or an editor can show what arrived
		// without reading the log file or asking the phone.
		type eventRow struct {
			Key       string    `json:"key"`
			Ref       string    `json:"ref"`
			Kind      string    `json:"kind"`
			Workspace string    `json:"workspace,omitempty"`
			Title     string    `json:"title,omitempty"`
			URL       string    `json:"url,omitempty"`
			State     string    `json:"state,omitempty"`
			At        time.Time `json:"at"`
			Blocked   string    `json:"blocked,omitempty"`
		}
		events := []eventRow{}
		moved := watch.LoadStateLog(dir)
		keeper := watch.NewInboxKeeper(now)
		for _, e := range watch.RecentEvents(dir, 25) {
			if state.IsIgnored(e.Key) || !keeper.Keep(e) {
				continue // dismissed, or an old routine report: not waiting on anyone
			}
			// Merged, closed, done: history rather than work. The same test
			// the phone's inbox uses, so the menu bar and the editor do not
			// disagree with it about what is waiting.
			current := e.State
			if now, ok := moved.Get(e.Key); ok {
				current = now.Status
			}
			if watch.Settled(e, current) != "" {
				continue
			}
			er := eventRow{Key: e.Key, Ref: e.Ref, Kind: string(e.Kind),
				Workspace: e.Workspace, Title: firstLineOf(e.Title), URL: e.URL, State: current, At: e.At}
			if b, ok := state.Fixes.Blocked(e.Workspace, e.Ref); ok {
				er.Blocked = b.Reason
			}
			events = append(events, er)
		}
		utils.PrintJSON(map[string]any{"workspaces": out, "polls": state.Summaries(), "fixes": fixes, "events": events})
		return
	}
	fmt.Println("Tokens")
	fmt.Printf("  machine-wide  %s · webhook secret %s\n", tokenLine(secrets), watch.Fingerprint(secrets.HookSecret))
	for _, id := range watch.WorkspacesWithSecrets(dir) {
		fmt.Printf("  %-13s %s\n", id, tokenLine(watch.LoadSecretsFor(dir, id)))
	}
	if len(specs) == 0 {
		fmt.Println("\nNo watched workspaces. Inside one: corgi agent watch enable --labels bug --prs")
		return
	}
	fmt.Println("\nWatched")
	for _, s := range specs {
		names := sourceNames(s)
		if len(names) == 0 {
			names = []string{"no source with a token"}
		}
		line := fmt.Sprintf("  %-20s %-8s every %-4s %s", s.Workspace, s.Action, s.Interval, strings.Join(names, ", "))
		if len(s.Skipped) > 0 {
			line += fmt.Sprintf(" (%s skipped: --prs off)", strings.Join(s.Skipped, ", "))
		}
		fmt.Println(line)
		if s.Action == "fix" {
			fmt.Printf("  %-20s %s\n", "", fixBudgetLine(s, state.Fixes, now))
		} else if s.Quiet != "" {
			// Quiet hours hold the notification too, so a reporting watch
			// has to say when it goes quiet or it looks broken in the evening.
			fmt.Printf("  %-20s quiet %s — held until the window opens\n", "", s.Quiet)
		}
	}
	polls := state.Summaries()
	if len(polls) > 0 {
		fmt.Println("\nLast polls")
		for _, p := range polls {
			line := fmt.Sprintf("  %-28s %s", p.Key, p.Polled)
			if p.Error != "" {
				line += " ✗ " + p.Error
			}
			fmt.Println(line)
		}
	}
	if fixes := state.Fixes.RecentFixes("", 5); len(fixes) > 0 {
		fmt.Println("\nLast fixes")
		for _, r := range fixes {
			what := r.Ref
			if what == "" {
				what = r.Key
			}
			line := fmt.Sprintf("  %-28s %s", what, roughAge(now.Sub(r.StartedAt))+" ago")
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
			fmt.Println(line)
		}
	}

	if n := countWatchEventsToday(dir); n > 0 {
		fmt.Printf("\n%d event(s) today — %s\n", n, filepath.Join(dir, "watch", "events.jsonl"))
	}
}

// loadWatchSpecs builds the daemon's watches from config and tokens. A
// workspace with watch enabled but no token for any source still appears,
// so the status can say why nothing happens.
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
		resolved := config.Resolve(w.ID, repo, user)
		wc := resolved.Watch
		if wc == nil || !wc.Enabled {
			// No watch, but routines on a clock still need a spec to run
			// under: the caps and the log, no sources, no polling.
			if len(resolved.Routines) > 0 {
				out = append(out, daemon.WatchSpec{Workspace: w.ID, Dir: w.AbsPath, ConfigDir: expandTilde(resolved.ConfigDir),
					SkipPermissions: resolved.DangerouslySkipPermissions, Models: resolved.Models, Routines: resolved.Routines})
			}
			continue
		}
		secrets := watch.LoadSecretsFor(dir, w.ID)
		spec := daemon.WatchSpec{Workspace: w.ID, Dir: w.AbsPath, ConfigDir: expandTilde(resolved.ConfigDir), Project: wc.Project, Repos: wc.Repos,
			Rules:    watch.Rules{Enabled: true, Labels: wc.Labels, States: wc.States, Assignee: wc.Assignee, Comments: wc.Comments, PRs: wc.PRs, CI: wc.CI, Reviews: wc.Reviews, From: wc.From},
			Interval: 3 * time.Minute, Action: "notify", SkipPermissions: resolved.DangerouslySkipPermissions,
			MaxFixesPerHour: wc.MaxFixesPerHour, MaxFixesPerDay: wc.MaxFixesPerDay, Quiet: wc.Quiet, FixKinds: wc.FixKinds, Lease: wc.Lease, Isolate: wc.Isolate, NoRetry: wc.NoRetry, ReviewStatus: wc.ReviewStatus, Models: resolved.Models, Routines: resolved.Routines}
		if wc.Action == "fix" {
			spec.Action = "fix"
		}
		// A source the rules take nothing from is not built: polling it would
		// only spend requests.
		spec.Skipped = spec.Rules.DeadSources()
		if wc.Interval != "" {
			if d, err := time.ParseDuration(wc.Interval); err == nil {
				spec.Interval = d
			} else if wc.Interval == "0" {
				spec.Interval = 0
			}
		}
		tracker := wc.Tracker
		if tracker == "" {
			switch {
			case secrets.Linear != "":
				tracker = "linear"
			case secrets.JiraToken != "":
				tracker = "jira"
			}
		}
		switch tracker {
		case "linear":
			if secrets.Linear != "" {
				spec.Sources = append(spec.Sources, watch.NewLinear(secrets, wc.Project))
			}
		case "jira":
			if secrets.JiraToken != "" {
				spec.Sources = append(spec.Sources, watch.NewJira(secrets, wc.Project))
			}
		}
		if !spec.Rules.DeadSource("github") {
			if gh := watch.NewGitHub(secrets, wc.Repos); gh.Token != "" {
				spec.Sources = append(spec.Sources, gh)
			}
		}
		if !spec.Rules.DeadSource("gitlab") && secrets.GitLab != "" {
			spec.Sources = append(spec.Sources, watch.NewGitLab(secrets))
		}
		out = append(out, spec)
	}
	return out, nil
}

func tokenLine(s watch.Secrets) string {
	return fmt.Sprintf("linear %s · jira %s · github %s · gitlab %s",
		watch.Fingerprint(s.Linear), watch.Fingerprint(s.JiraToken), githubTokenLabel(s), watch.Fingerprint(s.GitLab))
}

// githubTokenLabel is the fingerprint, marked gh-auth when the gh CLI is
// the only place a token comes from.
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

// fixBudgetLine is "caps 3/h 10/day · quiet 23:00-07:00 · fixes today: 2 (last 14:05) · 1 deferred".
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

// watchHookHandler receives one service's webhook, checks its signature
// and hands the events to the daemon through the spool. The MCP process
// answers the HTTP; the daemon owns the rules and the seen list.
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
		events, err := watch.ParseHook(source, r, body, watchIdentity(dir, source, secrets))
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

// watchIdentity is who "me" is for a webhook: what a poll learned, else
// what `watch auth --me` said.
func watchIdentity(dir, source string, secrets watch.Secrets) string {
	state := watch.LoadState(dir)
	for key, c := range state.Cursors {
		if strings.HasSuffix(key, "/"+source) && c["me"] != "" {
			return c["me"]
		}
	}
	return secrets.Me
}

// watchTargetWorkspace is --workspace when given, else the one the cwd is
// in. A menu bar or a Stream Deck has no cwd to speak of.
// prRefURL turns a ref back into the link that ref shape implies: acme/api#7
// is a GitHub pull request, acme/api!7 a GitLab merge request. The dry run
// prints the prompt a real event would carry, so a GitHub URL built from a
// GitLab ref would preview a link that does not exist.
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
	var parts []string
	if len(wc.Labels) > 0 {
		parts = append(parts, "labels "+strings.Join(wc.Labels, ","))
	}
	if wc.Assignee == "any" {
		parts = append(parts, "any assignee")
	} else {
		parts = append(parts, "assigned to me")
	}
	if wc.Comments {
		parts = append(parts, "issue comments")
	}
	if wc.PRs {
		parts = append(parts, "PR reviews and comments")
	}
	action := wc.Action
	if action == "" {
		action = "notify"
	}
	out := strings.Join(parts, " · ") + " → " + action
	if action == "fix" {
		perHour, perDay := daemon.WatchSpec{MaxFixesPerHour: wc.MaxFixesPerHour, MaxFixesPerDay: wc.MaxFixesPerDay}.FixCaps()
		out += fmt.Sprintf(", at most %d/h %d/day", perHour, perDay)
		if wc.Quiet != "" {
			out += ", quiet " + wc.Quiet
		}
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
	f.Int("max-per-hour", 0, "With --action fix: at most this many fixes an hour (default 3); more are deferred")
	f.Int("max-per-day", 0, "With --action fix: at most this many fixes a day (default 10)")
	f.String("quiet", "", "Local hours to stay quiet in, e.g. 23:00-07:00: no fix starts and nothing buzzes; one summary when it opens")
	f.String("pickup", "", "Column a ticket moves to when it is picked up, e.g. \"In Progress\"; empty writes nothing")
	f.String("review-status", "", "Column a ticket moves to once a run opened a pull request for it, e.g. \"In Review\"")
	f.Bool("lease", false, "Claim a ticket on the tracker before working it, so a second machine watching the same board leaves it alone")
	f.Bool("isolate", false, "Give every unattended run its own worktrees on a corgi/<ref> branch, so it never touches your checkout")
	f.Bool("no-retry", false, "Leave deferred fixes to a manual `watch run` instead of starting them when the budget returns")
	f.Bool("reviews", false, "Also pull requests someone asked me to review — theirs, not mine")
	f.Bool("ci", false, "Also builds that went red on something of mine — the one kind that brings its own test for done")
	f.String("from", "", "Only comments and reviews from these people (comma separated); empty is anyone")
	f.String("auto-for", "", "With --action fix, what to work on unattended: tickets, comments, reviews (comma separated). Empty means everything")
	agentWatchRunCmd.Flags().Bool("dry-run", false, "Do not advance the saved cursors")
	tf := agentWatchTestCmd.Flags()
	tf.String("ref", "", "Issue key (ABC-12) or PR (owner/repo#12); default: one shaped for the first watched workspace")
	tf.String("url", "", "PR URL, for pr.* kinds")
	tf.String("body", "", "Comment or review text")
	tf.StringSlice("labels", nil, "Labels on the made-up issue (default: the workspace's own, so the rules take it)")
	agentWatchHooksCmd.Flags().Bool("rotate", false, "Make a new secret")
	a := agentWatchAuthCmd.Flags()
	a.String("token", "", "API token")
	a.String("url", "", "Jira site (https://you.atlassian.net) or self-hosted GitLab URL")
	a.String("email", "", "Jira account email")
	a.String("me", "", "Your login or id on the service, for webhooks before the first poll")
	a.Bool("local", false, "Store for the workspace you are in, not the machine")
	a.String("workspace", "", "Store for this workspace id, from anywhere")
	a.Bool("clear", false, "Remove the token instead of setting one")
	agentWatchCmd.AddCommand(agentWatchEnableCmd, agentWatchDisableCmd, agentWatchRunCmd, agentWatchTestCmd, agentWatchHooksCmd, agentWatchAuthCmd)
	agentCmd.AddCommand(agentWatchCmd)
}
