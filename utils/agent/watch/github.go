package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
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

var githubReasons = map[string]struct {
	kind Kind
	mine bool
}{
	"review_requested": {KindReviewRequested, false},
	"mention":          {KindPRComment, true},
	"author":           {KindPRComment, true},
	"comment":          {KindPRComment, true},
	"team_mention":     {KindPRComment, false},
	"ci_activity":      {KindCIFailed, true},
}

type githubThread struct {
	ID        string `json:"id"`
	Reason    string `json:"reason"`
	UpdatedAt string `json:"updated_at"`
	Subject   struct {
		Title            string `json:"title"`
		URL              string `json:"url"`
		Type             string `json:"type"`
		LatestCommentURL string `json:"latest_comment_url"`
	} `json:"subject"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

func (g *GitHub) Poll(ctx context.Context, cursor Cursor) ([]Event, Cursor, error) {
	if g.Token == "" {
		return nil, cursor, ErrNoToken
	}
	if cursor == nil {
		cursor = Cursor{}
	}
	if g.Me == "" {
		g.Me = cursor["me"]
	}
	if g.Me == "" {
		var user struct {
			Login string `json:"login"`
		}
		resp, err := g.get(ctx, "/user", "")
		if err != nil {
			return nil, cursor, err
		}
		err = githubDecode(resp, &user)
		if err != nil {
			return nil, cursor, err
		}
		g.Me = user.Login
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
	states := map[string]string{}
	for _, t := range threads {
		if !g.wantsRepo(t.Repository.FullName) {
			continue
		}
		r, ok := githubReasons[t.Reason]
		if !ok {
			continue
		}
		if r.kind == KindCIFailed {
			if t.Subject.Type != "CheckSuite" {
				continue
			}
			at, _ := time.Parse(time.RFC3339, t.UpdatedAt)
			events = append(events, Event{
				Key:    "github:ci:" + t.Repository.FullName + ":" + t.ID + ":" + t.UpdatedAt,
				Source: g.Name(),
				Kind:   KindCIFailed,
				Ref:    t.Repository.FullName,
				Title:  firstNonEmptyText(t.Subject.Title, "a workflow run failed"),
				URL:    "https://github.com/" + t.Repository.FullName + "/actions",
				Mine:   true,
				At:     at,
			})
			continue
		}
		if t.Subject.Type != "PullRequest" {
			continue
		}
		number := t.Subject.URL[strings.LastIndex(t.Subject.URL, "/")+1:]
		ref := t.Repository.FullName + "#" + number
		at, _ := time.Parse(time.RFC3339, t.UpdatedAt)
		state := g.pullState(ctx, states, t.Subject.URL)
		author, body, bot, hasComment := g.latestComment(ctx, t.Subject.LatestCommentURL, t.Subject.URL)
		if r.kind == KindPRComment && (!hasComment || (author != "" && strings.EqualFold(author, g.Me))) {
			continue
		}
		events = append(events, Event{
			Key:    "github:" + ref + ":" + t.ID + ":" + t.UpdatedAt,
			Source: g.Name(),
			Kind:   r.kind,
			Ref:    ref,
			Title:  t.Subject.Title,
			Body:   body,
			URL:    "https://github.com/" + t.Repository.FullName + "/pull/" + number,
			Author: author,
			Mine:   r.mine,
			Bot:    bot,
			State:  state,
			At:     at,
		})
	}
	return events, next, nil
}

func (g *GitHub) latestComment(ctx context.Context, commentURL, subjectURL string) (author, body string, bot, hasComment bool) {
	if commentURL == "" || commentURL == subjectURL {
		return "", "", false, false
	}
	path := strings.TrimPrefix(commentURL, "https://api.github.com")
	if path == commentURL {
		return "", "", false, true
	}
	resp, err := g.get(ctx, path, "")
	if err != nil {
		return "", "", false, true
	}
	defer resp.Body.Close()
	var c struct {
		Body string `json:"body"`
		User struct {
			Login string `json:"login"`
			Type  string `json:"type"`
		} `json:"user"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&c) != nil {
		return "", "", false, true
	}
	bot = githubBot(c.User.Login, c.User.Type)
	return c.User.Login, clip(strings.TrimSpace(c.Body), bodyMax), bot, true
}

func (g *GitHub) pullState(ctx context.Context, cache map[string]string, apiURL string) string {
	if apiURL == "" {
		return ""
	}
	if state, ok := cache[apiURL]; ok {
		return state
	}
	cache[apiURL] = ""
	path := strings.TrimPrefix(apiURL, "https://api.github.com")
	if path == apiURL {
		return ""
	}
	resp, err := g.get(ctx, path, "")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var pr struct {
		State  string `json:"state"`
		Merged bool   `json:"merged"`
		Draft  bool   `json:"draft"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&pr) != nil {
		return ""
	}
	state := pr.State
	if pr.Merged {
		state = "merged"
	} else if pr.Draft && state == "open" {
		state = "draft"
	}
	cache[apiURL] = state
	return state
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
		base = "https://api.github.com"
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
	return g.pullState(ctx, map[string]string{}, "https://api.github.com/repos/"+repo+"/pulls/"+num)
}

func (g *GitHub) PullStatus(ctx context.Context, ref string) (PullStatus, bool) {
	repo, num, ok := strings.Cut(ref, "#")
	if !ok || g.Token == "" {
		return PullStatus{}, false
	}
	var pr struct {
		State  string `json:"state"`
		Merged bool   `json:"merged"`
		Draft  bool   `json:"draft"`
		Head   struct {
			SHA string `json:"sha"`
		} `json:"head"`
		RequestedReviewers []struct {
			Login string `json:"login"`
		} `json:"requested_reviewers"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	resp, err := g.get(ctx, "/repos/"+repo+"/pulls/"+num, "")
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

	var reviews []struct {
		State string `json:"state"`
		User  struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	if resp, err := g.get(ctx, "/repos/"+repo+"/pulls/"+num+"/reviews?per_page=100", ""); err == nil && githubDecode(resp, &reviews) == nil {
		last := map[string]string{}
		for _, r := range reviews {
			switch r.State {
			case "APPROVED", "CHANGES_REQUESTED", "DISMISSED":
				last[r.User.Login] = r.State
			}
		}
		out.Review = "none"
		for _, st := range last {
			if st == "APPROVED" && out.Review != "changes" {
				out.Review = "approved"
			}
			if st == "CHANGES_REQUESTED" {
				out.Review = "changes"
			}
		}
		if out.Review == "none" && len(pr.RequestedReviewers) > 0 {
			out.Review = "pending"
		}
	}

	var conclusions []string
	running := 0
	if pr.Head.SHA != "" {
		var runs struct {
			CheckRuns []struct {
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
			} `json:"check_runs"`
		}
		if resp, err := g.get(ctx, "/repos/"+repo+"/commits/"+pr.Head.SHA+"/check-runs?per_page=100", ""); err == nil && githubDecode(resp, &runs) == nil {
			for _, r := range runs.CheckRuns {
				if r.Status != "completed" {
					running++
					continue
				}
				conclusions = append(conclusions, r.Conclusion)
			}
		}
		var combined struct {
			Statuses []struct {
				State string `json:"state"`
			} `json:"statuses"`
		}
		if resp, err := g.get(ctx, "/repos/"+repo+"/commits/"+pr.Head.SHA+"/status", ""); err == nil && githubDecode(resp, &combined) == nil {
			for _, s := range combined.Statuses {
				if s.State == "pending" {
					running++
					continue
				}
				conclusions = append(conclusions, s.State)
			}
		}
	}
	out.Checks = checksVerdict(conclusions, running)
	return out, true
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
		State       string `json:"state"`
		SubmittedAt string `json:"submitted_at"`
		User        struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	resp, err := g.get(ctx, "/repos/"+repo+"/pulls/"+num+"/reviews?per_page=100", "")
	if err != nil || githubDecode(resp, &reviews) != nil {
		return ReviewOutcome{}, false
	}
	for _, r := range reviews {
		if r.User.Login != me || trackerTime(r.SubmittedAt).Before(since) {
			continue
		}
		switch r.State {
		case "APPROVED":
			out.Approved = true
		case "CHANGES_REQUESTED":
			out.ChangesRequested = true
		}
	}
	for _, path := range []string{"/repos/" + repo + "/pulls/" + num + "/comments?per_page=100", "/repos/" + repo + "/issues/" + num + "/comments?per_page=100"} {
		var comments []struct {
			CreatedAt string `json:"created_at"`
			User      struct {
				Login string `json:"login"`
			} `json:"user"`
		}
		if resp, err := g.get(ctx, path, ""); err == nil && githubDecode(resp, &comments) == nil {
			for _, c := range comments {
				if c.User.Login == me && !trackerTime(c.CreatedAt).Before(since) {
					out.Comments++
				}
			}
		}
	}
	return out, true
}
