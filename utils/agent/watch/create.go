package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Creating in the tracker: an epic (a parent) and the tickets under it, from
// an idea page. The same credentials the watch polls with.

// NewIssue is one ticket to create.
type NewIssue struct {
	Title string
	Body  string
	// Parent is the epic's key (Jira) or the parent issue's id (Linear).
	Parent string
	// Epic makes this the parent itself.
	Epic bool
}

// Created is what the tracker gave back.
type Created struct {
	ID  string // Linear's id, Jira's id; what children name as Parent on Linear
	Ref string // the key people use: ABC-123
	URL string
}

// Creator is a tracker that can take new tickets.
type Creator interface {
	Create(ctx context.Context, in NewIssue) (Created, error)
}

// CreatorFor is the tracker to create in, or nil when it has no credentials.
func CreatorFor(s Secrets, tracker, project string) Creator {
	switch strings.ToLower(strings.TrimSpace(tracker)) {
	case "jira":
		if s.JiraToken != "" && s.JiraURL != "" {
			return NewJira(s, project)
		}
	case "linear":
		if s.Linear != "" {
			return NewLinear(s, project)
		}
	}
	return nil
}

func (j *Jira) sendJSON(ctx context.Context, method, path string, body any, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	endpoint := strings.TrimRight(j.URL, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, endpoint, strings.NewReader(string(raw)))
	if err != nil {
		return fmt.Errorf("jira %s: %w", path, err)
	}
	req.SetBasicAuth(j.Email, j.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	client := j.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("jira %s: %w", path, err)
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("jira %s: HTTP %d: %s", path, resp.StatusCode, clip(string(answer), bodyMax))
	}
	if out != nil && len(answer) > 0 {
		if err := json.Unmarshal(answer, out); err != nil {
			return fmt.Errorf("jira %s: bad response: %w", path, err)
		}
	}
	return nil
}

// Create makes an issue in the Jira project: an Epic, or a Task under one.
func (j *Jira) Create(ctx context.Context, in NewIssue) (Created, error) {
	if j.Project == "" {
		return Created{}, fmt.Errorf("jira: say which project (its key) the tickets go in")
	}
	kind := "Task"
	if in.Epic {
		kind = "Epic"
	}
	fields := map[string]any{
		"project":   map[string]string{"key": j.Project},
		"summary":   clip(strings.TrimSpace(in.Title), 250),
		"issuetype": map[string]string{"name": kind},
	}
	if strings.TrimSpace(in.Body) != "" {
		fields["description"] = adf(in.Body)
	}
	if in.Parent != "" && !in.Epic {
		fields["parent"] = map[string]string{"key": in.Parent}
	}
	var out struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	}
	if err := j.sendJSON(ctx, http.MethodPost, "/rest/api/3/issue", map[string]any{"fields": fields}, &out); err != nil {
		return Created{}, err
	}
	return Created{ID: out.ID, Ref: out.Key, URL: strings.TrimRight(j.URL, "/") + "/browse/" + out.Key}, nil
}

// CreatePage writes a Confluence page in the space, on the same Atlassian
// site as the Jira credentials. body is storage-format XHTML.
func (j *Jira) CreatePage(ctx context.Context, space, title, body string) (Created, error) {
	if strings.TrimSpace(space) == "" {
		return Created{}, fmt.Errorf("confluence: say which space (its key) the page goes in")
	}
	payload := map[string]any{
		"type":  "page",
		"title": clip(strings.TrimSpace(title), 250),
		"space": map[string]string{"key": space},
		"body":  map[string]any{"storage": map[string]string{"value": body, "representation": "storage"}},
	}
	var out struct {
		ID    string `json:"id"`
		Links struct {
			Base  string `json:"base"`
			WebUI string `json:"webui"`
		} `json:"_links"`
	}
	if err := j.sendJSON(ctx, http.MethodPost, "/wiki/rest/api/content", payload, &out); err != nil {
		return Created{}, err
	}
	base := out.Links.Base
	if base == "" {
		base = strings.TrimRight(j.URL, "/") + "/wiki"
	}
	return Created{ID: out.ID, Ref: "page " + out.ID, URL: strings.TrimRight(base, "/") + out.Links.WebUI}, nil
}

// teamID is the Linear team's id for its key.
func (l *Linear) teamID(ctx context.Context) (string, error) {
	if l.Team == "" {
		return "", fmt.Errorf("linear: say which team (its key, as in ABC-123) the tickets go in")
	}
	var out struct {
		Teams struct {
			Nodes []struct {
				ID string `json:"id"`
			} `json:"nodes"`
		} `json:"teams"`
	}
	if err := l.query(ctx, "query { teams(filter: {key: {eq: "+jsonString(l.Team)+"}}) { nodes { id } } }", &out); err != nil {
		return "", err
	}
	if len(out.Teams.Nodes) == 0 {
		return "", fmt.Errorf("linear: no team with key %s", l.Team)
	}
	return out.Teams.Nodes[0].ID, nil
}

// Create makes an issue in the Linear team; a parent makes it a sub-issue.
func (l *Linear) Create(ctx context.Context, in NewIssue) (Created, error) {
	team, err := l.teamID(ctx)
	if err != nil {
		return Created{}, err
	}
	input := "teamId: " + jsonString(team) + ", title: " + jsonString(clip(strings.TrimSpace(in.Title), 250))
	if strings.TrimSpace(in.Body) != "" {
		input += ", description: " + jsonString(in.Body)
	}
	if in.Parent != "" && !in.Epic {
		input += ", parentId: " + jsonString(in.Parent)
	}
	var out struct {
		IssueCreate struct {
			Success bool `json:"success"`
			Issue   struct {
				ID         string `json:"id"`
				Identifier string `json:"identifier"`
				URL        string `json:"url"`
			} `json:"issue"`
		} `json:"issueCreate"`
	}
	if err := l.query(ctx, "mutation { issueCreate(input: {"+input+"}) { success issue { id identifier url } } }", &out); err != nil {
		return Created{}, err
	}
	if !out.IssueCreate.Success || out.IssueCreate.Issue.ID == "" {
		return Created{}, fmt.Errorf("linear: the issue was refused")
	}
	return Created{ID: out.IssueCreate.Issue.ID, Ref: out.IssueCreate.Issue.Identifier, URL: out.IssueCreate.Issue.URL}, nil
}

// SiteURL is the Atlassian site, for links.
func (j *Jira) SiteURL() string { return strings.TrimRight(j.URL, "/") }

// ConfluenceSpaces is the spaces the credentials can write to, for a picker.
func (j *Jira) ConfluenceSpaces(ctx context.Context) ([]Status, error) {
	var out struct {
		Results []struct {
			Key  string `json:"key"`
			Name string `json:"name"`
		} `json:"results"`
	}
	if err := j.get(ctx, "/wiki/rest/api/space", url.Values{"limit": {"50"}}, &out); err != nil {
		return nil, err
	}
	var spaces []Status
	for _, s := range out.Results {
		spaces = append(spaces, Status{ID: s.Key, Name: s.Name})
	}
	return spaces, nil
}
