package cmd

import (
	"context"
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
	Suggests   []string            `json:"whatIsMissing,omitempty"`
}

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
	polls := map[string]watch.Summary{}
	for _, p := range watch.LoadState(dir).Summaries() {
		polls[p.Key] = p
	}
	out := []watchWorkspaceStatus{}
	for _, w := range registry.Sorted() {
		if args.Workspace == "" || w.ID == args.Workspace {
			out = append(out, watchStatusRow(dir, w, user, polls))
		}
	}
	if args.Workspace != "" && len(out) == 0 {
		return nil, fmt.Errorf("%q is not a registered workspace - `corgi agent init` there first", args.Workspace)
	}
	return map[string]any{"workspaces": out, "agentDir": dir}, nil
}

func watchStatusRow(dir string, w workspace.Workspace, user *config.UserConfig, polls map[string]watch.Summary) watchWorkspaceStatus {
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
		row.Action = firstNonEmpty(wc.Action, "notify")
	}
	for _, src := range [][2]string{
		{"linear", secrets.Linear},
		{"jira", secrets.JiraToken},
		{"github", firstNonEmptyString(secrets.GitHub, githubCLIToken())},
		{"gitlab", secrets.GitLab},
	} {
		s := watchSourceStatus{Name: src[0], HasToken: strings.TrimSpace(src[1]) != "", TokenLabel: watch.Fingerprint(src[1])}
		if p, ok := polls[w.ID+"/"+src[0]]; ok {
			s.LastPoll, s.Error = p.Polled, p.Error
		}
		row.Sources = append(row.Sources, s)
	}
	row.Suggests = watchGaps(row)
	return row
}

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

func checkWatchEnable(dir string, args watchEnableArgs) (string, error) {
	id := strings.TrimSpace(args.Workspace)
	if id == "" {
		var err error
		if id, err = currentWorkspaceID(dir); err != nil {
			return "", err
		}
	}
	registry, err := workspace.Load(agentRegistryPath(dir))
	if err != nil {
		return "", err
	}
	if _, ok := registry.Find(id); !ok {
		return "", fmt.Errorf("%q is not a registered workspace - `corgi agent init` there first", id)
	}
	if t := strings.TrimSpace(args.Tracker); t != "" && t != "linear" && t != "jira" {
		return "", fmt.Errorf("tracker must be linear or jira")
	}
	if a := strings.TrimSpace(args.Action); a != "" && a != "notify" && a != "fix" {
		return "", fmt.Errorf("action must be notify or fix")
	}
	return id, nil
}

func applyWatchEnable(wc *config.WatchConfig, args watchEnableArgs) {
	wc.Enabled = true
	setIfNotEmpty(&wc.Tracker, strings.TrimSpace(args.Tracker))
	setIfNotEmpty(&wc.Project, strings.TrimSpace(args.Project))
	setIfNotEmpty(&wc.Action, strings.TrimSpace(args.Action))
	for _, l := range []struct {
		dst *[]string
		v   []string
	}{{&wc.Repos, args.Repos}, {&wc.States, args.States}, {&wc.Labels, args.Labels}} {
		if len(l.v) > 0 {
			*l.dst = trimmedList(l.v)
		}
	}
	if args.Comments != nil {
		wc.Comments = *args.Comments
	}
	if args.PRs != nil {
		wc.PRs = *args.PRs
	}
}

func mcpWatchEnable(args watchEnableArgs) (map[string]any, error) {
	dir, err := agentDir()
	if err != nil {
		return nil, err
	}
	id, err := checkWatchEnable(dir, args)
	if err != nil {
		return nil, err
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
	applyWatchEnable(wc, args)
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

func githubCLIToken() string {
	token, _ := watch.GitHubToken(watch.Secrets{})
	return token
}

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

func watchEventsForMCP(workspaceID string, limit int) ([]map[string]any, error) {
	dir, err := agentDir()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	out := []map[string]any{}
	pulls := watch.LoadPullLog(dir)
	for _, e := range watch.RecentEvents(dir, limit) {
		if workspaceID != "" && e.Workspace != workspaceID {
			continue
		}
		row := map[string]any{
			"key": e.Key, "kind": string(e.Kind), "ref": e.Ref, "title": firstLineOf(e.Title),
			"url": e.URL, "workspace": e.Workspace, "at": e.At.Format(time.RFC3339),
			"canWorkOn": daemon.FixPrompt(e) != "",
		}
		if st, ok := pulls.Get(firstNonEmptyString(watch.PullRef(e.URL), e.Ref)); ok {
			row["pull"] = st
			row["readyToMerge"] = st.Ready()
		}
		out = append(out, row)
	}
	return out, nil
}

func mcpWatchBoard(workspaceID string, refresh bool) (map[string]any, error) {
	dir, err := agentDir()
	if err != nil {
		return nil, err
	}
	if workspaceID == "" {
		if workspaceID, err = currentWorkspaceID(dir); err != nil {
			return nil, err
		}
	}
	info := watch.LoadBoardCache(dir).Get(workspaceID)
	if refresh || !info.Has() {
		w, project, err := watchWriter(dir, workspaceID)
		if err != nil && w == nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if info, err = watch.RefreshBoard(ctx, dir, workspaceID, w, project); err != nil {
			return nil, err
		}
	}
	names := make([]string, 0, len(info.Statuses))
	for _, s := range info.Statuses {
		names = append(names, s.Name)
	}
	out := map[string]any{"workspace": workspaceID, "tracker": info.Tracker, "project": info.Project, "columns": names}
	if info.Me.Name != "" {
		out["me"] = info.Me.Name
	}
	if info.Error != "" {
		out["problem"] = info.Error
	}
	return out, nil
}

func mcpWatchMove(workspaceID, ref, status string) (map[string]any, error) {
	dir, err := agentDir()
	if err != nil {
		return nil, err
	}
	if workspaceID == "" {
		if workspaceID, err = currentWorkspaceID(dir); err != nil {
			return nil, err
		}
	}
	ref, status = strings.TrimSpace(ref), strings.TrimSpace(status)
	if ref == "" || status == "" {
		return nil, fmt.Errorf("name the ticket and the column to move it to")
	}
	w, _, err := watchWriter(dir, workspaceID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := w.Move(ctx, ref, status); err != nil {
		return nil, err
	}
	return map[string]any{"workspace": workspaceID, "ref": ref, "done": ref + " → " + status}, nil
}

func setIfNotEmpty(dst *string, v string) {
	if v != "" {
		*dst = v
	}
}
