package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/handoff"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/usage"
	"andriiklymiuk/corgi/utils/agent/watch"
	"andriiklymiuk/corgi/utils/agent/workspace"
	"github.com/spf13/cobra"
)

// The kanban is one card per ticket, in a column corgi works out from what
// it already knows: the inbox, the runs, the sessions, the handoffs, the
// ticket's own state. Nobody drags a card into Running; a run puts it
// there. Moving a card moves the ticket on the tracker, nothing else.

const (
	ColInbox   = "Inbox"
	ColReady   = "Ready"
	ColRunning = "Running"
	ColBlocked = "Blocked"
	ColReview  = "Review"
	ColDone    = "Done"
)

var kanbanColumns = []string{ColInbox, ColReady, ColRunning, ColBlocked, ColReview, ColDone}

// KanbanCard is one ticket as the board sees it.
type KanbanCard struct {
	Ref       string    `json:"ref"`
	Key       string    `json:"key,omitempty"`
	Title     string    `json:"title,omitempty"`
	URL       string    `json:"url,omitempty"`
	Workspace string    `json:"workspace,omitempty"`
	Kind      string    `json:"kind,omitempty"`
	Column    string    `json:"column"`
	Why       string    `json:"why"`
	State     string    `json:"state,omitempty"`
	Branch    string    `json:"branch,omitempty"`
	Blocked   string    `json:"blocked,omitempty"`
	BlockedBy string    `json:"blockedBy,omitempty"`
	Session   *CardSess `json:"session,omitempty"`
	Fix       *CardFix  `json:"fix,omitempty"`
	Handoff   *CardHand `json:"handoff,omitempty"`
	// Picked is "Work on it" pressed and by whom — kept on the card while
	// the session it asked for is still on its way, and after, as history.
	Picked *CardPick `json:"picked,omitempty"`
	// Columns is where this card can be moved: a task's own five; a tracker
	// ticket's come from the board cache, keyed by workspace.
	Columns []string `json:"columns,omitempty"`
	// Body is the description, for a task — a tracker ticket's lives on the
	// tracker.
	Body string `json:"body,omitempty"`
	// Cost is what the ticket has cost so far: the unattended runs (with
	// claude's own receipt) plus every session that sat on its branch.
	Cost      *CardCost `json:"cost,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type CardCost struct {
	USD      float64 `json:"usd,omitempty"`
	Tokens   int64   `json:"tokens"`
	Runs     int     `json:"runs"`
	Sessions int     `json:"sessions"`
}

type CardSess struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"`
	PR     string `json:"pr,omitempty"`
}

type CardPick struct {
	At time.Time `json:"at"`
	By string    `json:"by,omitempty"`
}

type CardFix struct {
	Running   bool      `json:"running"`
	StartedAt time.Time `json:"startedAt"`
	Outcome   string    `json:"outcome"`
	PRs       []string  `json:"prs,omitempty"`
	Branch    string    `json:"branch,omitempty"`
}

type CardHand struct {
	State string `json:"state"`
	Next  string `json:"next,omitempty"`
	Path  string `json:"path"`
	Draft bool   `json:"draft,omitempty"`
}

// kanbanInputs is everything the board is built from, gathered once so the
// derivation is a pure function that a test can drive.
type kanbanInputs struct {
	events   []watch.Event
	ignored  func(key string) bool
	moved    *watch.StateLog
	fixes    *watch.FixLog
	sessions []sessions.Session
	packets  map[string][]handoff.Packet // by workspace id
	picks    *watch.PickLog
	now      time.Time
	// sessionTokens is a seam: what one session has spent, from its transcript.
	sessionTokens func(s sessions.Session) (int64, bool)
}

func buildKanban(in kanbanInputs) []KanbanCard {
	byRef := map[string]*KanbanCard{}
	order := []string{}
	card := func(ws, ref string) *KanbanCard {
		id := ws + "/" + ref
		if c, ok := byRef[id]; ok {
			return c
		}
		c := &KanbanCard{Ref: ref, Workspace: ws, Column: ColInbox}
		byRef[id] = c
		order = append(order, id)
		return c
	}

	// Inbox events, newest first: the first one on a ref names the card.
	for _, e := range in.events {
		if e.Ref == "" || e.Kind == watch.KindRoutine || (in.ignored != nil && in.ignored(e.Key)) {
			continue
		}
		current := e.State
		if now, ok := in.moved.Get(e.Key); ok {
			current = now.Status
		}
		c := card(e.Workspace, e.Ref)
		if c.Key == "" {
			c.Key, c.Title, c.URL, c.Kind, c.State, c.UpdatedAt = e.Key, firstLineOf(e.Title), e.URL, string(e.Kind), current, e.At
		}
		// A task of your own sits where you put it: its column is its state.
		// A session or a run on it still moves it along below.
		if e.Kind == watch.KindTask {
			c.Body, c.Columns = e.Body, watch.TaskColumns
			switch watch.TaskColumn(current) {
			case "Doing":
				c.Column, c.Why = ColRunning, "picked up"
			case "Review":
				c.Column, c.Why = ColReview, "in review"
			case "Done", "Canceled":
				if in.now.Sub(e.At) > 7*24*time.Hour {
					delete(byRef, e.Workspace+"/"+e.Ref)
					continue
				}
				c.Column, c.Why = ColDone, strings.ToLower(watch.TaskColumn(current))
			default:
				c.Why = "waiting in the inbox"
			}
			continue
		}
		if over := watch.Settled(e, current); over != "" && strings.HasPrefix(over, "picked up") {
			c.Column, c.Why = ColReady, over
		} else if over != "" {
			if in.now.Sub(e.At) > 24*time.Hour {
				delete(byRef, e.Workspace+"/"+e.Ref)
				continue
			}
			c.Column, c.Why = ColDone, over
		} else if c.Why == "" {
			c.Why = "waiting in the inbox"
		}
	}

	// Runs: in flight is Running; a pull request is Review; a failure is
	// something a person reads, so it stays where the ticket is with the
	// outcome on the card.
	if in.fixes != nil {
		for _, r := range in.fixes.RecentFixes("", 100) {
			if r.Ref == "" || strings.HasPrefix(r.Ref, "routine/") {
				continue
			}
			// Ignored means out of the inbox everywhere: a run that
			// happened on an ignored event brings no card back on its own.
			if in.ignored != nil && in.ignored(r.Key) {
				if _, kept := byRef[r.Workspace+"/"+r.Ref]; !kept {
					continue
				}
			}
			c := card(r.Workspace, r.Ref)
			if c.Fix != nil && !c.Fix.Running {
				continue // the newest run on a ref is the one that counts
			}
			c.Fix = &CardFix{Running: !r.Done(), StartedAt: r.StartedAt, Outcome: r.Outcome(), PRs: r.PRs, Branch: r.Branch}
			// A card the run brought in still needs a key: it is what Move,
			// Ignore and the log are addressed to.
			if c.Key == "" {
				c.Key, c.Kind = r.Key, string(r.Kind)
			}
			if c.Title == "" {
				c.Title = firstLineOf(r.Title)
			}
			if c.URL == "" {
				c.URL = r.URL
			}
			if r.StartedAt.After(c.UpdatedAt) {
				c.UpdatedAt = r.StartedAt
			}
			switch {
			case !r.Done():
				c.Column, c.Why = ColRunning, "a run started "+roughAge(in.now.Sub(r.StartedAt))+" ago"
			case len(r.PRs) > 0 && c.Column != ColDone:
				c.Column, c.Why = ColReview, r.Outcome()
			}
		}
		for _, e := range in.fixes.DeferredEvents() {
			if e.Ref == "" {
				continue
			}
			c := card(e.Workspace, e.Ref)
			if c.Column == ColInbox {
				c.Column, c.Why = ColReady, "deferred: waits for budget or a manual run"
			}
		}
	}

	// A live session on the ticket is a person or an agent at work: one
	// opened for it by "Work on it" (the ticket rides in its environment),
	// or one on a branch named after it.
	for _, s := range in.sessions {
		if s.Status == sessions.StatusGone || s.Status == sessions.StatusStale {
			continue
		}
		refs := sessionTicketRefs(s)
		if len(refs) == 0 {
			continue
		}
		for _, c := range byRef {
			if !containsFold(refs, c.Ref) {
				continue
			}
			c.Session = &CardSess{ID: s.ID, Label: firstNonEmpty(s.Display, s.Label), Status: string(s.Status), PR: s.PR}
			if s.Branch != "" {
				c.Branch = s.Branch
			}
			if in.sessionTokens != nil {
				if tok, ok := in.sessionTokens(s); ok {
					if c.Cost == nil {
						c.Cost = &CardCost{}
					}
					c.Cost.Tokens += tok
					c.Cost.Sessions++
				}
			}
			if c.Column == ColInbox || c.Column == ColReady || c.Column == ColRunning {
				c.Column, c.Why = ColRunning, sessionWhy(c.Session, s)
			}
			// A pull request from the session is the ticket in review.
			if s.PR != "" && (c.Column == ColRunning || c.Column == ColInbox || c.Column == ColReady) {
				c.Column, c.Why = ColReview, "session "+c.Session.Label+" opened a pull request"
			}
		}
	}

	// "Work on it" pressed, no session yet: the card says so, and by whom,
	// instead of sitting in the inbox as if nothing happened. A pick with a
	// session on the card is just history.
	if in.picks != nil {
		for _, c := range byRef {
			p, ok := in.picks.Get(c.Key)
			if !ok {
				continue
			}
			c.Picked = &CardPick{At: p.At, By: p.By}
			if c.Session != nil || c.Column == ColDone || c.Column == ColReview {
				continue
			}
			if in.now.Sub(p.At) <= watch.PickFresh {
				c.Column, c.Why = ColRunning, "picked from the "+pickedFrom(p.By)+" "+roughAge(in.now.Sub(p.At))+" ago · waiting for a session to open"
			} else if c.Column == ColRunning && c.Kind == string(watch.KindTask) {
				c.Why = "picked from the " + pickedFrom(p.By) + " " + roughAge(in.now.Sub(p.At)) + " ago · no session came — Work on it again"
			}
		}
	}

	// A handoff means someone stopped part-way: the card is Ready unless a
	// run or a session has it, and it says what comes next.
	for ws, list := range in.packets {
		for _, p := range list {
			if in.now.Sub(p.WrittenAt) > handoff.MaxAge {
				continue
			}
			c := card(ws, p.Ref)
			c.Handoff = &CardHand{State: p.State, Next: p.Next, Path: handoff.MarkdownPath("", p.Ref), Draft: p.Draft}
			if c.Branch == "" {
				c.Branch = p.Where.Branch
			}
			if p.WrittenAt.After(c.UpdatedAt) {
				c.UpdatedAt = p.WrittenAt
			}
			if c.Column == ColInbox {
				c.Column, c.Why = ColReady, "handoff: "+p.Summary()
			}
			if p.State == handoff.StateCompleted && c.Column != ColReview {
				c.Column, c.Why = ColDone, "handoff says completed"
			}
		}
	}

	// Blocked wins over everything but Done: a wall is a wall. And every
	// card says what it has cost so far.
	if in.fixes != nil {
		for _, c := range byRef {
			if b, ok := in.fixes.Blocked(c.Workspace, c.Ref); ok && c.Column != ColDone {
				c.Column, c.Why, c.Blocked, c.BlockedBy = ColBlocked, b.Reason, b.Reason, b.By
			}
			if cost := in.fixes.CostFor(c.Workspace, c.Ref); cost.Runs > 0 {
				if c.Cost == nil {
					c.Cost = &CardCost{}
				}
				c.Cost.USD += cost.USD
				c.Cost.Tokens += cost.Tokens
				c.Cost.Runs = cost.Runs
			}
		}
	}

	out := make([]KanbanCard, 0, len(byRef))
	// A ref can enter order twice: an old settled event drops its card,
	// then a newer event on the same ref makes it again. One card per ref.
	emitted := map[string]bool{}
	for _, id := range order {
		if c, ok := byRef[id]; ok && !emitted[id] {
			emitted[id] = true
			out = append(out, *c)
		}
	}
	rank := map[string]int{}
	for i, col := range kanbanColumns {
		rank[col] = i
	}
	sort.SliceStable(out, func(i, j int) bool {
		if rank[out[i].Column] != rank[out[j].Column] {
			return rank[out[i].Column] < rank[out[j].Column]
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

// gatherKanban reads everything the board needs from the agent dir.
func gatherKanban(dir, onlyWorkspace string, now time.Time) []KanbanCard {
	state := watch.LoadState(dir)
	in := kanbanInputs{
		events:  watch.RecentEvents(dir, 60),
		ignored: state.IsIgnored,
		moved:   watch.LoadStateLog(dir),
		fixes:   state.Fixes,
		packets: map[string][]handoff.Packet{},
		picks:   watch.LoadPicks(dir),
		now:     now,
	}
	if rep, err := readBoard(dir); err == nil {
		in.sessions = rep.State.Sessions
	}
	in.sessionTokens = func(s sessions.Session) (int64, bool) {
		if s.Cwd == "" || sessions.Placeholder(s.ID) {
			return 0, false
		}
		t, ok := usage.ForSession(s.ConfigDir, s.Cwd, s.ID)
		return t.Total(), ok
	}
	if registry, err := workspace.Load(agentRegistryPath(dir)); err == nil {
		for _, ws := range registry.Workspaces {
			if onlyWorkspace != "" && ws.ID != onlyWorkspace {
				continue
			}
			if list := handoff.List(ws.AbsPath); len(list) > 0 {
				in.packets[ws.ID] = list
			}
		}
	}
	cards := buildKanban(in)
	if onlyWorkspace == "" {
		return cards
	}
	kept := cards[:0]
	for _, c := range cards {
		if c.Workspace == onlyWorkspace {
			kept = append(kept, c)
		}
	}
	return kept
}

var agentKanbanCmd = &cobra.Command{
	Use:   "kanban",
	Short: "One card per ticket: Inbox, Ready, Running, Blocked, Review, Done",
	Long: `The board corgi derives from what it knows — the inbox, the unattended runs,
the sessions on each branch, the handoffs, the tracker's own columns. A card
is Running because a run or a session is on it, Review because a pull request
is open, Blocked because the breaker tripped or a run said so, Ready because
a handoff or a deferred run is waiting. Nobody drags a card into a column;
moving a ticket moves it on the tracker (corgi agent watch move).

  corgi agent kanban
  corgi agent kanban --workspace api --json`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		dir := mustAgentDir()
		ws, _ := cmd.Flags().GetString("workspace")
		cards := gatherKanban(dir, ws, time.Now())
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"columns": kanbanColumns, "cards": cards})
			return
		}
		if len(cards) == 0 {
			fmt.Println("No cards: nothing in the inbox, no runs, no handoffs.")
			return
		}
		for _, col := range kanbanColumns {
			var rows []KanbanCard
			for _, c := range cards {
				if c.Column == col {
					rows = append(rows, c)
				}
			}
			if len(rows) == 0 {
				continue
			}
			fmt.Printf("%s (%d)\n", col, len(rows))
			for _, c := range rows {
				line := fmt.Sprintf("  %-14s %s", c.Ref, clipTitle(c.Why, 60))
				if c.Session != nil {
					line += " · " + c.Session.Label
				}
				if c.Fix != nil && len(c.Fix.PRs) > 0 {
					line += " · " + c.Fix.PRs[0]
				}
				if c.Cost != nil {
					line += " · " + costLine(*c.Cost)
				}
				fmt.Println(line)
			}
		}
	},
}

func init() {
	agentKanbanCmd.Flags().String("workspace", "", "only this workspace's cards")
	agentCmd.AddCommand(agentKanbanCmd)
}

// costLine is "1.2M tok · $0.84 · 3 runs" — the tokens always, the money
// when a run reported it.
func costLine(c CardCost) string {
	parts := []string{humanTokens(c.Tokens) + " tok"}
	if c.USD > 0 {
		parts = append(parts, fmt.Sprintf("$%.2f", c.USD))
	}
	if c.Runs > 0 {
		parts = append(parts, plural(c.Runs, "run", "runs"))
	}
	if c.Sessions > 0 {
		parts = append(parts, plural(c.Sessions, "session", "sessions"))
	}
	return strings.Join(parts, " · ")
}

func humanTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.0fk", float64(n)/1_000)
	}
	return fmt.Sprint(n)
}

// sessionTicketRefs is every ticket a session is on: the refs "Work on it"
// put in its environment, and the one its branch is named after.
func sessionTicketRefs(s sessions.Session) []string {
	var refs []string
	for _, r := range strings.Split(s.Ticket, ",") {
		if r = strings.TrimSpace(r); r != "" {
			refs = append(refs, r)
		}
	}
	if ref := handoff.RefFromBranch(s.Branch); ref != "" {
		refs = append(refs, ref)
	}
	return refs
}

func containsFold(list []string, want string) bool {
	for _, s := range list {
		if strings.EqualFold(s, want) {
			return true
		}
	}
	return false
}

// sessionWhy is the one line a card says while a session is on it.
func sessionWhy(c *CardSess, s sessions.Session) string {
	switch s.Status {
	case sessions.StatusNeedsInput:
		return "session " + c.Label + " needs you"
	case sessions.StatusWorking:
		return "session " + c.Label + " is working on it"
	case sessions.StatusDone:
		return "session " + c.Label + " finished a turn — check it, then move the card"
	case sessions.StatusLimited:
		return "session " + c.Label + " is limited; continues later"
	}
	return "session " + c.Label + " is on it"
}

// pickedFrom is the surface that pressed Work on it, in the words a card uses.
func pickedFrom(by string) string {
	switch by {
	case "phone":
		return "phone"
	case "cli":
		return "command line"
	case "editor":
		return "editor"
	case "page", "dashboard":
		return "page"
	}
	return "board"
}
