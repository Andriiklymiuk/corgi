package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/handoff"
	"andriiklymiuk/corgi/utils/agent/sessions"
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
	UpdatedAt time.Time `json:"updatedAt"`
}

type CardSess struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"`
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
	now      time.Time
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
		if e.Ref == "" || (in.ignored != nil && in.ignored(e.Key)) {
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
			if r.Ref == "" {
				continue
			}
			c := card(r.Workspace, r.Ref)
			if c.Fix != nil && !c.Fix.Running {
				continue // the newest run on a ref is the one that counts
			}
			c.Fix = &CardFix{Running: !r.Done(), StartedAt: r.StartedAt, Outcome: r.Outcome(), PRs: r.PRs, Branch: r.Branch}
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

	// A live session on the ticket's branch is a person or an agent at work.
	for _, s := range in.sessions {
		ref := handoff.RefFromBranch(s.Branch)
		if ref == "" || s.Status == sessions.StatusGone || s.Status == sessions.StatusStale {
			continue
		}
		for id, c := range byRef {
			if !strings.EqualFold(c.Ref, ref) {
				continue
			}
			_ = id
			c.Session = &CardSess{ID: s.ID, Label: firstNonEmpty(s.Display, s.Label), Status: string(s.Status)}
			c.Branch = s.Branch
			if c.Column == ColInbox || c.Column == ColReady {
				c.Column, c.Why = ColRunning, "session "+c.Session.Label+" is on "+s.Branch
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

	// Blocked wins over everything but Done: a wall is a wall.
	if in.fixes != nil {
		for _, c := range byRef {
			if b, ok := in.fixes.Blocked(c.Workspace, c.Ref); ok && c.Column != ColDone {
				c.Column, c.Why, c.Blocked, c.BlockedBy = ColBlocked, b.Reason, b.Reason, b.By
			}
		}
	}

	out := make([]KanbanCard, 0, len(byRef))
	for _, id := range order {
		if c, ok := byRef[id]; ok {
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
		now:     now,
	}
	if rep, err := readBoard(dir); err == nil {
		in.sessions = rep.State.Sessions
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
				fmt.Println(line)
			}
		}
	},
}

func init() {
	agentKanbanCmd.Flags().String("workspace", "", "only this workspace's cards")
	agentCmd.AddCommand(agentKanbanCmd)
}
