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
		IssueType struct {
			Subtask bool `json:"subtask"`
		} `json:"issuetype"`
		Parent *struct {
			Key    string `json:"key"`
			Fields struct {
				Summary string `json:"summary"`
			} `json:"fields"`
		} `json:"parent"`
		Subtasks []struct {
			Key    string `json:"key"`
			Fields struct {
				Status struct {
					Name string `json:"name"`
				} `json:"status"`
				Assignee *struct {
					AccountID string `json:"accountId"`
				} `json:"assignee"`
			} `json:"fields"`
		} `json:"subtasks"`
	} `json:"fields"`
	Changelog struct {
		Histories []jiraHistory `json:"histories"`
	} `json:"changelog"`
}

type jiraHistory struct {
	ID      string `json:"id"`
	Created string `json:"created"`
	Author  struct {
		AccountID string `json:"accountId"`
	} `json:"author"`
	Items []struct {
		Field    string `json:"field"`
		To       string `json:"to"`
		ToString string `json:"toString"`
	} `json:"items"`
}

// The newest change since `since` by somebody else that handed me the ticket
// or moved it: that is the moment old work became mine.
func (j *Jira) becameMine(issue jiraIssue, since time.Time) (jiraHistory, bool) {
	var best jiraHistory
	found := false
	for _, h := range issue.Changelog.Histories {
		at := trackerTime(h.Created)
		if !at.After(since) || isMe(j.Me, h.Author.AccountID) {
			continue
		}
		for _, it := range h.Items {
			handed := it.Field == "assignee" && isMe(j.Me, it.To)
			moved := it.Field == "status"
			if (handed || moved) && (!found || at.After(trackerTime(best.Created))) {
				best, found = h, true
			}
		}
	}
	return best, found
}

func (j *Jira) resolveMe(ctx context.Context) error {
	if j.Me == "" {
		var me struct {
			AccountID string `json:"accountId"`
			TimeZone  string `json:"timeZone"`
		}
		if err := j.get(ctx, "/rest/api/3/myself", nil, &me); err != nil {
			return err
		}
		if me.AccountID == "" {
			return fmt.Errorf("jira: myself has no accountId")
		}
		j.Me = me.AccountID
		if zone, err := time.LoadLocation(me.TimeZone); err == nil && me.TimeZone != "" {
			j.zone = zone
		}
	}
	if j.zone == nil {
		j.zone = time.UTC
	}
	return nil
}

func (j *Jira) issueEvent(issue jiraIssue, key string, at time.Time) Event {
	e := Event{
		Key:      key,
		Source:   "jira",
		Kind:     KindIssueNew,
		Ref:      issue.Key,
		Title:    issue.Fields.Summary,
		Body:     clip(jiraText(issue.Fields.Description), bodyMax),
		URL:      strings.TrimRight(j.URL, "/") + "/browse/" + issue.Key,
		Labels:   issue.Fields.Labels,
		State:    issue.Fields.Status.Name,
		Mine:     issue.Fields.Assignee != nil && isMe(j.Me, issue.Fields.Assignee.AccountID),
		Subtasks: j.openSubtasksOfMine(issue),
		At:       at,
	}
	if p := issue.Fields.Parent; p != nil {
		e.Parent, e.ParentTitle = p.Key, p.Fields.Summary
	}
	if issue.Fields.Assignee != nil {
		e.Assignee = issue.Fields.Assignee.DisplayName
	}
	if issue.Fields.Creator != nil {
		e.Author = issue.Fields.Creator.DisplayName
		e.Self = isMe(j.Me, issue.Fields.Creator.AccountID)
	}
	return e
}

const jiraIssueFields = "summary,description,labels,status,assignee,creator,created,updated,comment,issuetype,parent,subtasks"

// Mine is every open ticket assigned to me, as new-issue events: what a
// sweep hands to the daemon so work that was waiting before the watch
// started is not skipped.
func (j *Jira) Mine(ctx context.Context) ([]Event, error) {
	if err := j.resolveMe(ctx); err != nil {
		return nil, err
	}
	jql := "assignee = currentUser() AND statusCategory != Done ORDER BY created ASC"
	if j.Project != "" {
		jql = fmt.Sprintf("project = %q AND ", j.Project) + jql
	}
	var search struct {
		Issues []jiraIssue `json:"issues"`
	}
	params := url.Values{"jql": {jql}, "fields": {jiraIssueFields}, "maxResults": {"50"}}
	if err := j.get(ctx, "/rest/api/3/search/jql", params, &search); err != nil {
		return nil, err
	}
	var events []Event
	for _, issue := range search.Issues {
		events = append(events, j.issueEvent(issue, "jira:"+issue.Key, trackerTime(issue.Fields.Created)))
	}
	return events, nil
}

func (j *Jira) openSubtasksOfMine(issue jiraIssue) []string {
	var keys []string
	for _, s := range issue.Fields.Subtasks {
		if s.Fields.Assignee == nil || !isMe(j.Me, s.Fields.Assignee.AccountID) {
			continue
		}
		if finishedState(s.Fields.Status.Name) != "" {
			continue
		}
		keys = append(keys, s.Key)
	}
	return keys
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
	if err := j.resolveMe(ctx); err != nil {
		return nil, cursor, err
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
		"fields":     {jiraIssueFields},
		"expand":     {"changelog"},
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
		key, at := "jira:"+issue.Key, created
		if !created.After(issuesSince) {
			h, ok := j.becameMine(issue, issuesSince)
			if !ok || !assignedToMe {
				continue
			}
			key, at = key+":h"+h.ID, trackerTime(h.Created)
		}
		events = append(events, j.issueEvent(issue, key, at))
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
