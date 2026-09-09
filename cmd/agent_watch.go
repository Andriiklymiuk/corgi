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
(or gh auth); GITLAB_TOKEN. Or store them once: corgi agent watch auth.`,
	Run: runAgentWatchStatus,
}

var agentWatchEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Watch this workspace (run inside it)",
	RunE: func(cmd *cobra.Command, _ []string) error {
		dir := mustAgentDir()
		id, err := currentWorkspaceID(dir)
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
		flags := cmd.Flags()
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
	RunE: func(_ *cobra.Command, _ []string) error {
		dir := mustAgentDir()
		id, err := currentWorkspaceID(dir)
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
	Short: "Store a token for a source (environment variables take precedence)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := mustAgentDir()
		secrets := watch.LoadSecrets(dir)
		token, _ := cmd.Flags().GetString("token")
		url, _ := cmd.Flags().GetString("url")
		email, _ := cmd.Flags().GetString("email")
		me, _ := cmd.Flags().GetString("me")
		switch args[0] {
		case "linear":
			secrets.Linear = token
		case "jira":
			secrets.JiraToken, secrets.JiraURL, secrets.JiraEmail = token, url, email
		case "github":
			secrets.GitHub = token
		case "gitlab":
			secrets.GitLab = token
			if url != "" {
				secrets.GitLabURL = url
			}
		default:
			return fmt.Errorf("unknown source %q", args[0])
		}
		if me != "" {
			secrets.Me = me
		}
		if err := watch.SaveSecrets(dir, secrets); err != nil {
			return err
		}
		utils.Infof("%s: token %s saved\n", args[0], watch.Fingerprint(token))
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
	if utils.JSONOutput {
		type spec struct {
			Workspace string   `json:"workspace"`
			Sources   []string `json:"sources"`
			Action    string   `json:"action"`
			Interval  string   `json:"interval"`
		}
		var out []spec
		for _, s := range specs {
			var names []string
			for _, src := range s.Sources {
				names = append(names, src.Name())
			}
			out = append(out, spec{s.Workspace, names, s.Action, s.Interval.String()})
		}
		utils.PrintJSON(map[string]any{"workspaces": out, "polls": state.Summaries()})
		return
	}
	fmt.Println("Tokens")
	fmt.Printf("  linear %s · jira %s · github %s · gitlab %s · webhook secret %s\n",
		watch.Fingerprint(secrets.Linear), watch.Fingerprint(secrets.JiraToken), watch.Fingerprint(secrets.GitHub), watch.Fingerprint(secrets.GitLab), watch.Fingerprint(secrets.HookSecret))
	if len(specs) == 0 {
		fmt.Println("\nNo watched workspaces. Inside one: corgi agent watch enable --labels bug --prs")
		return
	}
	fmt.Println("\nWatched")
	for _, s := range specs {
		var names []string
		for _, src := range s.Sources {
			names = append(names, src.Name())
		}
		if len(names) == 0 {
			names = []string{"no source with a token"}
		}
		fmt.Printf("  %-20s %-8s every %-4s %s\n", s.Workspace, s.Action, s.Interval, strings.Join(names, ", "))
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
	secrets := watch.LoadSecrets(dir)
	var out []daemon.WatchSpec
	for _, w := range registry.Sorted() {
		repo, _ := config.LoadRepo(w.AbsPath)
		resolved := config.Resolve(w.ID, repo, user)
		wc := resolved.Watch
		if wc == nil || !wc.Enabled {
			continue
		}
		spec := daemon.WatchSpec{Workspace: w.ID, Dir: w.AbsPath, ConfigDir: expandTilde(resolved.ConfigDir), Project: wc.Project, Repos: wc.Repos,
			Rules:    watch.Rules{Enabled: true, Labels: wc.Labels, States: wc.States, Assignee: wc.Assignee, Comments: wc.Comments, PRs: wc.PRs},
			Interval: 3 * time.Minute, Action: "notify", SkipPermissions: resolved.DangerouslySkipPermissions}
		if wc.Action == "fix" {
			spec.Action = "fix"
		}
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
		if wc.PRs {
			if gh := watch.NewGitHub(secrets, wc.Repos); gh.Token != "" {
				spec.Sources = append(spec.Sources, gh)
			}
			if secrets.GitLab != "" {
				spec.Sources = append(spec.Sources, watch.NewGitLab(secrets))
			}
		}
		out = append(out, spec)
	}
	return out, nil
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

func currentWorkspaceID(dir string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	registry, err := workspace.Load(agentRegistryPath(dir))
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = real
	}
	best, bestLen := "", 0
	for _, w := range registry.Sorted() {
		path := w.AbsPath
		if real, err := filepath.EvalSymlinks(path); err == nil {
			path = real
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
	return strings.Join(parts, " · ") + " → " + action
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
	f.Bool("comments", false, "Also new comments on issues assigned to me")
	f.Bool("prs", false, "Also reviews and comments on pull requests I opened")
	agentWatchRunCmd.Flags().Bool("dry-run", false, "Do not advance the saved cursors")
	agentWatchHooksCmd.Flags().Bool("rotate", false, "Make a new secret")
	a := agentWatchAuthCmd.Flags()
	a.String("token", "", "API token")
	a.String("url", "", "Jira site (https://you.atlassian.net) or self-hosted GitLab URL")
	a.String("email", "", "Jira account email")
	a.String("me", "", "Your login or id on the service, for webhooks before the first poll")
	agentWatchCmd.AddCommand(agentWatchEnableCmd, agentWatchDisableCmd, agentWatchRunCmd, agentWatchHooksCmd, agentWatchAuthCmd)
	agentCmd.AddCommand(agentWatchCmd)
}
