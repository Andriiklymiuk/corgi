package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	gitlabProjectsAPIPath = "/api/v4/projects/"
	mergeRequestsPath     = "/merge_requests/"
	gitlabDefaultBase     = "https://gitlab.com"
)

type GitLab struct {
	URL    string
	Token  string
	Me     string
	MeID   string
	Client *http.Client
}

func NewGitLab(s Secrets) *GitLab {
	return &GitLab{URL: strings.TrimSpace(s.GitLabURL), Token: strings.TrimSpace(s.GitLab)}
}

func (g *GitLab) Name() string { return "gitlab" }

var gitlabActions = map[string]Kind{
	"mentioned":          KindPRComment,
	"directly_addressed": KindPRComment,
	"assigned":           KindPRComment,
	"review_requested":   KindReviewRequested,
	"approval_required":  KindReviewRequested,
	"build_failed":       KindCIFailed,
}

type gitlabTodo struct {
	ID         int64             `json:"id"`
	ActionName string            `json:"action_name"`
	TargetType string            `json:"target_type"`
	TargetURL  string            `json:"target_url"`
	Body       string            `json:"body"`
	CreatedAt  string            `json:"created_at"`
	Project    gitlabTodoProject `json:"project"`
	Target     gitlabTodoTarget  `json:"target"`
	Author     gitlabTodoAuthor  `json:"author"`
}

type gitlabTodoProject struct {
	PathWithNamespace string `json:"path_with_namespace"`
}

type gitlabTodoTargetAuthor struct {
	Username string `json:"username"`
}

type gitlabTodoTarget struct {
	IID    int64                  `json:"iid"`
	Title  string                 `json:"title"`
	State  string                 `json:"state"`
	Author gitlabTodoTargetAuthor `json:"author"`
}

type gitlabTodoAuthor struct {
	Username string `json:"username"`
	Bot      bool   `json:"bot"`
}

var gitlabBotName = regexp.MustCompile(`(?i)^(project|group)_\d+_bot|[_-]bot$|^(ghost|support-bot|alert-bot|security-bot|gitlab-bot)$`)

func gitlabBot(username string, flagged bool) bool {
	return flagged || gitlabBotName.MatchString(strings.TrimSpace(username))
}

func (g *GitLab) Poll(ctx context.Context, cursor Cursor) ([]Event, Cursor, error) {
	if g.Token == "" {
		return nil, cursor, ErrNoToken
	}
	base := strings.TrimRight(firstOr(g.URL, gitlabDefaultBase), "/")
	g.Me = firstOr(g.Me, cursor["me"])
	g.MeID = firstOr(g.MeID, cursor["meId"])
	if g.Me == "" || g.MeID == "" {
		g.learnMe(ctx, base)
	}
	todos, err := g.pendingTodos(ctx, base)
	if err != nil {
		return nil, cursor, err
	}
	lastID, _ := strconv.ParseInt(cursor["lastId"], 10, 64)
	maxID := lastID
	var events []Event
	for _, t := range todos {
		maxID = max(maxID, t.ID)
		if t.ID <= lastID {
			continue
		}
		if e, ok := g.todoEvent(t); ok {
			events = append(events, e)
		}
	}
	next := Cursor{}
	for k, v := range cursor {
		next[k] = v
	}
	if maxID > 0 {
		next["lastId"] = strconv.FormatInt(maxID, 10)
	}
	if g.MeID != "" {
		next["meId"] = g.MeID
	}
	if g.Me != "" {
		next["me"] = g.Me
		notes, since := g.notesOnMyMRs(ctx, base, cursor["notesSince"])
		events = append(events, notes...)
		if since != "" {
			next["notesSince"] = since
		}
	}
	return events, next, nil
}

func (g *GitLab) learnMe(ctx context.Context, base string) {
	var user struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	}
	if err := g.getInto(ctx, base+"/api/v4/user", &user); err != nil {
		return
	}
	g.Me = firstOr(g.Me, user.Username)
	if user.ID > 0 {
		g.MeID = strconv.FormatInt(user.ID, 10)
	}
}

func (g *GitLab) pendingTodos(ctx context.Context, base string) ([]gitlabTodo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v4/todos?state=pending&per_page=50", nil)
	if err != nil {
		return nil, fmt.Errorf("gitlab todos: %v", err)
	}
	req.Header.Set(gitlabTokenHeader, g.Token)
	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gitlab todos: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 120))
		return nil, fmt.Errorf("gitlab todos: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var todos []gitlabTodo
	if err := json.NewDecoder(resp.Body).Decode(&todos); err != nil {
		return nil, fmt.Errorf("gitlab todos: decoding: %v", err)
	}
	return todos, nil
}

func (g *GitLab) todoEvent(t gitlabTodo) (Event, bool) {
	if t.TargetType != "MergeRequest" {
		return Event{}, false
	}
	kind, ok := gitlabActions[t.ActionName]
	if !ok || (kind == KindPRComment && isMe(g.Me, t.Author.Username)) {
		return Event{}, false
	}
	at, _ := time.Parse(time.RFC3339, t.CreatedAt)
	body := t.Body
	if kind == KindCIFailed {
		body = ""
	}
	return Event{
		Key:    "gitlab:todo:" + strconv.FormatInt(t.ID, 10),
		Source: g.Name(),
		Kind:   kind,
		Ref:    t.Project.PathWithNamespace + "!" + strconv.FormatInt(t.Target.IID, 10),
		Title:  t.Target.Title,
		Body:   gitlabTruncate(body, 200),
		URL:    t.TargetURL,
		Author: t.Author.Username,
		Mine:   kind != KindReviewRequested && isMe(g.Me, t.Target.Author.Username),
		Bot:    kind == KindPRComment && gitlabBot(t.Author.Username, t.Author.Bot),
		State:  t.Target.State,
		At:     at,
	}, true
}

type gitlabMyMR struct {
	IID       int64          `json:"iid"`
	ProjectID int64          `json:"project_id"`
	Title     string         `json:"title"`
	WebURL    string         `json:"web_url"`
	State     string         `json:"state"`
	UpdatedAt string         `json:"updated_at"`
	Refs      gitlabMyMRRefs `json:"references"`
}

type gitlabMyMRRefs struct {
	Full string `json:"full"`
}

type gitlabNote struct {
	ID        int64            `json:"id"`
	Body      string           `json:"body"`
	System    bool             `json:"system"`
	CreatedAt string           `json:"created_at"`
	Author    gitlabTodoAuthor `json:"author"`
}

// A reviewer's comment on my own merge request makes no to-do item unless it
// mentions me, so the to-do list alone misses most review feedback. This reads the
// notes of my open merge requests that moved since the last round, one event
// per merge request carrying its newest note. The first round only marks
// where to start.
func (g *GitLab) notesOnMyMRs(ctx context.Context, base, since string) ([]Event, string) {
	endpoint := base + "/api/v4/merge_requests?scope=created_by_me&state=opened&order_by=updated_at&sort=desc&per_page=50"
	if since != "" {
		endpoint += "&updated_after=" + url.QueryEscape(since)
	}
	var mrs []gitlabMyMR
	if err := g.getInto(ctx, endpoint, &mrs); err != nil {
		return nil, since
	}
	sinceAt, _ := time.Parse(time.RFC3339, since)
	next := latestMRUpdate(mrs, sinceAt).UTC().Format(time.RFC3339Nano)
	if since == "" {
		return nil, next
	}
	var events []Event
	for _, mr := range mrs {
		if e, ok := g.newestNoteOn(ctx, base, mr, sinceAt); ok {
			events = append(events, e)
		}
	}
	return events, next
}

func latestMRUpdate(mrs []gitlabMyMR, sinceAt time.Time) time.Time {
	latest := sinceAt
	for _, mr := range mrs {
		if at, err := time.Parse(time.RFC3339, mr.UpdatedAt); err == nil && at.After(latest) {
			latest = at
		}
	}
	if latest.IsZero() {
		return time.Now()
	}
	return latest
}

func (g *GitLab) newestNoteOn(ctx context.Context, base string, mr gitlabMyMR, sinceAt time.Time) (Event, bool) {
	var notes []gitlabNote
	notesURL := fmt.Sprintf("%s/api/v4/projects/%d/merge_requests/%d/notes?sort=desc&order_by=created_at&per_page=20", base, mr.ProjectID, mr.IID)
	if err := g.getInto(ctx, notesURL, &notes); err != nil {
		return Event{}, false
	}
	for _, n := range notes {
		at, _ := time.Parse(time.RFC3339, n.CreatedAt)
		if !at.After(sinceAt) {
			return Event{}, false
		}
		if n.System || isMe(g.Me, n.Author.Username) || gitlabBot(n.Author.Username, n.Author.Bot) || strings.TrimSpace(n.Body) == "" {
			continue
		}
		return Event{
			Key:    "gitlab:note:" + strconv.FormatInt(n.ID, 10),
			Source: g.Name(),
			Kind:   KindPRComment,
			Ref:    firstOr(mr.Refs.Full, gitlabRefFromURL(mr.WebURL, mr.IID)),
			Title:  mr.Title,
			Body:   gitlabTruncate(n.Body, 200),
			URL:    mr.WebURL,
			Author: n.Author.Username,
			Mine:   true,
			State:  mr.State,
			At:     at,
		}, true
	}
	return Event{}, false
}

func gitlabRefFromURL(webURL string, iid int64) string {
	u, err := url.Parse(webURL)
	if err != nil {
		return ""
	}
	project, _, _ := strings.Cut(strings.Trim(u.Path, "/"), "/-/")
	return project + "!" + strconv.FormatInt(iid, 10)
}

func gitlabTruncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func (g *GitLab) RefState(ctx context.Context, ref string) string {
	project, num, ok := strings.Cut(ref, "!")
	if !ok || g.Token == "" {
		return ""
	}
	base := g.URL
	if base == "" {
		base = gitlabDefaultBase
	}
	var mr struct {
		State string `json:"state"`
		Draft bool   `json:"draft"`
	}
	if err := g.getInto(ctx, base+gitlabProjectsAPIPath+url.PathEscape(project)+mergeRequestsPath+num, &mr); err != nil {
		return ""
	}
	if mr.Draft && mr.State == "opened" {
		return "draft"
	}
	return mr.State
}

// AnsweredSince is the Answerer for a merge request: a note of mine after the
// moment, and a head commit after it.
func (g *GitLab) AnsweredSince(ctx context.Context, ref string, at time.Time) string {
	project, num, ok := strings.Cut(ref, "!")
	if !ok || g.Token == "" || g.Me == "" || at.IsZero() {
		return ""
	}
	base := g.URL
	if base == "" {
		base = gitlabDefaultBase
	}
	mrURL := base + gitlabProjectsAPIPath + url.PathEscape(project) + mergeRequestsPath + num
	var notes []gitlabNote
	if err := g.getInto(ctx, mrURL+"/notes?sort=desc&order_by=created_at&per_page=20", &notes); err != nil {
		return ""
	}
	replied := false
	for _, n := range notes {
		if n.System || !isMe(g.Me, n.Author.Username) {
			continue
		}
		if t, err := time.Parse(time.RFC3339, n.CreatedAt); err == nil && !t.Before(at) {
			replied = true
			break
		}
	}
	if !replied {
		return ""
	}
	var mr struct {
		SHA string `json:"sha"`
	}
	if err := g.getInto(ctx, mrURL, &mr); err != nil || mr.SHA == "" {
		return ""
	}
	var c struct {
		CommittedDate string `json:"committed_date"`
	}
	if err := g.getInto(ctx, base+gitlabProjectsAPIPath+url.PathEscape(project)+"/repository/commits/"+mr.SHA, &c); err != nil {
		return ""
	}
	if t, err := time.Parse(time.RFC3339, c.CommittedDate); err != nil || t.Before(at) {
		return ""
	}
	return "you replied and pushed after it"
}

// MyReviewSince is the ReviewTeller for a merge request. GitLab keeps no
// review object: an approval is a system note, a review is my notes.
func (g *GitLab) MyReviewSince(ctx context.Context, ref string, since time.Time) (ReviewOutcome, bool) {
	project, num, ok := strings.Cut(ref, "!")
	if !ok || g.Token == "" {
		return ReviewOutcome{}, false
	}
	base := strings.TrimRight(firstOr(g.URL, gitlabDefaultBase), "/")
	if g.Me == "" {
		var user struct {
			Username string `json:"username"`
		}
		if err := g.getInto(ctx, base+"/api/v4/user", &user); err != nil || user.Username == "" {
			return ReviewOutcome{}, false
		}
		g.Me = user.Username
	}
	var notes []gitlabNote
	if err := g.getInto(ctx, base+gitlabProjectsAPIPath+url.PathEscape(project)+mergeRequestsPath+num+"/notes?sort=desc&order_by=created_at&per_page=100", &notes); err != nil {
		return ReviewOutcome{}, false
	}
	var out ReviewOutcome
	for _, n := range notes {
		if isMe(g.Me, n.Author.Username) {
			if t, err := time.Parse(time.RFC3339, n.CreatedAt); err == nil && !t.Before(since) {
				countMyNote(&out, n)
			}
		}
	}
	return out, true
}

func countMyNote(out *ReviewOutcome, n gitlabNote) {
	body := strings.ToLower(strings.TrimSpace(n.Body))
	switch {
	case n.System && strings.HasPrefix(body, "approved this merge request"):
		out.Approved = true
	case n.System && strings.HasPrefix(body, "requested changes"):
		out.ChangesRequested = true
	case !n.System:
		out.Comments++
	}
}

func (g *GitLab) getInto(ctx context.Context, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("PRIVATE-TOKEN", g.Token)
	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("gitlab: HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
}

func (g *GitLab) PullStatus(ctx context.Context, ref string) (PullStatus, bool) {
	project, num, ok := strings.Cut(ref, "!")
	if !ok || g.Token == "" {
		return PullStatus{}, false
	}
	base := firstOr(g.URL, gitlabDefaultBase)
	var mr struct {
		State        string                          `json:"state"`
		Draft        bool                            `json:"draft"`
		HeadPipeline *gitLabPullStatusMrHeadPipeline `json:"head_pipeline"`
		Author       gitlabTodoTargetAuthor          `json:"author"`
	}
	endpoint := base + gitlabProjectsAPIPath + url.PathEscape(project) + mergeRequestsPath + num
	if err := g.getInto(ctx, endpoint, &mr); err != nil {
		return PullStatus{}, false
	}
	out := PullStatus{State: gitlabMRState(mr.State, mr.Draft), At: time.Now(), Mine: g.Me != "" && isMe(g.Me, mr.Author.Username)}
	if out.State != "open" && out.State != "draft" {
		return out, true
	}
	out.Checks = "none"
	if mr.HeadPipeline != nil {
		out.Checks = firstOr(gitlabPipelineChecks[mr.HeadPipeline.Status], out.Checks)
	}
	g.addApprovals(ctx, endpoint, &out)
	return out, true
}

var gitlabPipelineChecks = map[string]string{
	"success": "passing", "failed": "failing",
	"running": "pending", "pending": "pending", "created": "pending", "waiting_for_resource": "pending", "preparing": "pending", "scheduled": "pending",
}

func gitlabMRState(state string, draft bool) string {
	if state == "opened" {
		state = "open"
	}
	if draft && state == "open" {
		return "draft"
	}
	return state
}

func (g *GitLab) addApprovals(ctx context.Context, endpoint string, out *PullStatus) {
	var approvals struct {
		Approved   bool                                  `json:"approved"`
		ApprovedBy []gitLabPullStatusApprovalsApprovedBy `json:"approved_by"`
	}
	if err := g.getInto(ctx, endpoint+"/approvals", &approvals); err != nil {
		return
	}
	out.Review = "pending"
	if approvals.Approved || len(approvals.ApprovedBy) > 0 {
		out.Review = "approved"
	}
	for _, a := range approvals.ApprovedBy {
		if isMe(g.Me, a.User.Username) {
			// GitLab gives an approval no time: seen now is when it counts from.
			out.MyReviewAt = out.At
		}
	}
}

type gitLabPullStatusMrHeadPipeline struct {
	Status string `json:"status"`
}

type gitLabPullStatusApprovalsApprovedBy struct {
	User gitlabTodoTargetAuthor `json:"user"`
}

// TokenScopes asks GitLab what the token may do. A token that reads the
// tracker fine still merges nothing without the api scope, and the forge only
// says so at the merge.
func (g *GitLab) TokenScopes(ctx context.Context) ([]string, error) {
	if g.Token == "" {
		return nil, ErrNoToken
	}
	base := g.URL
	if base == "" {
		base = gitlabDefaultBase
	}
	var out struct {
		Scopes []string `json:"scopes"`
	}
	if err := g.getInto(ctx, strings.TrimRight(base, "/")+"/api/v4/personal_access_tokens/self", &out); err != nil {
		return nil, err
	}
	return out.Scopes, nil
}

// CanWrite says whether a scope list covers writing: api, or nothing known
// (a token GitLab will not describe, such as an OAuth one, is not judged).
func GitLabScopesCanWrite(scopes []string) bool {
	if len(scopes) == 0 {
		return true
	}
	for _, s := range scopes {
		if s == "api" {
			return true
		}
	}
	return false
}
