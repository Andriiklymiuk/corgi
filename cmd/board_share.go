package cmd

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/watch"

	"github.com/spf13/cobra"
)

const (
	boardKeyHeader    = "X-Corgi-Board-Key"
	boardSharePath    = "/board"
	boardShareDefault = 7 * 24 * time.Hour
	boardPageCSP      = "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
)

var boardGuestMisses = &guestLimiter{seen: map[string][]time.Time{}}

type sharedCard struct {
	Ref       string           `json:"ref"`
	Title     string           `json:"title,omitempty"`
	URL       string           `json:"url,omitempty"`
	Source    string           `json:"source,omitempty"`
	Workspace string           `json:"workspace,omitempty"`
	Column    string           `json:"column"`
	Why       string           `json:"why,omitempty"`
	Owner     string           `json:"owner,omitempty"`
	Priority  string           `json:"priority,omitempty"`
	Due       string           `json:"due,omitempty"`
	Overdue   bool             `json:"overdue,omitempty"`
	WaitsFor  []string         `json:"waitsFor,omitempty"`
	Links     []watch.TaskLink `json:"links,omitempty"`
	Idea      string           `json:"idea,omitempty"`
	Feature   string           `json:"feature,omitempty"`
	Running   bool             `json:"running,omitempty"`
	UpdatedAt time.Time        `json:"updatedAt"`
}

func webLink(link string) string {
	clean, err := watch.CleanTaskLink(link)
	if err != nil {
		return ""
	}
	return clean
}

func sharedCardOf(c KanbanCard) sharedCard {
	out := sharedCard{Ref: c.Ref, Title: c.Title, URL: webLink(c.URL), Workspace: c.Workspace, Column: c.Column, Why: c.Why,
		Owner: c.Owner, Priority: c.Priority, Due: c.Due, Overdue: c.Overdue, WaitsFor: c.WaitsFor, UpdatedAt: c.UpdatedAt}
	if out.URL != "" {
		out.Source = watch.LinkSource(out.URL)
	}
	seen := map[string]bool{out.URL: true}
	add := func(link string) {
		if link = webLink(link); link != "" && !seen[link] {
			seen[link] = true
			out.Links = append(out.Links, watch.TaskLink{URL: link, Source: watch.LinkSource(link), Label: watch.LinkLabel(link)})
		}
	}
	for _, link := range c.Links {
		add(link.URL)
	}
	if c.Fix != nil {
		out.Running = c.Fix.Running
		for _, pr := range c.Fix.PRs {
			add(pr)
		}
	}
	if c.Pull != nil {
		add(c.Pull.Link)
	}
	if c.Session != nil {
		out.Running = out.Running || c.Session.Status == string(sessions.StatusWorking)
		add(c.Session.PR)
	}
	if c.Idea != nil {
		out.Idea = c.Idea.Name
	}
	if c.Feature != nil {
		out.Feature = strings.TrimSpace(c.Feature.Ref + " " + c.Feature.Title)
	}
	return out
}

func sharedBoard(dir string, share watch.BoardShare, now time.Time) map[string]any {
	cards := []sharedCard{}
	for _, c := range gatherKanban(dir, share.Workspace, now) {
		cards = append(cards, sharedCardOf(c))
	}
	sort.SliceStable(cards, func(i, j int) bool { return cards[i].UpdatedAt.After(cards[j].UpdatedAt) })
	out := map[string]any{"columns": kanbanColumns, "cards": cards, "workspace": share.Workspace, "label": share.Label, "at": now.UTC()}
	if !share.Expires.IsZero() {
		out["expires"] = share.Expires.UTC()
	}
	return out
}

func boardShareHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setLaunchHeaders(w)
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.Method != http.MethodGet {
			writeLaunchError(w, http.StatusMethodNotAllowed, "the shared board is for reading only")
			return
		}
		switch strings.TrimSuffix(r.URL.Path, "/") {
		case boardSharePath:
			w.Header().Set(headerContentType, htmlContentType)
			w.Header().Set(headerContentSecurityPolicy, boardPageCSP)
			_, _ = fmt.Fprint(w, boardPageHTML)
		case boardSharePath + "/doc":
			boardShareDoc(w, r)
		default:
			writeLaunchError(w, http.StatusNotFound, "no such part of the board")
		}
	}
}

func boardShareDoc(w http.ResponseWriter, r *http.Request) {
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now()
	share, ok := watch.LoadBoardShares(dir).Find(r.Header.Get(boardKeyHeader), now)
	if !ok {
		if !boardGuestMisses.allow("board", guestAddress(r), now) {
			w.Header().Set("Retry-After", "60")
			writeLaunchError(w, http.StatusTooManyRequests, "too many tries - wait a minute")
			return
		}
		writeLaunchError(w, http.StatusForbidden, "this board link is closed or has expired - ask for a fresh one")
		return
	}
	writeLaunchJSON(w, sharedBoard(dir, share, now))
}

func boardShareLink(key string) string {
	local, public := ideaLinkBases()
	base := firstNonEmptyString(public, local)
	if base == "" {
		return ""
	}
	return base + boardSharePath + "#k=" + key
}

var agentKanbanShareCmd = &cobra.Command{
	Use:   "share",
	Short: "A read-only link to the board for a teammate: it expires, and you can close it",
	Long: `Makes a link a teammate opens in a browser, with no app and no pairing.
They see the cards: title, column, owner, priority, due date and the links to
Jira, Linear, GitHub and GitLab. They never see a description, a comment, a
session, a cost or a file path, and they cannot change anything.

The key is in the part of the link after #, so it never reaches a server log.
corgi keeps only a hash of it: the link is shown once, here.

  corgi agent kanban share --workspace api --for 7d --label "for Anna"
  corgi agent kanban share --list
  corgi agent kanban share --off <id>      (or --off all)`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		dir := mustAgentDir()
		shares, now := watch.LoadBoardShares(dir), time.Now()
		if off, _ := cmd.Flags().GetString("off"); off != "" {
			closed, err := shares.Close(off)
			if err != nil {
				exitWithError("agent_kanban_share", err, 2)
			}
			if utils.JSONOutput {
				utils.PrintJSON(map[string]any{"closed": closed})
				return
			}
			fmt.Printf("closed %d board link(s)\n", closed)
			return
		}
		if list, _ := cmd.Flags().GetBool("list"); list {
			printBoardShares(shares.List(now))
			return
		}
		ws, _ := cmd.Flags().GetString("workspace")
		if ws = strings.TrimSpace(ws); ws != "" {
			if _, err := workspaceRoot(ws); err != nil {
				exitWithError("agent_kanban_share", fmt.Errorf("no such workspace: %s", ws), 2)
			}
		}
		lasts := boardShareDefault
		if cmd.Flags().Changed("for") {
			raw, _ := cmd.Flags().GetString("for")
			d, err := parseLinkFor(raw)
			if err != nil {
				exitWithError("agent_kanban_share", err, 2)
			}
			lasts = d
		}
		label, _ := cmd.Flags().GetString("label")
		share, key, err := shares.Add(ws, label, lasts, now)
		if err != nil {
			exitWithError("agent_kanban_share", err, 2)
		}
		link := boardShareLink(key)
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"id": share.ID, "link": link, "key": key, "workspace": share.Workspace, "expires": share.Expires})
			return
		}
		if link == "" {
			fmt.Printf("board link %s made, but the dashboard is not running: start corgi agent up, then open <its address>%s#k=%s\n", share.ID, boardSharePath, key)
			return
		}
		fmt.Println(link)
		fmt.Printf("id %s · %s · %s · read-only · shown once\n", share.ID, firstNonEmptyString(share.Workspace, "every workspace"), boardShareExpiry(share, now))
	},
}

func boardShareExpiry(s watch.BoardShare, now time.Time) string {
	if s.Expires.IsZero() {
		return "does not expire"
	}
	return "expires in " + s.Expires.Sub(now).Round(time.Minute).String()
}

func printBoardShares(list []watch.BoardShare) {
	if utils.JSONOutput {
		utils.PrintJSON(map[string]any{"shares": list})
		return
	}
	if len(list) == 0 {
		fmt.Println("no open board links - corgi agent kanban share")
		return
	}
	now := time.Now()
	for _, s := range list {
		fmt.Printf("%s  %-20s %s  %s\n", s.ID, firstNonEmptyString(s.Workspace, "every workspace"), boardShareExpiry(s, now), s.Label)
	}
}

func init() {
	agentKanbanShareCmd.Flags().String("workspace", "", "share only this workspace's cards (default: every workspace)")
	agentKanbanShareCmd.Flags().String("for", "7d", "how long the link works: 30m, 12h, 7d; 0 never expires")
	agentKanbanShareCmd.Flags().String("label", "", "who it is for, so you know which one to close")
	agentKanbanShareCmd.Flags().Bool("list", false, "the open links (not their keys)")
	agentKanbanShareCmd.Flags().String("off", "", "close a link by its id, or all")
	agentKanbanCmd.AddCommand(agentKanbanShareCmd)
}
