package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// KindErrorNew is a new error from an error tracker or a log alert. It lands in
// the inbox to be picked; a run starts by itself only with --auto-for errors.
const KindErrorNew Kind = "error.new"

const sentryDefaultURL = "https://sentry.io"

var sentryLevels = map[string]int{"debug": 0, "info": 1, "warning": 2, "error": 3, "fatal": 4}

type Sentry struct {
	Token    string
	URL      string
	Org      string
	Projects []string
	MinLevel string
	Client   *http.Client
}

func NewSentry(s Secrets, org string, projects []string, minLevel string) *Sentry {
	base := strings.TrimRight(s.SentryURL, "/")
	if base == "" {
		base = sentryDefaultURL
	}
	return &Sentry{Token: s.Sentry, URL: base, Org: org, Projects: projects, MinLevel: minLevel, Client: &http.Client{Timeout: 30 * time.Second}}
}

func (s *Sentry) Name() string { return "sentry" }

type sentryIssue struct {
	ID        string `json:"id"`
	ShortID   string `json:"shortId"`
	Title     string `json:"title"`
	Culprit   string `json:"culprit"`
	Level     string `json:"level"`
	Count     string `json:"count"`
	UserCount int    `json:"userCount"`
	Permalink string `json:"permalink"`
	FirstSeen string `json:"firstSeen"`
	Project   struct {
		Slug string `json:"slug"`
	} `json:"project"`
}

func (s *Sentry) Poll(ctx context.Context, cursor Cursor) ([]Event, Cursor, error) {
	now := time.Now()
	since := cursorTime(cursor["firstSeen"], now)
	issues, err := s.issues(ctx)
	if err != nil {
		return nil, cursor, err
	}
	next := Cursor{}
	newest := since
	var out []Event
	for _, issue := range issues {
		first := trackerTime(issue.FirstSeen)
		if !first.After(since) || !s.loudEnough(issue.Level) {
			continue
		}
		out = append(out, sentryEvent(issue, first))
		newest = later(newest, first)
	}
	setCursorTime(next, cursor, "firstSeen", newest, since)
	return out, next, nil
}

func (s *Sentry) loudEnough(level string) bool {
	want, ok := sentryLevels[strings.ToLower(strings.TrimSpace(s.MinLevel))]
	if !ok {
		want = sentryLevels["error"]
	}
	got, ok := sentryLevels[strings.ToLower(level)]
	return !ok || got >= want
}

func (s *Sentry) issues(ctx context.Context) ([]sentryIssue, error) {
	query := "is:unresolved firstSeen:-24h"
	if len(s.Projects) > 0 {
		query += " project:[" + strings.Join(s.Projects, ",") + "]"
	}
	endpoint := fmt.Sprintf("%s/api/0/organizations/%s/issues/?query=%s&sort=new&limit=25", s.URL, url.PathEscape(s.Org), url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.Token)
	res, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	switch {
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("sentry refused the token (%d): it needs event:read and org:read - corgi agent watch auth sentry --token <new>", res.StatusCode)
	case res.StatusCode == http.StatusNotFound:
		return nil, fmt.Errorf("sentry has no organization %q: corgi agent watch enable --sentry-org <slug>", s.Org)
	case res.StatusCode >= 300:
		return nil, fmt.Errorf("sentry answered %d", res.StatusCode)
	}
	var issues []sentryIssue
	if err := json.Unmarshal(body, &issues); err != nil {
		return nil, fmt.Errorf("sentry: %w", err)
	}
	return issues, nil
}

func sentryEvent(issue sentryIssue, at time.Time) Event {
	var facts []string
	if issue.Culprit != "" {
		facts = append(facts, issue.Culprit)
	}
	if issue.Count != "" {
		facts = append(facts, issue.Count+" events")
	}
	if issue.UserCount > 0 {
		facts = append(facts, strconv.Itoa(issue.UserCount)+" users")
	}
	e := Event{Key: "sentry:" + issue.ID, Source: "sentry", Kind: KindErrorNew, Ref: issue.ShortID, Title: issue.Title,
		Body: strings.Join(facts, " · "), URL: issue.Permalink, State: issue.Level, Mine: true, At: at}
	if issue.Project.Slug != "" {
		e.Labels = []string{issue.Project.Slug}
	}
	return e
}

// parseAlertHook reads an alert a log or monitoring tool posts. Coralogix lets
// you write the webhook's body, so it takes the common field names.
// The workspace comes from the URL: /hooks/coralogix?workspace=<id>.
func parseAlertHook(source string, r *http.Request, body []byte, now time.Time) ([]Event, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	field := func(names ...string) string {
		for _, n := range names {
			if v, ok := raw[n]; ok && v != nil {
				if s := strings.TrimSpace(fmt.Sprint(v)); s != "" {
					return s
				}
			}
		}
		return ""
	}
	title := field("alert_name", "alertName", "name", "title")
	if title == "" {
		return nil, fmt.Errorf("an alert needs a name or a title")
	}
	id := field("alert_id", "alertId", "uniqueIdentifier", "id")
	if id == "" {
		id = title
	}
	link := field("url", "alert_url", "alertUrl", "link", "coralogix_url")
	if !strings.HasPrefix(link, "https://") {
		link = ""
	}
	var facts []string
	for _, f := range []string{field("description", "message", "text"), field("application", "applicationName"), field("subsystem", "subsystemName")} {
		if f != "" {
			facts = append(facts, f)
		}
	}
	// Once a day per alert: one that keeps firing is one thing to look at.
	day := now.UTC().Format("2006-01-02")
	return []Event{{Key: source + ":" + id + ":" + day, Source: source, Kind: KindErrorNew, Workspace: strings.TrimSpace(r.URL.Query().Get("workspace")),
		Ref: source + "-" + clipAlertRef(id), Title: title, Body: strings.Join(facts, " · "), URL: link, State: strings.ToLower(field("severity", "priority")), Mine: true, At: now}}, nil
}

func clipAlertRef(id string) string {
	clean := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, id)
	if len(clean) > 12 {
		return clean[:12]
	}
	return clean
}
