package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// GitLab polls pending todos: GitLab already filters them to what is
// addressed to me, so one small request covers every project.
type GitLab struct {
	URL    string
	Token  string
	Me     string
	Client *http.Client
}

// NewGitLab uses the saved token; URL falls back to gitlab.com.
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
}

type gitlabTodo struct {
	ID         int64  `json:"id"`
	ActionName string `json:"action_name"`
	TargetType string `json:"target_type"`
	TargetURL  string `json:"target_url"`
	Body       string `json:"body"`
	CreatedAt  string `json:"created_at"`
	Project    struct {
		PathWithNamespace string `json:"path_with_namespace"`
	} `json:"project"`
	Target struct {
		IID   int64  `json:"iid"`
		Title string `json:"title"`
	} `json:"target"`
	Author struct {
		Username string `json:"username"`
	} `json:"author"`
}

// Poll returns merge-request todos newer than cursor["lastId"]; the cursor
// advances to the largest id in the response, skipped todos included.
func (g *GitLab) Poll(ctx context.Context, cursor Cursor) ([]Event, Cursor, error) {
	if g.Token == "" {
		return nil, cursor, ErrNoToken
	}
	base := g.URL
	if base == "" {
		base = "https://gitlab.com"
	}
	path := "/api/v4/todos?state=pending&per_page=50"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+path, nil)
	if err != nil {
		return nil, cursor, fmt.Errorf("gitlab todos: %v", err)
	}
	req.Header.Set("PRIVATE-TOKEN", g.Token)
	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, cursor, fmt.Errorf("gitlab todos: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 120))
		return nil, cursor, fmt.Errorf("gitlab todos: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var todos []gitlabTodo
	if err := json.NewDecoder(resp.Body).Decode(&todos); err != nil {
		return nil, cursor, fmt.Errorf("gitlab todos: decoding: %v", err)
	}

	lastID, _ := strconv.ParseInt(cursor["lastId"], 10, 64)
	maxID := lastID
	var events []Event
	for _, t := range todos {
		maxID = max(maxID, t.ID)
		if t.ID <= lastID || t.TargetType != "MergeRequest" {
			continue
		}
		kind, ok := gitlabActions[t.ActionName]
		if !ok {
			continue
		}
		at, _ := time.Parse(time.RFC3339, t.CreatedAt)
		events = append(events, Event{
			Key:    "gitlab:todo:" + strconv.FormatInt(t.ID, 10),
			Source: g.Name(),
			Kind:   kind,
			Ref:    t.Project.PathWithNamespace + "!" + strconv.FormatInt(t.Target.IID, 10),
			Title:  t.Target.Title,
			Body:   gitlabTruncate(t.Body, 200),
			URL:    t.TargetURL,
			Author: t.Author.Username,
			// A review request is on someone else's merge request; every
			// other todo is on something of mine.
			Mine: kind != KindReviewRequested,
			At:   at,
		})
	}

	next := Cursor{}
	for k, v := range cursor {
		next[k] = v
	}
	if maxID > 0 {
		next["lastId"] = strconv.FormatInt(maxID, 10)
	}
	return events, next, nil
}

func gitlabTruncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
