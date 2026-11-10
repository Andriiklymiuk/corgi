package cmd

import (
	"context"
	"fmt"
	"html"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils/agent/idea"
	"andriiklymiuk/corgi/utils/agent/watch"
)

// Sending a page out: to the tracker as an epic with its tickets, or to
// Confluence as a page. The credentials are the watch's.

const ideaSendTimeout = 2 * time.Minute

// ideaLinearURL points the Linear client elsewhere; tests set it.
var ideaLinearURL = ""

// ideaTracker is the tracker the workspace's watch polls, and its project
// or team; with no watch, whichever credentials exist.
func ideaTracker(dir, ws string) (tracker, project string, secrets watch.Secrets) {
	secrets = watch.LoadSecretsFor(dir, ws)
	if ws != "" {
		if name, proj, found := trackerFromWatch(dir, ws); found {
			return name, proj, secrets
		} else {
			project = proj
		}
	}
	switch {
	case secrets.Linear != "":
		return "linear", project, secrets
	case secrets.JiraToken != "" && secrets.JiraURL != "":
		return "jira", project, secrets
	}
	return "", project, secrets
}

func trackerFromWatch(dir, ws string) (tracker, project string, found bool) {
	specs, err := loadWatchSpecs(dir)
	if err != nil {
		return "", "", false
	}
	for _, s := range specs {
		if s.Workspace != ws {
			continue
		}
		for _, name := range sourceNames(s) {
			if name == "linear" || name == "jira" {
				return name, s.Project, true
			}
		}
		project = s.Project
	}
	return "", project, false
}

// ideaSendOptions says where this page can go, for the buttons.
func ideaSendOptions(dir string, m idea.Meta) map[string]any {
	tracker, project, secrets := ideaTracker(dir, m.Workspace)
	out := map[string]any{}
	if secrets.Linear != "" {
		team := ""
		if tracker == "linear" {
			team = project
		}
		out["linear"] = map[string]any{"team": team}
	}
	if secrets.JiraToken != "" && secrets.JiraURL != "" {
		key := ""
		if tracker == "jira" {
			key = project
		}
		out["jira"] = map[string]any{"project": key, "site": strings.TrimRight(secrets.JiraURL, "/")}
		out["confluence"] = map[string]any{"site": strings.TrimRight(secrets.JiraURL, "/")}
	}
	return out
}

// liveTickets is the page's split with the board's current state on each
// ticket that is a board task.
func liveTickets(dir string, m idea.Meta) []idea.Ticket {
	out := append([]idea.Ticket(nil), m.Tickets...)
	if len(out) == 0 {
		return out
	}
	byRef := map[string]watch.Task{}
	for _, t := range watch.LoadTasks(dir).ByIdea(m.Slug) {
		byRef[t.Ref()] = t
	}
	for i, t := range out {
		if bt, ok := byRef[firstNonEmpty(t.Task, t.Ref)]; ok {
			out[i].State = bt.State
		}
	}
	return out
}

// sendIdea creates the page's epic and tickets in the tracker, or the page
// in Confluence, and remembers where they went.
type ideaSend struct {
	ctx     context.Context
	dir     string
	slug    string
	to      string
	id      *idea.Idea
	name    string
	link    string
	secrets watch.Secrets
	now     time.Time
}

func sendIdea(dir, slug, to, project, space string) ([]idea.Link, error) {
	id, err := idea.Load(dir, slug)
	if err != nil {
		return nil, err
	}
	to = strings.ToLower(strings.TrimSpace(to))
	tracker, dflt, secrets := ideaTracker(dir, id.Meta.Workspace)
	if to == "" {
		to = tracker
	}
	if project == "" && to == tracker {
		project = dflt
	}
	ctx, cancel := context.WithTimeout(context.Background(), ideaSendTimeout)
	defer cancel()
	s := ideaSend{ctx: ctx, dir: dir, slug: slug, to: to, id: id, name: firstNonEmpty(id.Meta.Name, id.Meta.Title),
		link: ideaLink(dir, id.Meta, false), secrets: secrets, now: time.Now()}
	var links []idea.Link
	switch to {
	case "jira", "linear":
		links, err = s.toTracker(project)
	case "confluence":
		links, err = s.toConfluence(space)
	case "":
		return nil, fmt.Errorf("say where: --to jira, linear or confluence (no tracker credentials were found)")
	default:
		return nil, fmt.Errorf("--to is jira, linear or confluence")
	}
	if err != nil {
		return links, err
	}
	ideaNotify(dir, id.Meta, "sent to "+to, links[0].URL)
	nudgeDaemon(dir)
	return links, nil
}

func (s ideaSend) creator(project string) (watch.Creator, error) {
	creator := watch.CreatorFor(s.secrets, s.to, project)
	if l, ok := creator.(*watch.Linear); ok && ideaLinearURL != "" {
		l.URL = ideaLinearURL
	}
	if creator == nil {
		return nil, fmt.Errorf("no %s credentials - corgi agent watch auth %s, or set the env the watch reads", s.to, s.to)
	}
	if len(s.id.Meta.Tickets) == 0 {
		return nil, fmt.Errorf("the page has no split into tickets yet - corgi idea answer %s writes one", s.slug)
	}
	return creator, nil
}

// epic is the one an earlier send made when it broke part-way: the epic and
// the tickets it made are kept and reused, not made twice.
func (s ideaSend) epic(creator watch.Creator) (idea.Link, error) {
	for _, l := range s.id.Meta.Sent {
		if l.Kind == "epic" && strings.HasPrefix(l.Title, s.to+":") {
			return l, nil
		}
	}
	body := s.id.Meta.Summary
	if s.link != "" {
		body += "\n\nThe page: " + s.link
	}
	epic, err := creator.Create(s.ctx, watch.NewIssue{Title: s.name, Body: body, Epic: true})
	if err != nil {
		return idea.Link{}, err
	}
	made := idea.Link{Kind: "epic", ID: epic.ID, Ref: epic.Ref, URL: epic.URL, Title: s.to + ":" + s.name, At: s.now}
	_, err = idea.Update(s.dir, s.slug, func(id *idea.Idea) error { id.Meta.Sent = append(id.Meta.Sent, made); return nil })
	return made, err
}

func (s ideaSend) toTracker(project string) ([]idea.Link, error) {
	creator, err := s.creator(project)
	if err != nil {
		return nil, err
	}
	epic, err := s.epic(creator)
	if err != nil {
		return nil, err
	}
	parent := epic.Ref
	if s.to == "linear" {
		parent = epic.ID
	}
	refs := make([]idea.Ticket, len(s.id.Meta.Tickets))
	copy(refs, s.id.Meta.Tickets)
	links := []idea.Link{epic}
	for i, t := range refs {
		if t.URL != "" {
			// Already in the tracker from an earlier, interrupted send.
			links = append(links, idea.Link{Kind: "ticket", Ref: t.Ref, URL: t.URL, Title: t.Title, At: s.now})
			continue
		}
		made, err := creator.Create(s.ctx, watch.NewIssue{Title: t.Title, Body: s.ticketBody(refs, i), Parent: parent})
		if err != nil {
			_, _ = idea.Update(s.dir, s.slug, func(id *idea.Idea) error { id.Meta.Tickets = refs; return nil })
			return links, fmt.Errorf("after %d ticket(s) (kept; run send again to finish): %v", i, err)
		}
		refs[i].Ref, refs[i].URL = made.Ref, made.URL
		refs[i].State = firstNonEmpty(refs[i].State, "Todo")
		links = append(links, idea.Link{Kind: "ticket", Ref: made.Ref, URL: made.URL, Title: t.Title, At: s.now})
	}
	_, err = idea.Update(s.dir, s.slug, func(id *idea.Idea) error {
		id.Meta.Tickets = refs
		note := fmt.Sprintf("Sent to %s as %s (%s): %d ticket(s) under it.", s.to, epic.Ref, epic.URL, len(refs))
		if c, err := id.AddComment(idea.KindComment, idea.Agent, note, nil, nil, []string{epic.URL}, s.now); err == nil {
			_, _ = id.SetStatus(c.ID, idea.StatusAnswered, s.now)
		}
		return nil
	})
	return links, err
}

func (s ideaSend) ticketBody(refs []idea.Ticket, i int) string {
	t := refs[i]
	tb := t.Body
	if s.link != "" {
		tb += "\n\nFrom the page: " + s.link
	}
	if len(t.After) == 0 {
		return tb
	}
	var after []string
	for _, n := range t.After {
		if n < 1 || n > len(refs) {
			continue
		}
		if n-1 < i && refs[n-1].URL != "" {
			after = append(after, refs[n-1].Ref)
		} else {
			after = append(after, fmt.Sprintf("#%d %s", n, refs[n-1].Title))
		}
	}
	return tb + "\nAfter: " + strings.Join(after, ", ")
}

func (s ideaSend) confluenceHTML() string {
	html := renderIdeaHTML(s.id.Doc)
	if len(s.id.Meta.Tickets) > 0 {
		var b strings.Builder
		b.WriteString("<h2>Tickets</h2><ol>")
		for _, t := range s.id.Meta.Tickets {
			b.WriteString("<li>")
			if t.URL != "" {
				fmt.Fprintf(&b, `<a href="%s">%s</a> `, htmlEscape(t.URL), htmlEscape(t.Ref))
			}
			b.WriteString(htmlEscape(t.Title))
			b.WriteString("</li>")
		}
		b.WriteString("</ol>")
		html += b.String()
	}
	if s.link != "" {
		html += fmt.Sprintf(`<p><em>Kept by corgi: <a href="%s">%s</a></em></p>`, htmlEscape(s.link), htmlEscape(s.link))
	}
	return html
}

func (s ideaSend) toConfluence(space string) ([]idea.Link, error) {
	if s.secrets.JiraToken == "" || s.secrets.JiraURL == "" {
		return nil, fmt.Errorf("no Atlassian credentials - corgi agent watch auth jira (Confluence uses the same email and token)")
	}
	page, err := watch.NewJira(s.secrets, "").CreatePage(s.ctx, space, s.name, s.confluenceHTML())
	if err != nil {
		return nil, err
	}
	links := []idea.Link{{Kind: "page", Ref: page.Ref, URL: page.URL, Title: s.name, At: s.now}}
	_, err = idea.Update(s.dir, s.slug, func(id *idea.Idea) error {
		id.Meta.Sent = append(id.Meta.Sent, links[0])
		if c, err := id.AddComment(idea.KindComment, idea.Agent, "Sent to Confluence: "+page.URL, nil, nil, []string{page.URL}, s.now); err == nil {
			_, _ = id.SetStatus(c.ID, idea.StatusAnswered, s.now)
		}
		return nil
	})
	return links, err
}

func htmlEscape(s string) string { return html.EscapeString(s) }
