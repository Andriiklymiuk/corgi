package cmd

import (
	"encoding/json"
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
	Ref        string           `json:"ref"`
	Title      string           `json:"title,omitempty"`
	URL        string           `json:"url,omitempty"`
	Source     string           `json:"source,omitempty"`
	Workspace  string           `json:"workspace,omitempty"`
	Column     string           `json:"column"`
	Why        string           `json:"why,omitempty"`
	Owner      string           `json:"owner,omitempty"`
	Priority   string           `json:"priority,omitempty"`
	Due        string           `json:"due,omitempty"`
	Overdue    bool             `json:"overdue,omitempty"`
	WaitsFor   []string         `json:"waitsFor,omitempty"`
	Touches    []string         `json:"touches,omitempty"`
	Links      []watch.TaskLink `json:"links,omitempty"`
	Idea       string           `json:"idea,omitempty"`
	Feature    string           `json:"feature,omitempty"`
	Running    bool             `json:"running,omitempty"`
	Unreviewed string           `json:"unreviewed,omitempty"`
	Triage     bool             `json:"triage,omitempty"`
	Cycle      string           `json:"cycle,omitempty"`
	Task       bool             `json:"task,omitempty"`
	Trail      []CardStep       `json:"trail,omitempty"`
	Comments   int              `json:"comments,omitempty"`
	UpdatedAt  time.Time        `json:"updatedAt"`
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
		Owner: c.Owner, Priority: c.Priority, Due: c.Due, Overdue: c.Overdue, WaitsFor: c.WaitsFor, Touches: c.Touches, UpdatedAt: c.UpdatedAt,
		Unreviewed: c.Unreviewed, Triage: c.Triage, Cycle: c.Cycle, Task: c.Kind == string(watch.KindTask), Trail: c.Trail}
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
	out := map[string]any{"columns": kanbanColumns, "cards": cards, "workspace": share.Workspace, "label": share.Label, "at": now.UTC(), "write": share.Write}
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
		path := strings.TrimSuffix(r.URL.Path, "/")
		if path == boardSharePath+"/act" {
			boardShareAct(w, r)
			return
		}
		if r.Method != http.MethodGet {
			writeLaunchError(w, http.StatusMethodNotAllowed, "the shared board is for reading only")
			return
		}
		switch path {
		case boardSharePath:
			setSharedPageHeaders(w, boardPagePolicy())
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

const boardActLimit = 16 << 10

var boardGuestWrites = &guestLimiter{seen: map[string][]time.Time{}}

type boardAct struct {
	Do    string `json:"do"`
	Ref   string `json:"ref"`
	Text  string `json:"text"`
	Title string `json:"title"`
	By    string `json:"by"`
}

func boardShareAct(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "POST an action")
		return
	}
	if !strings.HasPrefix(r.Header.Get(headerContentType), mimeJSON) {
		writeLaunchError(w, http.StatusUnsupportedMediaType, "send JSON")
		return
	}
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
	if !share.Write {
		writeLaunchError(w, http.StatusForbidden, "this link is for reading only")
		return
	}
	if !boardGuestWrites.allow(share.ID, guestAddress(r), now) {
		w.Header().Set("Retry-After", "60")
		writeLaunchError(w, http.StatusTooManyRequests, "too many changes at once - wait a minute")
		return
	}
	var act boardAct
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, boardActLimit)).Decode(&act); err != nil {
		writeLaunchError(w, http.StatusBadRequest, requestUnreadableMessage)
		return
	}
	done, err := guestBoardAct(dir, share, act, now)
	if err != nil {
		writeLaunchError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeLaunchJSON(w, map[string]any{"done": done})
}

const maxGuestComment = 2000

func guestTask(tasks *watch.TaskLog, share watch.BoardShare, ref string) (watch.Task, error) {
	t, ok := tasks.Find(ref)
	if !ok || (share.Workspace != "" && t.Workspace != share.Workspace) {
		return watch.Task{}, fmt.Errorf("no such task on this board")
	}
	return t, nil
}

func guestBoardAct(dir string, share watch.BoardShare, act boardAct, now time.Time) (string, error) {
	tasks, who := watch.LoadTasks(dir), watch.GuestName(act.By)
	switch act.Do {
	case "add":
		t, err := tasks.AddForTriage(act.Title, clipGuestText(act.Text), share.Workspace, who, now)
		if err != nil {
			return "", err
		}
		return t.Ref() + " waits for the owner's yes", nil
	case "comment":
		t, err := guestTask(tasks, share, act.Ref)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(act.Text) == "" || len(act.Text) > maxGuestComment {
			return "", fmt.Errorf("a comment has words, under %d characters", maxGuestComment)
		}
		if _, err := tasks.Comment(t.Ref(), who, act.Text, now); err != nil {
			return "", err
		}
		return "comment added to " + t.Ref(), nil
	case "claim":
		t, err := guestTask(tasks, share, act.Ref)
		if err != nil {
			return "", err
		}
		if _, err := tasks.Claim(t.Ref(), who, false, now); err != nil {
			return "", fmt.Errorf("%s is held by someone already", t.Ref())
		}
		return t.Ref() + " is yours", nil
	case "release":
		t, err := guestTask(tasks, share, act.Ref)
		if err != nil {
			return "", err
		}
		if !strings.EqualFold(t.Owner, who) {
			return "", fmt.Errorf("only the one who holds %s gives it back", t.Ref())
		}
		if _, err := tasks.Release(t.Ref(), now); err != nil {
			return "", err
		}
		return t.Ref() + " is free", nil
	}
	return "", fmt.Errorf("a guest can add, comment, claim or release")
}

func clipGuestText(text string) string {
	text = strings.TrimSpace(text)
	if len(text) > maxGuestComment {
		return text[:maxGuestComment]
	}
	return text
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

With --write a guest may also add a task (it waits in triage until you accept
it), comment on a task and claim one. What a guest writes never starts an
agent, and an agent reads it as a note, not as an instruction.

The key is in the part of the link after #, so it never reaches a server log.
corgi keeps only a hash of it: the link is shown once, here.

  corgi agent kanban share --workspace api --for 7d --label "for Anna"
  corgi agent kanban share --workspace api --write --label "the team"
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
		write, _ := cmd.Flags().GetBool("write")
		share, key, err := shares.Add(ws, label, lasts, write, now)
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
		fmt.Printf("id %s · %s · %s · %s · shown once\n", share.ID, firstNonEmptyString(share.Workspace, "every workspace"), boardShareExpiry(share, now), boardShareScope(share))
	},
}

func boardShareScope(s watch.BoardShare) string {
	if s.Write {
		return "may add, comment and claim"
	}
	return "read-only"
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
		fmt.Printf("%s  %-20s %s  %s  %s\n", s.ID, firstNonEmptyString(s.Workspace, "every workspace"), boardShareExpiry(s, now), boardShareScope(s), s.Label)
	}
}

func init() {
	agentKanbanShareCmd.Flags().String("workspace", "", "share only this workspace's cards (default: every workspace)")
	agentKanbanShareCmd.Flags().String("for", "7d", "how long the link works: 30m, 12h, 7d; 0 never expires")
	agentKanbanShareCmd.Flags().String("label", "", "who it is for, so you know which one to close")
	agentKanbanShareCmd.Flags().Bool("write", false, "let the guest add a task (it waits in triage), comment and claim a task; a guest never starts an agent")
	agentKanbanShareCmd.Flags().Bool("list", false, "the open links (not their keys)")
	agentKanbanShareCmd.Flags().String("off", "", "close a link by its id, or all")
	agentKanbanCmd.AddCommand(agentKanbanShareCmd)
}
