package watch

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Move is one ticket changing column.
type Move struct {
	Ref   string    `json:"ref"`
	Title string    `json:"title"`
	URL   string    `json:"url,omitempty"`
	From  string    `json:"from,omitempty"`
	To    string    `json:"to"`
	By    string    `json:"by,omitempty"`
	At    time.Time `json:"at"`
}

// Mover reads the column changes since a moment, with the tracker token
// corgi already holds - no MCP sign-in needed.
type Mover interface {
	Moves(ctx context.Context, since time.Time) ([]Move, error)
}

// movesLimit is how many updated tickets one call reads; a busier day is
// cut there and says so.
const movesLimit = 100

// ErrMovesCut says the tracker had more updated tickets than one read takes.
var ErrMovesCut = fmt.Errorf("more than %d tickets changed; the oldest are left out", movesLimit)

func (l *Linear) Moves(ctx context.Context, since time.Time) ([]Move, error) {
	if strings.TrimSpace(l.Token) == "" {
		return nil, ErrNoToken
	}
	filter := "updatedAt: {gt: " + graphqlString(since) + "}"
	if l.Team != "" {
		filter += ", team: {key: {eq: " + strconv.Quote(l.Team) + "}}"
	}
	query := fmt.Sprintf(`{
  issues(filter: {%s}, first: %d, sort: [{updatedAt: {order: Descending}}]) {
    nodes { identifier title url history(last: 20) { nodes { createdAt actor { name } fromState { name } toState { name } } } }
  }
}`, filter, movesLimit)
	var data struct {
		Issues struct {
			Nodes []struct {
				Identifier, Title, URL string
				History                struct {
					Nodes []struct {
						CreatedAt string
						Actor     *struct{ Name string }
						FromState *struct{ Name string }
						ToState   *struct{ Name string }
					}
				}
			}
		}
	}
	if err := l.query(ctx, query, &data); err != nil {
		return nil, err
	}
	var moves []Move
	for _, n := range data.Issues.Nodes {
		for _, h := range n.History.Nodes {
			at := trackerTime(h.CreatedAt)
			if h.ToState == nil || !at.After(since) {
				continue
			}
			m := Move{Ref: n.Identifier, Title: n.Title, URL: n.URL, To: h.ToState.Name, At: at}
			if h.FromState != nil {
				m.From = h.FromState.Name
			}
			if h.Actor != nil {
				m.By = h.Actor.Name
			}
			moves = append(moves, m)
		}
	}
	return newestFirst(moves), cutWhen(len(data.Issues.Nodes))
}

func (j *Jira) Moves(ctx context.Context, since time.Time) ([]Move, error) {
	if strings.TrimSpace(j.URL) == "" || strings.TrimSpace(j.Email) == "" || strings.TrimSpace(j.Token) == "" {
		return nil, ErrNoToken
	}
	if err := j.resolveMe(ctx); err != nil {
		return nil, err
	}
	jql := fmt.Sprintf("updated >= %q ORDER BY updated DESC", since.In(j.zone).Format("2006-01-02 15:04"))
	if j.Project != "" {
		jql = fmt.Sprintf("project = %q AND ", j.Project) + jql
	}
	var search struct {
		Issues []struct {
			Key    string `json:"key"`
			Fields struct {
				Summary string `json:"summary"`
			} `json:"fields"`
			Changelog struct {
				Histories []struct {
					Created string `json:"created"`
					Author  struct {
						DisplayName string `json:"displayName"`
					} `json:"author"`
					Items []struct {
						Field      string `json:"field"`
						FromString string `json:"fromString"`
						ToString   string `json:"toString"`
					} `json:"items"`
				} `json:"histories"`
			} `json:"changelog"`
		} `json:"issues"`
	}
	params := url.Values{
		"jql":        {jql},
		"fields":     {"summary"},
		"expand":     {"changelog"},
		"maxResults": {strconv.Itoa(movesLimit)},
	}
	if err := j.get(ctx, "/rest/api/3/search/jql", params, &search); err != nil {
		return nil, err
	}
	var moves []Move
	for _, issue := range search.Issues {
		for _, h := range issue.Changelog.Histories {
			at := trackerTime(h.Created)
			if !at.After(since) {
				continue
			}
			for _, it := range h.Items {
				if !strings.EqualFold(it.Field, "status") {
					continue
				}
				moves = append(moves, Move{Ref: issue.Key, Title: issue.Fields.Summary, URL: strings.TrimRight(j.URL, "/") + "/browse/" + issue.Key,
					From: it.FromString, To: it.ToString, By: h.Author.DisplayName, At: at})
			}
		}
	}
	return newestFirst(moves), cutWhen(len(search.Issues))
}

func newestFirst(moves []Move) []Move {
	sort.SliceStable(moves, func(a, b int) bool { return moves[a].At.After(moves[b].At) })
	return moves
}

func cutWhen(read int) error {
	if read >= movesLimit {
		return ErrMovesCut
	}
	return nil
}
