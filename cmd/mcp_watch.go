package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/watch"
	"andriiklymiuk/corgi/utils/agent/workspace"
)

type watchStatusArgs struct {
	Workspace string
}

type watchSourceStatus struct {
	Name       string `json:"name"`
	HasToken   bool   `json:"hasToken"`
	TokenLabel string `json:"token"`
	LastPoll   string `json:"lastPoll,omitempty"`
	Error      string `json:"error,omitempty"`
}

type watchWorkspaceStatus struct {
	Workspace  string              `json:"workspace"`
	Dir        string              `json:"dir,omitempty"`
	Enabled    bool                `json:"enabled"`
	Tracker    string              `json:"tracker,omitempty"`
	Project    string              `json:"project,omitempty"`
	Repos      []string            `json:"repos,omitempty"`
	States     []string            `json:"states,omitempty"`
	Labels     []string            `json:"labels,omitempty"`
	Comments   bool                `json:"comments"`
	PRs        bool                `json:"prs"`
	Action     string              `json:"action,omitempty"`
	LocalToken bool                `json:"hasOwnTokens"`
	Sources    []watchSourceStatus `json:"sources,omitempty"`
	// Suggests is what this workspace still needs before anything arrives.
	Suggests []string `json:"whatIsMissing,omitempty"`
}

// mcpWatchStatus reports what each workspace watches and what it still needs,
// so a caller can set the rest up without guessing.
func mcpWatchStatus(args watchStatusArgs) (map[string]any, error) {
	dir, err := agentDir()
	if err != nil {
		return nil, err
	}
	registry, err := workspace.Load(agentRegistryPath(dir))
	if err != nil {
		return nil, err
	}
	user, err := config.LoadUser(agentUserConfigPath(dir))
	if err != nil {
		return nil, err
	}
	state := watch.LoadState(dir)
	polls := map[string]watch.Summary{}
	for _, p := range state.Summaries() {
		polls[p.Key] = p
	}

	out := []watchWorkspaceStatus{}
	for _, w := range registry.Sorted() {
		if args.Workspace != "" && w.ID != args.Workspace {
			continue
		}
		repo, _ := config.LoadRepo(w.AbsPath)
		resolved := config.Resolve(w.ID, repo, user)
		secrets := watch.LoadSecretsFor(dir, w.ID)
		row := watchWorkspaceStatus{
			Workspace:  w.ID,
			Dir:        w.AbsPath,
			LocalToken: !watch.WorkspaceSecrets(dir, w.ID).IsZero(),
		}
		if wc := resolved.Watch; wc != nil {
			row.Enabled, row.Tracker, row.Project = wc.Enabled, wc.Tracker, wc.Project
			row.Repos, row.States, row.Labels = wc.Repos, wc.States, wc.Labels
			row.Comments, row.PRs = wc.Comments, wc.PRs
			row.Action = wc.Action
			if row.Action == "" {
				row.Action = "notify"
			}
		}
		for _, src := range []struct {
			name  string
			token string
		}{
			{"linear", secrets.Linear},
			{"jira", secrets.JiraToken},
			{"github", firstNonEmptyString(secrets.GitHub, githubCLIToken())},
			{"gitlab", secrets.GitLab},
		} {
			s := watchSourceStatus{Name: src.name, HasToken: strings.TrimSpace(src.token) != "", TokenLabel: watch.Fingerprint(src.token)}
			if p, ok := polls[w.ID+"/"+src.name]; ok {
				s.LastPoll, s.Error = p.Polled, p.Error
			}
			row.Sources = append(row.Sources, s)
		}
		row.Suggests = watchGaps(row)
		out = append(out, row)
	}
	if args.Workspace != "" && len(out) == 0 {
		return nil, fmt.Errorf("%q is not a registered workspace — `corgi agent init` there first", args.Workspace)
	}
	return map[string]any{"workspaces": out, "agentDir": dir}, nil
}

// watchGaps is what stops this workspace reporting anything, in the order a
// person would fix it.
func watchGaps(row watchWorkspaceStatus) []string {
	var gaps []string
	tracker := row.Tracker
	hasTracker := false
	for _, s := range row.Sources {
		if (tracker == "" || s.Name == tracker) && (s.Name == "linear" || s.Name == "jira") && s.HasToken {
			hasTracker = true
		}
	}
	if !row.Enabled {
		gaps = append(gaps, "watch is off here: corgi agent watch enable")
	}
	if !hasTracker {
		gaps = append(gaps, "no tracker token: corgi agent watch auth <linear|jira> --local, inside this workspace")
	}
	if row.Enabled && row.Project == "" {
		gaps = append(gaps, "no --project: issues cannot be routed here. Read the key off the repos' commit ids rather than guessing one")
	}
	if row.Enabled && row.PRs && len(row.Repos) == 0 {
		gaps = append(gaps, "no --repos: reviews cannot be routed here")
	}
	for _, s := range row.Sources {
		if s.Error != "" {
			gaps = append(gaps, s.Name+" last poll failed: "+s.Error)
		}
	}
	return gaps
}

// optionalBool tells "not asked for" apart from "asked for, false": the two
// defaults agree only when the caller actually sent the key.
func optionalBool(r mcp.CallToolRequest, name string) *bool {
	v := r.GetBool(name, false)
	if r.GetBool(name, true) != v {
		return nil
	}
	return &v
}

type watchEnableArgs struct {
	Workspace string
	Tracker   string
	Project   string
	Repos     []string
	States    []string
	Labels    []string
	Comments  *bool
	PRs       *bool
	Action    string
}

// mcpWatchEnable turns the watch on for one workspace and says what it will
// take. It never accepts a token: a token belongs in a command flag or the
// environment, never in a tool call that is logged with the conversation.
func mcpWatchEnable(args watchEnableArgs) (map[string]any, error) {
	dir, err := agentDir()
	if err != nil {
		return nil, err
	}
	id := strings.TrimSpace(args.Workspace)
	if id == "" {
		if id, err = currentWorkspaceID(dir); err != nil {
			return nil, err
		}
	}
	registry, err := workspace.Load(agentRegistryPath(dir))
	if err != nil {
		return nil, err
	}
	known := false
	for _, w := range registry.Sorted() {
		if w.ID == id {
			known = true
			break
		}
	}
	if !known {
		return nil, fmt.Errorf("%q is not a registered workspace — `corgi agent init` there first", id)
	}
	if t := strings.TrimSpace(args.Tracker); t != "" && t != "linear" && t != "jira" {
		return nil, fmt.Errorf("tracker must be linear or jira")
	}
	if a := strings.TrimSpace(args.Action); a != "" && a != "notify" && a != "fix" {
		return nil, fmt.Errorf("action must be notify or fix")
	}

	path := agentUserConfigPath(dir)
	user, err := config.LoadUser(path)
	if err != nil {
		return nil, err
	}
	entry := user.Workspaces[id]
	wc := entry.Watch
	if wc == nil {
		wc = &config.WatchConfig{}
	}
	wc.Enabled = true
	if v := strings.TrimSpace(args.Tracker); v != "" {
		wc.Tracker = v
	}
	if v := strings.TrimSpace(args.Project); v != "" {
		wc.Project = v
	}
	if len(args.Repos) > 0 {
		wc.Repos = trimmedList(args.Repos)
	}
	if len(args.States) > 0 {
		wc.States = trimmedList(args.States)
	}
	if len(args.Labels) > 0 {
		wc.Labels = trimmedList(args.Labels)
	}
	if args.Comments != nil {
		wc.Comments = *args.Comments
	}
	if args.PRs != nil {
		wc.PRs = *args.PRs
	}
	if v := strings.TrimSpace(args.Action); v != "" {
		wc.Action = v
	}
	entry.Watch = wc
	if user.Workspaces == nil {
		user.Workspaces = map[string]config.WorkspaceConfig{}
	}
	user.Workspaces[id] = entry
	if err := writeUserConfig(path, user); err != nil {
		return nil, err
	}

	status, err := mcpWatchStatus(watchStatusArgs{Workspace: id})
	if err != nil {
		return nil, err
	}
	rows, _ := status["workspaces"].([]watchWorkspaceStatus)
	var gaps []string
	if len(rows) == 1 {
		gaps = rows[0].Suggests
	}
	next := []string{"corgi agent restart"}
	if len(gaps) > 0 {
		next = append([]string{"fix what is listed in whatIsMissing first"}, next...)
	}
	return map[string]any{
		"workspace":     id,
		"watch":         wc,
		"whatIsMissing": gaps,
		"next":          next,
		"note":          "the daemon polls every 3 minutes once restarted; the first tracker round looks 24 hours back",
	}, nil
}

func trimmedList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// githubCLIToken is what a GitHub poll would fall back to, without printing it.
func githubCLIToken() string {
	token, _ := watch.GitHubToken(watch.Secrets{})
	return token
}

// watchFixesForMCP is what the unattended mode did: what it worked on, and
// what it opened. The notification announcing a PR is gone in a second; this
// outlives it.
func watchFixesForMCP(workspaceID string, limit int) ([]map[string]any, error) {
	dir, err := agentDir()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	out := []map[string]any{}
	for _, r := range watch.LoadFixLog(dir).RecentFixes(workspaceID, limit) {
		row := map[string]any{
			"key": r.Key, "ref": r.Ref, "kind": r.Kind, "workspace": r.Workspace,
			"startedAt": r.StartedAt.Format(time.RFC3339), "running": !r.Done(),
		}
		if r.URL != "" {
			row["issueUrl"] = r.URL
		}
		if len(r.PRs) > 0 {
			row["prs"] = r.PRs
		}
		if r.Note != "" {
			row["note"] = r.Note
		}
		if r.Error != "" {
			row["error"] = r.Error
		}
		out = append(out, row)
	}
	return out, nil
}

// watchEventsForMCP is the recent watch events, so a caller can act on them.
func watchEventsForMCP(workspaceID string, limit int) ([]map[string]any, error) {
	dir, err := agentDir()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	out := []map[string]any{}
	for _, e := range watch.RecentEvents(dir, limit) {
		if workspaceID != "" && e.Workspace != workspaceID {
			continue
		}
		out = append(out, map[string]any{
			"key": e.Key, "kind": string(e.Kind), "ref": e.Ref, "title": firstLineOf(e.Title),
			"url": e.URL, "workspace": e.Workspace, "at": e.At.Format(time.RFC3339),
			"canWorkOn": daemon.FixPrompt(e) != "",
		})
	}
	return out, nil
}
