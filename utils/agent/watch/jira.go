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

type Jira struct {
	URL, Email, Token, Project string
	Me                         string
	Client                     *http.Client
	zone                       *time.Location
}

func NewJira(s Secrets, project string) *Jira {
	return &Jira{URL: s.JiraURL, Email: s.JiraEmail, Token: s.JiraToken, Project: project}
}

func (j *Jira) Name() string { return "jira" }

type jiraIssue struct {
	Key    string `json:"key"`
	Fields struct {
		Summary     string          `json:"summary"`
		Description json.RawMessage `json:"description"`
		Labels      []string        `json:"labels"`
		Status      struct {
			Name string `json:"name"`
		} `json:"status"`
		Assignee *struct {
			AccountID   string `json:"accountId"`
			DisplayName string `json:"displayName"`
		} `json:"assignee"`
		Creator *struct {
			AccountID   string `json:"accountId"`
			DisplayName string `json:"displayName"`
		} `json:"creator"`
		Created string `json:"created"`
		Updated string `json:"updated"`
		Comment *struct {
			Total    int           `json:"total"`
			Comments []jiraComment `json:"comments"`
		} `json:"comment"`
	} `json:"fields"`
}

type jiraComment struct {
	ID     string `json:"id"`
	Author struct {
		AccountID   string `json:"accountId"`
		DisplayName string `json:"displayName"`
	} `json:"author"`
	Body    json.RawMessage `json:"body"`
	Created string          `json:"created"`
}

func (j *Jira) Poll(ctx context.Context, cursor Cursor) ([]Event, Cursor, error) {
	if strings.TrimSpace(j.URL) == "" || strings.TrimSpace(j.Email) == "" || strings.TrimSpace(j.Token) == "" {
		return nil, cursor, ErrNoToken
	}
	if j.Me == "" {
		j.Me = cursor["me"]
		if zone, err := time.LoadLocation(cursor["zone"]); err == nil && cursor["zone"] != "" {
			j.zone = zone
		}
	}
	if j.Me == "" {
		var me struct {
			AccountID string `json:"accountId"`
			TimeZone  string `json:"timeZone"`
		}
		if err := j.get(ctx, "/rest/api/3/myself", nil, &me); err != nil {
			return nil, cursor, err
		}
		if me.AccountID == "" {
			return nil, cursor, fmt.Errorf("jira: myself has no accountId")
		}
		j.Me = me.AccountID
		if zone, err := time.LoadLocation(me.TimeZone); err == nil && me.TimeZone != "" {
			j.zone = zone
		}
	}
	if j.zone == nil {
		j.zone = time.UTC
	}

	now := time.Now()
	issuesSince := cursorTime(cursor["issues"], now)
	commentsSince := cursorTime(cursor["comments"], now)

	jql := fmt.Sprintf("updated >= %q ORDER BY updated ASC", issuesSince.In(j.zone).Format("2006-01-02 15:04"))
	if j.Project != "" {
		jql = fmt.Sprintf("project = %q AND ", j.Project) + jql
	}
	var search struct {
		Issues []jiraIssue `json:"issues"`
	}
	params := url.Values{
		"jql":        {jql},
		"fields":     {"summary,description,labels,status,assignee,creator,created,updated,comment"},
		"maxResults": {"50"},
	}
	if err := j.get(ctx, "/rest/api/3/search/jql", params, &search); err != nil {
		return nil, cursor, err
	}

	var events []Event
	newestIssue, newestComment := issuesSince, commentsSince
	var mine []jiraIssue
	for _, issue := range search.Issues {
		created, updated := trackerTime(issue.Fields.Created), trackerTime(issue.Fields.Updated)
		newestIssue = later(newestIssue, updated)
		assignedToMe := issue.Fields.Assignee != nil && isMe(j.Me, issue.Fields.Assignee.AccountID)
		if assignedToMe {
			mine = append(mine, issue)
		}
		if !created.After(issuesSince) {
			continue
		}
		e := Event{
			Key:    "jira:" + issue.Key,
			Source: "jira",
			Kind:   KindIssueNew,
			Ref:    issue.Key,
			Title:  issue.Fields.Summary,
			Body:   clip(jiraText(issue.Fields.Description), bodyMax),
			URL:    strings.TrimRight(j.URL, "/") + "/browse/" + issue.Key,
			Labels: issue.Fields.Labels,
			State:  issue.Fields.Status.Name,
			Mine:   assignedToMe,
			At:     created,
		}
		if issue.Fields.Assignee != nil {
			e.Assignee = issue.Fields.Assignee.DisplayName
		}
		if issue.Fields.Creator != nil {
			e.Author = issue.Fields.Creator.DisplayName
			e.Self = isMe(j.Me, issue.Fields.Creator.AccountID)
		}
		events = append(events, e)
	}

	for _, issue := range mine {
		var page struct {
			Comments []jiraComment `json:"comments"`
		}
		if c := issue.Fields.Comment; c != nil && c.Total <= len(c.Comments) {
			page.Comments = c.Comments
		} else {
			params := url.Values{"orderBy": {"-created"}, "maxResults": {"20"}}
			if err := j.get(ctx, "/rest/api/3/issue/"+url.PathEscape(issue.Key)+"/comment", params, &page); err != nil {
				return nil, cursor, err
			}
		}
		for _, c := range page.Comments {
			created := trackerTime(c.Created)
			if !created.After(commentsSince) {
				continue
			}
			newestComment = later(newestComment, created)
			if isMe(j.Me, c.Author.AccountID) {
				continue
			}
			events = append(events, Event{
				Key:    "jira:" + issue.Key + ":c" + c.ID,
				Source: "jira",
				Kind:   KindIssueComment,
				Ref:    issue.Key,
				Title:  issue.Fields.Summary,
				Body:   clip(jiraText(c.Body), bodyMax),
				URL:    strings.TrimRight(j.URL, "/") + "/browse/" + issue.Key,
				Author: c.Author.DisplayName,
				State:  issue.Fields.Status.Name,
				Mine:   true,
				At:     created,
			})
		}
	}

	next := Cursor{"me": j.Me}
	if j.zone != time.UTC {
		next["zone"] = j.zone.String()
	}
	setCursorTime(next, cursor, "issues", newestIssue, issuesSince)
	setCursorTime(next, cursor, "comments", newestComment, commentsSince)
	return events, next, nil
}

func (j *Jira) get(ctx context.Context, path string, params url.Values, out any) error {
	endpoint := strings.TrimRight(j.URL, "/") + path
	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("jira %s: %w", path, err)
	}
	req.SetBasicAuth(j.Email, j.Token)
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
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("jira %s: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jira %s: HTTP %d: %s", path, resp.StatusCode, clip(string(raw), bodyMax))
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("jira %s: bad response: %w", path, err)
	}
	return nil
}
