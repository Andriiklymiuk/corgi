package watch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ReviewThread is one conversation on a pull request: an inline thread a
// reply can go into, or a review's summary body (Kind "review"), which has
// no thread to resolve.
type ReviewThread struct {
	Kind     string       `json:"kind"`
	ID       string       `json:"id,omitempty"`
	Resolved bool         `json:"resolved"`
	Outdated bool         `json:"outdated,omitempty"`
	Path     string       `json:"path,omitempty"`
	Line     int          `json:"line,omitempty"`
	State    string       `json:"state,omitempty"`
	Notes    []ThreadNote `json:"notes"`
}

// ThreadNote.ID is what a reply names: GitHub's comment databaseId
// (in_reply_to), GitLab's note id.
type ThreadNote struct {
	ID     string    `json:"id"`
	Author string    `json:"author"`
	At     time.Time `json:"at"`
	Body   string    `json:"body"`
}

type threadsClient struct {
	http    *http.Client
	github  string
	gitlab  func(Secrets, string) (string, error)
	ghToken func() string
}

var defaultThreadsClient = threadsClient{
	http:    &http.Client{Timeout: 30 * time.Second},
	github:  "https://api.github.com",
	gitlab:  gitlabMRAPI,
	ghToken: githubAuthToken,
}

// ReviewThreads reads every review thread and review summary on a pull or
// merge request, resolved ones included, oldest first.
func ReviewThreads(ctx context.Context, s Secrets, link string) ([]ReviewThread, error) {
	return defaultThreadsClient.threads(ctx, s, link)
}

func (c threadsClient) threads(ctx context.Context, s Secrets, link string) ([]ReviewThread, error) {
	switch {
	case strings.Contains(link, "github.com/"):
		m := githubPRPath.FindStringSubmatch(link)
		if m == nil {
			return nil, fmt.Errorf("cannot read a repo and number out of %s", link)
		}
		token := strings.TrimSpace(s.GitHub)
		if token == "" {
			token = strings.TrimSpace(c.ghToken())
		}
		if token == "" {
			return nil, ErrNoToken
		}
		owner, repo, _ := strings.Cut(m[1], "/")
		return c.githubThreads(ctx, token, owner, repo, m[2], link)
	case strings.Contains(link, "/-/merge_requests/"):
		if s.GitLab == "" {
			return nil, ErrNoToken
		}
		api, err := c.gitlab(s, link)
		if err != nil {
			return nil, err
		}
		return c.gitlabThreads(ctx, s.GitLab, api, link)
	}
	return nil, fmt.Errorf("%s is not a GitHub pull request or a GitLab merge request", link)
}

const githubThreadsQuery = `query($o:String!,$r:String!,$n:Int!){repository(owner:$o,name:$r){pullRequest(number:$n){
reviewThreads(first:100){nodes{id isResolved isOutdated path line originalLine comments(first:50){nodes{databaseId author{login} createdAt body}}}}
reviews(first:50){nodes{databaseId author{login} state submittedAt body}}}}}`

type githubNote struct {
	DatabaseID  int64                  `json:"databaseId"`
	Author      struct{ Login string } `json:"author"`
	CreatedAt   time.Time              `json:"createdAt"`
	SubmittedAt time.Time              `json:"submittedAt"`
	State       string                 `json:"state"`
	Body        string                 `json:"body"`
}

func (c threadsClient) githubThreads(ctx context.Context, token, owner, repo, number, link string) ([]ReviewThread, error) {
	var n int
	if _, err := fmt.Sscan(number, &n); err != nil {
		return nil, fmt.Errorf("cannot read a number out of %s", link)
	}
	payload, _ := json.Marshal(map[string]any{"query": githubThreadsQuery, "variables": map[string]any{"o": owner, "r": repo, "n": n}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.github, "/")+"/graphql", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	var out struct {
		Data struct {
			Repository struct {
				PullRequest *struct {
					ReviewThreads struct {
						Nodes []struct {
							ID           string `json:"id"`
							IsResolved   bool   `json:"isResolved"`
							IsOutdated   bool   `json:"isOutdated"`
							Path         string `json:"path"`
							Line         int    `json:"line"`
							OriginalLine int    `json:"originalLine"`
							Comments     struct {
								Nodes []githubNote `json:"nodes"`
							} `json:"comments"`
						} `json:"nodes"`
					} `json:"reviewThreads"`
					Reviews struct {
						Nodes []githubNote `json:"nodes"`
					} `json:"reviews"`
				} `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := c.do(req, link, &out); err != nil {
		return nil, err
	}
	if len(out.Errors) > 0 {
		return nil, fmt.Errorf("reading the threads of %s: %s", link, out.Errors[0].Message)
	}
	pr := out.Data.Repository.PullRequest
	if pr == nil {
		return nil, fmt.Errorf("%s: no such pull request this token can see", link)
	}
	var threads []ReviewThread
	for _, r := range pr.Reviews.Nodes {
		if strings.TrimSpace(r.Body) == "" {
			continue
		}
		threads = append(threads, ReviewThread{Kind: "review", State: strings.ToLower(r.State),
			Notes: []ThreadNote{{ID: fmt.Sprint(r.DatabaseID), Author: r.Author.Login, At: r.SubmittedAt, Body: r.Body}}})
	}
	for _, t := range pr.ReviewThreads.Nodes {
		line := t.Line
		if line == 0 {
			line = t.OriginalLine
		}
		th := ReviewThread{Kind: "thread", ID: t.ID, Resolved: t.IsResolved, Outdated: t.IsOutdated, Path: t.Path, Line: line}
		for _, n := range t.Comments.Nodes {
			th.Notes = append(th.Notes, ThreadNote{ID: fmt.Sprint(n.DatabaseID), Author: n.Author.Login, At: n.CreatedAt, Body: n.Body})
		}
		threads = append(threads, th)
	}
	return threads, nil
}

func (c threadsClient) gitlabThreads(ctx context.Context, token, api, link string) ([]ReviewThread, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api+"/discussions?per_page=100", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	var out []struct {
		ID    string `json:"id"`
		Notes []struct {
			ID         int64     `json:"id"`
			Body       string    `json:"body"`
			System     bool      `json:"system"`
			Resolvable bool      `json:"resolvable"`
			Resolved   bool      `json:"resolved"`
			CreatedAt  time.Time `json:"created_at"`
			Author     struct {
				Username string `json:"username"`
			} `json:"author"`
			Position *struct {
				NewPath string `json:"new_path"`
				NewLine int    `json:"new_line"`
				OldPath string `json:"old_path"`
				OldLine int    `json:"old_line"`
			} `json:"position"`
		} `json:"notes"`
	}
	if err := c.do(req, link, &out); err != nil {
		return nil, err
	}
	var threads []ReviewThread
	for _, d := range out {
		th := ReviewThread{Kind: "comment", ID: d.ID, Resolved: true}
		for _, n := range d.Notes {
			if n.System {
				continue
			}
			if n.Resolvable {
				th.Kind = "thread"
				th.Resolved = th.Resolved && n.Resolved
			}
			if p := n.Position; p != nil && th.Path == "" {
				th.Path, th.Line = p.NewPath, p.NewLine
				if th.Path == "" {
					th.Path, th.Line = p.OldPath, p.OldLine
				}
			}
			th.Notes = append(th.Notes, ThreadNote{ID: fmt.Sprint(n.ID), Author: n.Author.Username, At: n.CreatedAt, Body: n.Body})
		}
		if len(th.Notes) == 0 {
			continue
		}
		if th.Kind == "comment" {
			th.Resolved = false
		}
		threads = append(threads, th)
	}
	return threads, nil
}

func (c threadsClient) do(req *http.Request, link string, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("reading the threads of %s: %w", link, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("reading the threads of %s: HTTP %d: %s", link, resp.StatusCode, clip(string(body), bodyMax))
	}
	return json.Unmarshal(body, out)
}
