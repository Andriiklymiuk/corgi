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

// bodyMax is how much of a description or comment an event carries.
const bodyMax = 200

// Linear polls issues and comments through the GraphQL API with a
// personal API key.
type Linear struct {
	Token, Team string
	Me          string // viewer id, resolved on the first poll and kept in the cursor
	Client      *http.Client
	URL         string
}

func NewLinear(s Secrets, team string) *Linear {
	return &Linear{Token: s.Linear, Team: team}
}

func (l *Linear) Name() string { return "linear" }

// Poll asks for issues updated and comments on my issues created since the
// cursor. New issues become KindIssueNew, other people's comments on issues
// assigned to me become KindIssueComment.
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
    nodes { id identifier title description url state { name } labels { nodes { name } } assignee { id name } createdAt updatedAt }
  }
  comments(filter: {createdAt: {gt: %s}, issue: {assignee: {id: {eq: %s}}}}, first: 50, orderBy: createdAt) {
    nodes { id body createdAt user { id name } issue { identifier url title state { name } } }
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
				CreatedAt, UpdatedAt                    string
			}
		}
		Comments linearComments
	}
	if err := l.query(ctx, query, &data); err != nil {
		return nil, cursor, err
	}
	// A busy interval can hold more than one page of comments; the oldest
	// would otherwise slip past the cursor.
	comments := data.Comments.Nodes
	for page := data.Comments; page.PageInfo.HasNextPage && len(comments) < 500; {
		var more struct{ Comments linearComments }
		q := fmt.Sprintf(`{ comments(filter: {createdAt: {gt: %s}, issue: {assignee: {id: {eq: %s}}}}, first: 50, orderBy: createdAt, after: %s) {
    nodes { id body createdAt user { id name } issue { identifier url title state { name } } }
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
		if !created.After(issuesSince) {
			continue
		}
		e := Event{
			Key:    "linear:" + n.Identifier,
			Source: "linear",
			Kind:   KindIssueNew,
			Ref:    n.Identifier,
			Title:  n.Title,
			Body:   clip(n.Description, bodyMax),
			URL:    n.URL,
			State:  n.State.Name,
			At:     created,
		}
		for _, label := range n.Labels.Nodes {
			e.Labels = append(e.Labels, label.Name)
		}
		if n.Assignee != nil {
			e.Assignee = n.Assignee.Name
			e.Mine = isMe(l.Me, n.Assignee.ID)
		}
		events = append(events, e)
	}
	for _, n := range comments {
		created := trackerTime(n.CreatedAt)
		newestComment = later(newestComment, created)
		if !created.After(commentsSince) || (n.User != nil && isMe(l.Me, n.User.ID)) {
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
			// The issue's column travels with its comments, so the rules can
			// tell a live discussion from chatter on finished work.
			State: n.Issue.State.Name,
			Mine:  true,
			At:    created,
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

// query posts one GraphQL document and decodes data into out; HTTP and
// GraphQL errors both come back as one descriptive error.
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

// cursorTime reads a saved bookmark; a missing or unreadable one means the
// last day.
func cursorTime(s string, now time.Time) time.Time {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	return now.Add(-24 * time.Hour)
}

// setCursorTime writes the newest time seen, or carries the old value when
// nothing moved so an empty cursor stays empty.
func setCursorTime(next, prev Cursor, key string, newest, since time.Time) {
	if newest.After(since) {
		next[key] = newest.UTC().Format(time.RFC3339Nano)
	} else if prev[key] != "" {
		next[key] = prev[key]
	}
}

// trackerTime reads the timestamps Linear and Jira send; unreadable is zero.
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
