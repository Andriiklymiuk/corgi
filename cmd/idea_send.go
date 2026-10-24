package cmd

import (
	"context"
	"fmt"
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
		if specs, err := loadWatchSpecs(dir); err == nil {
			for _, s := range specs {
				if s.Workspace != ws {
					continue
				}
				for _, name := range sourceNames(s) {
					if name == "linear" || name == "jira" {
						return name, s.Project, secrets
					}
				}
				project = s.Project
			}
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
		if bt, ok := byRef[t.Ref]; ok {
			out[i].State = bt.State
		}
	}
	return out
}

// sendIdea creates the page's epic and tickets in the tracker, or the page
// in Confluence, and remembers where they went.
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
	name := firstNonEmpty(id.Meta.Name, id.Meta.Title)
	link := ideaLink(dir, id.Meta, false)
	ctx, cancel := context.WithTimeout(context.Background(), ideaSendTimeout)
	defer cancel()
	now := time.Now()
	var links []idea.Link
	switch to {
	case "jira", "linear":
		creator := watch.CreatorFor(secrets, to, project)
		if l, ok := creator.(*watch.Linear); ok && ideaLinearURL != "" {
			l.URL = ideaLinearURL
		}
		if creator == nil {
			return nil, fmt.Errorf("no %s credentials - corgi agent watch auth %s, or set the env the watch reads", to, to)
		}
		if len(id.Meta.Tickets) == 0 {
			return nil, fmt.Errorf("the page has no split into tickets yet - corgi idea answer %s writes one", slug)
		}
		body := id.Meta.Summary
		if link != "" {
			body += "\n\nThe page: " + link
		}
		epic, err := creator.Create(ctx, watch.NewIssue{Title: name, Body: body, Epic: true})
		if err != nil {
			return nil, err
		}
		links = append(links, idea.Link{Kind: "epic", Ref: epic.Ref, URL: epic.URL, Title: name, At: now})
		parent := epic.Ref
		if to == "linear" {
			parent = epic.ID
		}
		refs := make([]idea.Ticket, len(id.Meta.Tickets))
		copy(refs, id.Meta.Tickets)
		for i, t := range refs {
			tb := t.Body
			if link != "" {
				tb += "\n\nFrom the page: " + link
			}
			if len(t.After) > 0 {
				var after []string
				for _, n := range t.After {
					if n >= 1 && n <= len(refs) {
						after = append(after, firstNonEmpty(refs[n-1].Ref, fmt.Sprintf("#%d", n)))
					}
				}
				tb += "\nAfter: " + strings.Join(after, ", ")
			}
			made, err := creator.Create(ctx, watch.NewIssue{Title: t.Title, Body: tb, Parent: parent})
			if err != nil {
				return links, fmt.Errorf("after %d ticket(s): %v", i, err)
			}
			refs[i].Ref, refs[i].URL = made.Ref, made.URL
			if refs[i].State == "" {
				refs[i].State = "Todo"
			}
			links = append(links, idea.Link{Kind: "ticket", Ref: made.Ref, URL: made.URL, Title: t.Title, At: now})
		}
		_, err = idea.Update(dir, slug, func(id *idea.Idea) error {
			id.Meta.Tickets = refs
			id.Meta.Sent = append(id.Meta.Sent, links[0])
			note := fmt.Sprintf("Sent to %s as %s (%s): %d ticket(s) under it.", to, epic.Ref, epic.URL, len(refs))
			if c, err := id.AddComment(idea.KindComment, idea.Agent, note, nil, nil, []string{epic.URL}, now); err == nil {
				_, _ = id.SetStatus(c.ID, idea.StatusAnswered, now)
			}
			return nil
		})
		if err != nil {
			return links, err
		}
	case "confluence":
		if secrets.JiraToken == "" || secrets.JiraURL == "" {
			return nil, fmt.Errorf("no Atlassian credentials - corgi agent watch auth jira (Confluence uses the same email and token)")
		}
		j := watch.NewJira(secrets, "")
		html := renderIdeaHTML(id.Doc)
		if len(id.Meta.Tickets) > 0 {
			var b strings.Builder
			b.WriteString("<h2>Tickets</h2><ol>")
			for _, t := range id.Meta.Tickets {
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
		if link != "" {
			html += fmt.Sprintf(`<p><em>Kept by corgi: <a href="%s">%s</a></em></p>`, htmlEscape(link), htmlEscape(link))
		}
		page, err := j.CreatePage(ctx, space, name, html)
		if err != nil {
			return nil, err
		}
		links = append(links, idea.Link{Kind: "page", Ref: page.Ref, URL: page.URL, Title: name, At: now})
		_, err = idea.Update(dir, slug, func(id *idea.Idea) error {
			id.Meta.Sent = append(id.Meta.Sent, links[0])
			if c, err := id.AddComment(idea.KindComment, idea.Agent, "Sent to Confluence: "+page.URL, nil, nil, []string{page.URL}, now); err == nil {
				_, _ = id.SetStatus(c.ID, idea.StatusAnswered, now)
			}
			return nil
		})
		if err != nil {
			return links, err
		}
	case "":
		return nil, fmt.Errorf("say where: --to jira, linear or confluence (no tracker credentials were found)")
	default:
		return nil, fmt.Errorf("--to is jira, linear or confluence")
	}
	ideaNotify(dir, id.Meta, "sent to "+to, links[0].URL)
	nudgeDaemon(dir)
	return links, nil
}

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}
