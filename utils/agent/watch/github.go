package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	commitsSegment   = "/commits/"
	pullsSegment     = "/pulls/"
	reposSegment     = "/repos/"
	reviewsPageQuery = "/reviews?per_page=100"
	githubKeyPrefix  = "github:"
	githubAPIBase    = "https://api.github.com"
)

type GitHub struct {
	Token  string
	Repos  []string
	Me     string
	Client *http.Client
	URL    string
}

var githubAuthToken = func() string {
	out, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func NewGitHub(s Secrets, repos []string) *GitHub {
	token := strings.TrimSpace(s.GitHub)
	if token == "" {
		token = strings.TrimSpace(githubAuthToken())
	}
	return &GitHub{Token: token, Repos: repos}
}

func (g *GitHub) Name() string { return "github" }

type githubReason struct {
	kind Kind
	mine bool
}

var githubReasons = map[string]githubReason{
	"review_requested": {KindReviewRequested, false},
	"mention":          {KindPRComment, true},
	"author":           {KindPRComment, true},
	"comment":          {KindPRComment, true},
	"team_mention":     {KindPRComment, false},
	"ci_activity":      {KindCIFailed, true},
}

type githubThread struct {
	ID         string                 `json:"id"`
	Reason     string                 `json:"reason"`
	UpdatedAt  string                 `json:"updated_at"`
	Subject    githubThreadSubject    `json:"subject"`
	Repository githubThreadRepository `json:"repository"`
}

type githubThreadSubject struct {
	Title            string `json:"title"`
	URL              string `json:"url"`
	Type             string `json:"type"`
	LatestCommentURL string `json:"latest_comment_url"`
}

type githubThreadRepository struct {
	FullName string `json:"full_name"`
}

func (g *GitHub) Poll(ctx context.Context, cursor Cursor) ([]Event, Cursor, error) {
	if g.Token == "" {
		return nil, cursor, ErrNoToken
	}
	if cursor == nil {
		cursor = Cursor{}
	}
	g.Me = firstOr(g.Me, cursor["me"])
	if g.Me == "" {
		if err := g.learnLogin(ctx); err != nil {
			return nil, cursor, err
		}
	}
	resp, err := g.get(ctx, "/notifications?all=false&participating=true", cursor["lastModified"])
	if err != nil {
		return nil, cursor, err
	}
	if resp.StatusCode == http.StatusNotModified {
		resp.Body.Close()
		return nil, cursor, nil
	}
	var threads []githubThread
	if err := githubDecode(resp, &threads); err != nil {
		return nil, cursor, err
	}
	next := Cursor{}
	for k, v := range cursor {
		next[k] = v
	}
	next["me"] = g.Me
	if lm := resp.Header.Get("Last-Modified"); lm != "" {
		next["lastModified"] = lm
	}
	if pi := resp.Header.Get("X-Poll-Interval"); pi != "" {
		next["pollInterval"] = pi
	}
	var events []Event
	pulls := map[string]githubPullInfo{}
	for _, t := range threads {
		if e, ok := g.threadEvent(ctx, t, pulls); ok {
			events = append(events, e)
		}
	}
	return events, next, nil
}

func (g *GitHub) learnLogin(ctx context.Context) error {
	var user struct {
		Login string `json:"login"`
	}
	resp, err := g.get(ctx, "/user", "")
	if err != nil {
		return err
	}
	if err := githubDecode(resp, &user); err != nil {
		return err
	}
	g.Me = user.Login
	return nil
}

func (g *GitHub) threadEvent(ctx context.Context, t githubThread, pulls map[string]githubPullInfo) (Event, bool) {
	if !g.wantsRepo(t.Repository.FullName) {
		return Event{}, false
	}
	r, ok := githubReasons[t.Reason]
	if !ok {
		return Event{}, false
	}
	if r.kind == KindCIFailed {
		return g.ciThreadEvent(t)
	}
	if t.Subject.Type != "PullRequest" {
		return Event{}, false
	}
	return g.pullThreadEvent(ctx, t, r, pulls)
}

func (g *GitHub) ciThreadEvent(t githubThread) (Event, bool) {
	if t.Subject.Type != "CheckSuite" {
		return Event{}, false
	}
	at, _ := time.Parse(time.RFC3339, t.UpdatedAt)
	return Event{
		Key:    "github:ci:" + t.Repository.FullName + ":" + t.ID + ":" + t.UpdatedAt,
		Source: g.Name(),
		Kind:   KindCIFailed,
		Ref:    t.Repository.FullName,
		Title:  firstNonEmptyText(t.Subject.Title, "a workflow run failed"),
		URL:    "https://github.com/" + t.Repository.FullName + "/actions",
		Mine:   true,
		At:     at,
	}, true
}

type threadComment struct {
	author, body, review string
	bot, has             bool
	kind                 Kind
	key                  string
}

func (g *GitHub) pullThreadEvent(ctx context.Context, t githubThread, r githubReason, pulls map[string]githubPullInfo) (Event, bool) {
	number := t.Subject.URL[strings.LastIndex(t.Subject.URL, "/")+1:]
	ref := t.Repository.FullName + "#" + number
	at, _ := time.Parse(time.RFC3339, t.UpdatedAt)
	pull := g.pull(ctx, pulls, t.Subject.URL)
	c := g.threadComment(ctx, t, r, ref, number, at)
	if r.kind == KindPRComment && (!c.has || (c.author != "" && strings.EqualFold(c.author, g.Me))) {
		return Event{}, false
	}
	return Event{
		Key:    c.key,
		Source: g.Name(),
		Kind:   c.kind,
		Ref:    ref,
		Title:  t.Subject.Title,
		Body:   c.body,
		URL:    "https://github.com/" + t.Repository.FullName + "/pull/" + number,
		Author: c.author,
		Mine:   r.mine && isMe(g.Me, pull.author),
		Bot:    c.bot,
		State:  pull.state,
		Review: c.review,
		At:     at,
	}, true
}

func (g *GitHub) threadComment(ctx context.Context, t githubThread, r githubReason, ref, number string, at time.Time) threadComment {
	c := threadComment{kind: r.kind, key: githubKeyPrefix + ref + ":" + t.ID + ":" + t.UpdatedAt}
	c.author, c.body, c.bot, c.has = g.latestComment(ctx, t.Subject.LatestCommentURL, t.Subject.URL)
	if id := githubCommentKey(t.Subject.LatestCommentURL); id != "" {
		c.key = githubKeyPrefix + ref + ":" + id
	}
	if c.has || r.kind != KindPRComment {
		return c
	}
	// A review submitted with a body and nothing after it leaves the thread's
	// latest_comment_url pointing at the pull request itself, so the review that
	// raised the thread has to be read off the pull request. Its key is the one
	// the webhook gives the same review, so the two are one event.
	if rv, ok := g.reviewBehind(ctx, t.Repository.FullName, number, at); ok {
		c.author, c.body, c.bot, c.has = rv.author, rv.body, rv.bot, true
		c.kind, c.review = KindPRReview, rv.state
		c.key = githubKeyPrefix + ref + ":r" + rv.id
	}
	return c
}

func (g *GitHub) latestComment(ctx context.Context, commentURL, subjectURL string) (author, body string, bot, hasComment bool) {
	if commentURL == "" || commentURL == subjectURL {
		return "", "", false, false
	}
	path := strings.TrimPrefix(commentURL, githubAPIBase)
	if path == commentURL {
		return "", "", false, true
	}
	resp, err := g.get(ctx, path, "")
	if err != nil {
		return "", "", false, true
	}
	defer resp.Body.Close()
	var c struct {
		Body string                  `json:"body"`
		User gitHubLatestCommentUser `json:"user"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&c) != nil {
		return "", "", false, true
	}
	bot = githubBot(c.User.Login, c.User.Type)
	return c.User.Login, clip(strings.TrimSpace(c.Body), bodyMax), bot, true
}

type gitHubLatestCommentUser struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

type githubReview struct {
	id, author, body, state string
	bot                     bool
}

// reviewBehind is the review that raised a thread whose latest_comment_url is
// the pull request itself: the newest one by someone else, with words in it and
// not older than the thread's activity, so a stale review cannot be raised again
// by a push or a merge. An inline-only review carries no body and arrives as its
// comments instead.
func (g *GitHub) reviewBehind(ctx context.Context, repo, num string, at time.Time) (githubReview, bool) {
	var reviews []struct {
		ID          int64                   `json:"id"`
		State       string                  `json:"state"`
		Body        string                  `json:"body"`
		SubmittedAt string                  `json:"submitted_at"`
		User        gitHubLatestCommentUser `json:"user"`
	}
	resp, err := g.get(ctx, reposSegment+repo+pullsSegment+num+reviewsPageQuery, "")
	if err != nil || githubDecode(resp, &reviews) != nil {
		return githubReview{}, false
	}
	var best githubReview
	var bestAt time.Time
	for _, r := range reviews {
		if r.State == "PENDING" || strings.TrimSpace(r.Body) == "" {
			continue
		}
		if g.Me != "" && strings.EqualFold(r.User.Login, g.Me) {
			continue
		}
		submitted := trackerTime(r.SubmittedAt)
		if !at.IsZero() && submitted.Before(at.Add(-2*time.Minute)) {
			continue
		}
		if submitted.Before(bestAt) {
			continue
		}
		bestAt = submitted
		best = githubReview{
			id:     strconv.FormatInt(r.ID, 10),
			author: r.User.Login,
			body:   clip(strings.TrimSpace(r.Body), bodyMax),
			state:  strings.ToLower(r.State),
			bot:    githubBot(r.User.Login, r.User.Type),
		}
	}
	return best, best.id != ""
}

type githubPullInfo struct {
	state  string
	author string
}

func (g *GitHub) pull(ctx context.Context, cache map[string]githubPullInfo, apiURL string) githubPullInfo {
	if apiURL == "" {
		return githubPullInfo{}
	}
	if p, ok := cache[apiURL]; ok {
		return p
	}
	cache[apiURL] = githubPullInfo{}
	path := strings.TrimPrefix(apiURL, githubAPIBase)
	if path == apiURL {
		return githubPullInfo{}
	}
	resp, err := g.get(ctx, path, "")
	if err != nil {
		return githubPullInfo{}
	}
	defer resp.Body.Close()
	var pr struct {
		State  string           `json:"state"`
		Merged bool             `json:"merged"`
		Draft  bool             `json:"draft"`
		User   gitHubPullPrUser `json:"user"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&pr) != nil {
		return githubPullInfo{}
	}
	state := pr.State
	if pr.Merged {
		state = "merged"
	} else if pr.Draft && state == "open" {
		state = "draft"
	}
	p := githubPullInfo{state: state, author: pr.User.Login}
	cache[apiURL] = p
	return p
}

type gitHubPullPrUser struct {
	Login string `json:"login"`
}

func firstNonEmptyText(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

func (g *GitHub) wantsRepo(fullName string) bool {
	if len(g.Repos) == 0 {
		return true
	}
	return containsFold(g.Repos, fullName)
}

func (g *GitHub) get(ctx context.Context, path, ifModifiedSince string) (*http.Response, error) {
	base := g.URL
	if base == "" {
		base = githubAPIBase
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+path, nil)
	if err != nil {
		return nil, fmt.Errorf("github %s: %v", path, err)
	}
	req.Header.Set("Authorization", "Bearer "+g.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	if ifModifiedSince != "" {
		req.Header.Set("If-Modified-Since", ifModifiedSince)
	}
	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github %s: %v", path, err)
	}
	if resp.StatusCode == http.StatusNotModified || (resp.StatusCode >= 200 && resp.StatusCode < 300) {
		return resp, nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 120))
	return nil, fmt.Errorf("github %s: HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
}

func githubDecode(resp *http.Response, v any) error {
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("github: decoding %s: %v", resp.Request.URL.Path, err)
	}
	return nil
}

func (g *GitHub) RefState(ctx context.Context, ref string) string {
	repo, num, ok := strings.Cut(ref, "#")
	if !ok || g.Token == "" {
		return ""
	}
	return g.pull(ctx, map[string]githubPullInfo{}, "https://api.github.com/repos/"+repo+pullsSegment+num).state
}

func (g *GitHub) PullStatus(ctx context.Context, ref string) (PullStatus, bool) {
	repo, num, ok := strings.Cut(ref, "#")
	if !ok || g.Token == "" {
		return PullStatus{}, false
	}
	var pr struct {
		State              string                 `json:"state"`
		Merged             bool                   `json:"merged"`
		Draft              bool                   `json:"draft"`
		Head               gitHubPullStatusPrHead `json:"head"`
		RequestedReviewers []gitHubPullPrUser     `json:"requested_reviewers"`
		User               gitHubPullPrUser       `json:"user"`
	}
	resp, err := g.get(ctx, reposSegment+repo+pullsSegment+num, "")
	if err != nil || githubDecode(resp, &pr) != nil {
		return PullStatus{}, false
	}
	out := PullStatus{State: pr.State, At: time.Now(), Mine: g.Me != "" && strings.EqualFold(pr.User.Login, g.Me)}
	if pr.Merged {
		out.State = "merged"
	} else if pr.Draft && pr.State == "open" {
		out.State = "draft"
	}
	if out.State != "open" && out.State != "draft" {
		return out, true
	}
	g.addReviews(ctx, reposSegment+repo+pullsSegment+num+reviewsPageQuery, len(pr.RequestedReviewers) > 0, &out)
	var conclusions []string
	running := 0
	if pr.Head.SHA != "" {
		conclusions, running = g.checksOn(ctx, reposSegment+repo+commitsSegment+pr.Head.SHA)
	}
	out.Checks = checksVerdict(conclusions, running)
	return out, true
}

type githubReviewRow struct {
	State       string           `json:"state"`
	SubmittedAt time.Time        `json:"submitted_at"`
	User        gitHubPullPrUser `json:"user"`
}

func (g *GitHub) addReviews(ctx context.Context, path string, requested bool, out *PullStatus) {
	var reviews []githubReviewRow
	resp, err := g.get(ctx, path, "")
	if err != nil || githubDecode(resp, &reviews) != nil {
		return
	}
	last := map[string]string{}
	for _, r := range reviews {
		if g.Me != "" && strings.EqualFold(r.User.Login, g.Me) && r.SubmittedAt.After(out.MyReviewAt) {
			out.MyReviewAt = r.SubmittedAt
		}
		switch r.State {
		case "APPROVED", "CHANGES_REQUESTED", "DISMISSED":
			last[r.User.Login] = r.State
		}
	}
	out.Review = reviewVerdict(last, requested)
}

func reviewVerdict(last map[string]string, requested bool) string {
	verdict := "none"
	for _, st := range last {
		if st == "APPROVED" && verdict != "changes" {
			verdict = "approved"
		}
		if st == "CHANGES_REQUESTED" {
			verdict = "changes"
		}
	}
	if verdict == "none" && requested {
		return "pending"
	}
	return verdict
}

func (g *GitHub) checksOn(ctx context.Context, commit string) ([]string, int) {
	var conclusions []string
	running := 0
	var runs struct {
		CheckRuns []gitHubPullStatusRunsCheckRuns `json:"check_runs"`
	}
	if resp, err := g.get(ctx, commit+"/check-runs?per_page=100", ""); err == nil && githubDecode(resp, &runs) == nil {
		for _, r := range runs.CheckRuns {
			if r.Status != "completed" {
				running++
				continue
			}
			conclusions = append(conclusions, r.Conclusion)
		}
	}
	var combined struct {
		Statuses []gitHubPullStatusCombinedStatuses `json:"statuses"`
	}
	if resp, err := g.get(ctx, commit+"/status", ""); err == nil && githubDecode(resp, &combined) == nil {
		for _, s := range combined.Statuses {
			if s.State == "pending" {
				running++
				continue
			}
			conclusions = append(conclusions, s.State)
		}
	}
	return conclusions, running
}

type gitHubPullStatusPrHead struct {
	SHA string `json:"sha"`
}

type gitHubPullStatusRunsCheckRuns struct {
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

type gitHubPullStatusCombinedStatuses struct {
	State string `json:"state"`
}

func (g *GitHub) login(ctx context.Context) string {
	if g.Me != "" {
		return g.Me
	}
	var user struct {
		Login string `json:"login"`
	}
	if resp, err := g.get(ctx, "/user", ""); err == nil && githubDecode(resp, &user) == nil {
		g.Me = user.Login
	}
	return g.Me
}

func (g *GitHub) MyReviewSince(ctx context.Context, ref string, since time.Time) (ReviewOutcome, bool) {
	repo, num, ok := strings.Cut(ref, "#")
	if !ok || g.Token == "" {
		return ReviewOutcome{}, false
	}
	me := g.login(ctx)
	if me == "" {
		return ReviewOutcome{}, false
	}
	var out ReviewOutcome
	var reviews []struct {
		State       string           `json:"state"`
		SubmittedAt string           `json:"submitted_at"`
		User        gitHubPullPrUser `json:"user"`
	}
	resp, err := g.get(ctx, reposSegment+repo+pullsSegment+num+reviewsPageQuery, "")
	if err != nil || githubDecode(resp, &reviews) != nil {
		return ReviewOutcome{}, false
	}
	for _, r := range reviews {
		if r.User.Login == me && !trackerTime(r.SubmittedAt).Before(since) {
			out.Approved = out.Approved || r.State == "APPROVED"
			out.ChangesRequested = out.ChangesRequested || r.State == "CHANGES_REQUESTED"
		}
	}
	for _, path := range []string{reposSegment + repo + pullsSegment + num + "/comments?per_page=100", reposSegment + repo + "/issues/" + num + "/comments?per_page=100"} {
		out.Comments += g.myCommentsSince(ctx, path, me, since)
	}
	return out, true
}

func (g *GitHub) myCommentsSince(ctx context.Context, path, me string, since time.Time) int {
	var comments []struct {
		CreatedAt string           `json:"created_at"`
		User      gitHubPullPrUser `json:"user"`
	}
	resp, err := g.get(ctx, path, "")
	if err != nil || githubDecode(resp, &comments) != nil {
		return 0
	}
	n := 0
	for _, c := range comments {
		if c.User.Login == me && !trackerTime(c.CreatedAt).Before(since) {
			n++
		}
	}
	return n
}

// AnsweredSince is the Answerer for a pull request: a comment of mine after
// the moment, and a head commit after it.
func (g *GitHub) AnsweredSince(ctx context.Context, ref string, at time.Time) string {
	if at.IsZero() {
		return ""
	}
	out, ok := g.MyReviewSince(ctx, ref, at)
	if !ok || out.Comments == 0 {
		return ""
	}
	repo, num, _ := strings.Cut(ref, "#")
	var pr struct {
		Head gitHubPullStatusPrHead `json:"head"`
	}
	resp, err := g.get(ctx, reposSegment+repo+pullsSegment+num, "")
	if err != nil || githubDecode(resp, &pr) != nil || pr.Head.SHA == "" {
		return ""
	}
	var c struct {
		Commit gitHubAnsweredSinceCommit `json:"commit"`
	}
	resp, err = g.get(ctx, reposSegment+repo+commitsSegment+pr.Head.SHA, "")
	if err != nil || githubDecode(resp, &c) != nil || trackerTime(c.Commit.Committer.Date).Before(at) {
		return ""
	}
	return "you replied and pushed after it"
}

type gitHubAnsweredSinceCommitCommitter struct {
	Date string `json:"date"`
}

type gitHubAnsweredSinceCommit struct {
	Committer gitHubAnsweredSinceCommitCommitter `json:"committer"`
}

var githubCommentID = regexp.MustCompile(`/(?:issues/comments|pulls/comments|reviews)/(\d+)$`)

// githubCommentKey names a comment the way its webhook does (c<id> for a
// comment, r<id> for a review), so the same comment seen by the poll and by
// the webhook is one event, not two runs.
func githubCommentKey(apiURL string) string {
	m := githubCommentID.FindStringSubmatch(apiURL)
	if m == nil {
		return ""
	}
	if strings.Contains(apiURL, "/reviews/") {
		return "r" + m[1]
	}
	return "c" + m[1]
}
