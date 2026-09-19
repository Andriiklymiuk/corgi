package watch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const linearURL = "https://api.linear.app/graphql"

const bodyMax = 200

type Linear struct {
	Token, Team string
	Me          string
	Client      *http.Client
	URL         string
}

func NewLinear(s Secrets, team string) *Linear {
	return &Linear{Token: s.Linear, Team: team}
}

func (l *Linear) Name() string { return "linear" }

func (l *Linear) Poll(ctx context.Context, cursor Cursor) ([]Event, Cursor, error) {
	if strings.TrimSpace(l.Token) == "" {
		return nil, cursor, ErrNoToken
	}
	if l.Me == "" {
		l.Me = cursor["me"]
	}
	if l.Me == "" {
		var viewer struct {
			Viewer struct{ ID, Name, Email string } `json:"viewer"`
		}
		if err := l.query(ctx, `{ viewer { id name email } }`, &viewer); err != nil {
			return nil, cursor, err
		}
		if viewer.Viewer.ID == "" {
			return nil, cursor, errors.New("linear: viewer has no id")
		}
		l.Me = viewer.Viewer.ID
	}

	now := time.Now()
	issuesSince := cursorTime(cursor["issues"], now)
	commentsSince := cursorTime(cursor["comments"], now)

	issueFilter := "updatedAt: {gt: " + graphqlString(issuesSince) + "}"
	if l.Team != "" {
		issueFilter += ", team: {key: {eq: " + strconv.Quote(l.Team) + "}}"
	}
	query := fmt.Sprintf(`{
  issues(filter: {%s}, first: 50, sort: [{updatedAt: {order: Ascending}}]) {
    nodes { id identifier title description url state { name } labels { nodes { name } } assignee { id name } creator { id name } createdAt updatedAt
      parent { identifier title }
      children { nodes { identifier state { name } assignee { id } } }
      history(last: 10) { nodes { id createdAt actor { id } toAssignee { id } toState { name } } } }
  }
  comments(filter: {createdAt: {gt: %s}, issue: {assignee: {id: {eq: %s}}}}, first: 50, orderBy: createdAt) {
    nodes { id body createdAt user { id name } botActor { name } issue { identifier url title state { name } } }
    pageInfo { hasNextPage endCursor }
  }
}`, issueFilter, graphqlString(commentsSince), strconv.Quote(l.Me))

	var data struct {
		Issues struct {
			Nodes []struct {
				ID, Identifier, Title, Description, URL string
				State                                   struct{ Name string }
				Labels                                  struct{ Nodes []struct{ Name string } }
				Assignee                                *struct{ ID, Name string }
				Creator                                 *struct{ ID, Name string }
				CreatedAt, UpdatedAt                    string
				Parent                                  *struct{ Identifier, Title string }
				Children                                struct {
					Nodes []struct {
						Identifier string
						State      struct{ Name string }
						Assignee   *struct{ ID string }
					}
				}
				History struct{ Nodes []linearHistory }
			}
		}
		Comments linearComments
	}
	if err := l.query(ctx, query, &data); err != nil {
		return nil, cursor, err
	}
	comments := data.Comments.Nodes
	for page := data.Comments; page.PageInfo.HasNextPage && len(comments) < 500; {
		var more struct{ Comments linearComments }
		q := fmt.Sprintf(`{ comments(filter: {createdAt: {gt: %s}, issue: {assignee: {id: {eq: %s}}}}, first: 50, orderBy: createdAt, after: %s) {
    nodes { id body createdAt user { id name } botActor { name } issue { identifier url title state { name } } }
    pageInfo { hasNextPage endCursor }
  } }`, graphqlString(commentsSince), strconv.Quote(l.Me), strconv.Quote(page.PageInfo.EndCursor))
		if err := l.query(ctx, q, &more); err != nil {
			return nil, cursor, err
		}
		comments = append(comments, more.Comments.Nodes...)
		page = more.Comments
	}

	var events []Event
	newestIssue, newestComment := issuesSince, commentsSince
	for _, n := range data.Issues.Nodes {
		created, updated := trackerTime(n.CreatedAt), trackerTime(n.UpdatedAt)
		newestIssue = later(newestIssue, updated)
		mine := n.Assignee != nil && isMe(l.Me, n.Assignee.ID)
		key, at := "linear:"+n.Identifier, created
		if !created.After(issuesSince) {
			h, ok := l.becameMine(n.History.Nodes, issuesSince)
			if !ok || !mine {
				continue
			}
			key, at = key+":"+h.ID, trackerTime(h.CreatedAt)
		}
		e := Event{
			Key:    key,
			Source: "linear",
			Kind:   KindIssueNew,
			Ref:    n.Identifier,
			Title:  n.Title,
			Body:   clip(n.Description, bodyMax),
			URL:    n.URL,
			State:  n.State.Name,
			At:     at,
		}
		if n.Parent != nil {
			e.Parent, e.ParentTitle = n.Parent.Identifier, n.Parent.Title
		}
		for _, c := range n.Children.Nodes {
			if c.Assignee != nil && isMe(l.Me, c.Assignee.ID) && finishedState(c.State.Name) == "" {
				e.Subtasks = append(e.Subtasks, c.Identifier)
			}
		}
		for _, label := range n.Labels.Nodes {
			e.Labels = append(e.Labels, label.Name)
		}
		if n.Assignee != nil {
			e.Assignee = n.Assignee.Name
			e.Mine = isMe(l.Me, n.Assignee.ID)
		}
		if n.Creator != nil {
			e.Author = n.Creator.Name
			e.Self = isMe(l.Me, n.Creator.ID)
		}
		events = append(events, e)
	}
	for _, n := range comments {
		created := trackerTime(n.CreatedAt)
		newestComment = later(newestComment, created)
		if !created.After(commentsSince) || (n.User != nil && isMe(l.Me, n.User.ID)) {
			continue
		}
		if n.User == nil || n.BotActor != nil {
			continue
		}
		e := Event{
			Key:    "linear:" + n.Issue.Identifier + ":c" + n.ID,
			Source: "linear",
			Kind:   KindIssueComment,
			Ref:    n.Issue.Identifier,
			Title:  n.Issue.Title,
			Body:   clip(n.Body, bodyMax),
			URL:    n.Issue.URL,
			State:  n.Issue.State.Name,
			Mine:   true,
			At:     created,
		}
		if n.User != nil {
			e.Author = n.User.Name
		}
		events = append(events, e)
	}

	next := Cursor{"me": l.Me}
	setCursorTime(next, cursor, "issues", newestIssue, issuesSince)
	setCursorTime(next, cursor, "comments", newestComment, commentsSince)
	return events, next, nil
}

type linearComments struct {
	Nodes []struct {
		ID, Body, CreatedAt string
		User                *struct{ ID, Name string }
		BotActor            *struct{ Name string }
		Issue               struct {
			Identifier, URL, Title string
			State                  struct{ Name string }
		}
	}
	PageInfo struct {
		HasNextPage bool
		EndCursor   string
	}
}

func (l *Linear) query(ctx context.Context, document string, out any) error {
	body, err := json.Marshal(map[string]string{"query": document})
	if err != nil {
		return err
	}
	endpoint := l.URL
	if endpoint == "" {
		endpoint = linearURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", l.Token)
	client := l.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("linear: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("linear: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("linear: %s: %s", resp.Status, clip(string(raw), bodyMax))
	}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("linear: bad response: %w", err)
	}
	if len(envelope.Errors) > 0 {
		messages := make([]string, 0, len(envelope.Errors))
		for _, e := range envelope.Errors {
			messages = append(messages, e.Message)
		}
		return fmt.Errorf("linear: %s", strings.Join(messages, "; "))
	}
	if out != nil && len(envelope.Data) > 0 {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return fmt.Errorf("linear: bad response: %w", err)
		}
	}
	return nil
}

func graphqlString(t time.Time) string {
	return strconv.Quote(t.UTC().Format(time.RFC3339Nano))
}

func cursorTime(s string, now time.Time) time.Time {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	return now.Add(-24 * time.Hour)
}

func setCursorTime(next, prev Cursor, key string, newest, since time.Time) {
	if newest.After(since) {
		next[key] = newest.UTC().Format(time.RFC3339Nano)
	} else if prev[key] != "" {
		next[key] = prev[key]
	}
}

func trackerTime(s string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.000-0700"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

type linearHistory struct {
	ID, CreatedAt string
	Actor         *struct{ ID string }
	ToAssignee    *struct{ ID string }
	ToState       *struct{ Name string }
}

// The newest change since `since` by another person that handed me the issue
// or moved it. An automation (no actor) moving it is not a hand-off.
func (l *Linear) becameMine(history []linearHistory, since time.Time) (linearHistory, bool) {
	var best linearHistory
	found := false
	for _, h := range history {
		at := trackerTime(h.CreatedAt)
		if !at.After(since) || h.Actor == nil || isMe(l.Me, h.Actor.ID) {
			continue
		}
		handed := h.ToAssignee != nil && isMe(l.Me, h.ToAssignee.ID)
		if (handed || h.ToState != nil) && (!found || at.After(trackerTime(best.CreatedAt))) {
			best, found = h, true
		}
	}
	return best, found
}
