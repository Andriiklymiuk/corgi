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

const (
	ColInbox   = "Inbox"
	ColReady   = "Ready"
	ColRunning = "Running"
	ColBlocked = "Blocked"
	ColReview  = "Review"
	ColDone    = "Done"
)

var kanbanColumns = []string{ColInbox, ColReady, ColRunning, ColBlocked, ColReview, ColDone}

type KanbanCard struct {
	Ref       string            `json:"ref"`
	Key       string            `json:"key,omitempty"`
	Title     string            `json:"title,omitempty"`
	URL       string            `json:"url,omitempty"`
	Workspace string            `json:"workspace,omitempty"`
	Kind      string            `json:"kind,omitempty"`
	Column    string            `json:"column"`
	Why       string            `json:"why"`
	State     string            `json:"state,omitempty"`
	Branch    string            `json:"branch,omitempty"`
	Blocked   string            `json:"blocked,omitempty"`
	BlockedBy string            `json:"blockedBy,omitempty"`
	Session   *CardSess         `json:"session,omitempty"`
	Fix       *CardFix          `json:"fix,omitempty"`
	Handoff   *CardHand         `json:"handoff,omitempty"`
	Picked    *CardPick         `json:"picked,omitempty"`
	Columns   []string          `json:"columns,omitempty"`
	Body      string            `json:"body,omitempty"`
	Cost      *CardCost         `json:"cost,omitempty"`
	Pull      *watch.PullStatus `json:"pull,omitempty"`
	UpdatedAt time.Time         `json:"updatedAt"`
	Standing  sessions.Standing `json:"standing"`
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

type kanbanInputs struct {
	events        []watch.Event
	ignored       func(key string) bool
	moved         *watch.StateLog
	fixes         *watch.FixLog
	sessions      []sessions.Session
	packets       map[string][]handoff.Packet
	picks         *watch.PickLog
	pulls         *watch.PullLog
	now           time.Time
	sessionTokens func(s sessions.Session) (int64, bool)
}

type kanbanBoard struct {
	in    kanbanInputs
	byRef map[string]*KanbanCard
	order []string
}

const kanbanSessionWord = "session "

func buildKanban(in kanbanInputs) []KanbanCard {
	b := &kanbanBoard{in: in, byRef: map[string]*KanbanCard{}}
	b.placeEvents()
	b.placeRuns()
	b.placeSessions()
	b.placePicks()
	b.placeHandoffs()
	b.placeWallsAndCost()
	b.placePulls()
	return b.cards()
}

func (b *kanbanBoard) card(ws, ref string) *KanbanCard {
	id := ws + "/" + ref
	if c, ok := b.byRef[id]; ok {
		return c
	}
	c := &KanbanCard{Ref: ref, Workspace: ws, Column: ColInbox}
	b.byRef[id] = c
	b.order = append(b.order, id)
	return c
}

func (b *kanbanBoard) drop(ws, ref string) {
	delete(b.byRef, ws+"/"+ref)
}

func cardOpen(c *KanbanCard) bool {
	return c.Column == ColInbox || c.Column == ColReady || c.Column == ColRunning
}

func (b *kanbanBoard) placeEvents() {
	for _, e := range b.in.events {
		if e.Ref == "" || e.Kind == watch.KindRoutine || (b.in.ignored != nil && b.in.ignored(e.Key)) {
			continue
		}
		current := e.State
		if now, ok := b.in.moved.Get(e.Key); ok {
			current = now.Status
		}
		c := b.card(e.Workspace, e.Ref)
		if c.Key == "" {
			c.Key, c.Title, c.URL, c.Kind, c.State, c.UpdatedAt = e.Key, firstLineOf(e.Title), e.URL, string(e.Kind), current, e.At
		}
		if e.Kind == watch.KindTask {
			b.placeTask(c, e, current)
		} else {
			b.placeTicket(c, e, current)
		}
	}
}

func (b *kanbanBoard) placeTask(c *KanbanCard, e watch.Event, current string) {
	c.Body, c.Columns = e.Body, watch.TaskColumns
	switch watch.TaskColumn(current) {
	case "Doing":
		c.Column, c.Why = ColRunning, "picked up"
	case "Review":
		c.Column, c.Why = ColReview, "in review"
	case "Done", "Canceled":
		if b.in.now.Sub(e.At) > 7*24*time.Hour {
			b.drop(e.Workspace, e.Ref)
			return
		}
		c.Column, c.Why = ColDone, strings.ToLower(watch.TaskColumn(current))
	default:
		c.Why = "waiting in the inbox"
	}
}

func (b *kanbanBoard) placeTicket(c *KanbanCard, e watch.Event, current string) {
	over := watch.Settled(e, current)
	switch {
	case over != "" && strings.HasPrefix(over, "picked up"):
		c.Column, c.Why = ColReady, over
	case over != "":
		if b.in.now.Sub(e.At) > 24*time.Hour {
			b.drop(e.Workspace, e.Ref)
			return
		}
		c.Column, c.Why = ColDone, over
	case c.Why == "":
		c.Why = "waiting in the inbox"
	}
}

func (b *kanbanBoard) placeRuns() {
	if b.in.fixes == nil {
		return
	}
	for _, r := range b.in.fixes.RecentFixes("", 100) {
		b.placeRun(r)
	}
	for _, e := range b.in.fixes.DeferredEvents() {
		if e.Ref == "" {
			continue
		}
		c := b.card(e.Workspace, e.Ref)
		if c.Column == ColInbox {
			c.Column, c.Why = ColReady, "deferred: waits for budget or a manual run"
		}
	}
}

func (b *kanbanBoard) placeRun(r watch.FixRecord) {
	if r.Ref == "" || strings.HasPrefix(r.Ref, "routine/") {
		return
	}
	if b.in.ignored != nil && b.in.ignored(r.Key) {
		if _, kept := b.byRef[r.Workspace+"/"+r.Ref]; !kept {
			return
		}
	}
	c := b.card(r.Workspace, r.Ref)
	if c.Fix != nil && !c.Fix.Running {
		return
	}
	c.Fix = &CardFix{Running: !r.Done(), StartedAt: r.StartedAt, Outcome: r.Outcome(), PRs: r.PRs, Branch: r.Branch}
	nameCardFromRun(c, r)
	switch {
	case !r.Done():
		c.Column, c.Why = ColRunning, "a run started "+roughAge(b.in.now.Sub(r.StartedAt))+" ago"
	case len(r.PRs) > 0 && c.Column != ColDone:
		c.Column, c.Why = ColReview, r.Outcome()
	}
}

func nameCardFromRun(c *KanbanCard, r watch.FixRecord) {
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
}

func (b *kanbanBoard) placeSessions() {
	for _, s := range b.in.sessions {
		if s.Status == sessions.StatusGone || s.Status == sessions.StatusStale {
			continue
		}
		refs := sessionTicketRefs(s)
		if len(refs) == 0 {
			continue
		}
		for _, c := range b.byRef {
			if containsFold(refs, c.Ref) {
				b.placeSession(c, s)
			}
		}
	}
}

func (b *kanbanBoard) placeSession(c *KanbanCard, s sessions.Session) {
	c.Session = &CardSess{ID: s.ID, Label: firstNonEmpty(s.Display, s.Label), Status: string(s.Status), PR: s.PR}
	if s.Branch != "" {
		c.Branch = s.Branch
	}
	b.addSessionCost(c, s)
	if cardOpen(c) {
		c.Column, c.Why = ColRunning, sessionWhy(c.Session, s)
	}
	if s.PR != "" && cardOpen(c) {
		c.Column, c.Why = ColReview, kanbanSessionWord+c.Session.Label+" opened a pull request"
	}
}

func (b *kanbanBoard) addSessionCost(c *KanbanCard, s sessions.Session) {
	if b.in.sessionTokens == nil {
		return
	}
	tok, ok := b.in.sessionTokens(s)
	if !ok {
		return
	}
	if c.Cost == nil {
		c.Cost = &CardCost{}
	}
	c.Cost.Tokens += tok
	c.Cost.Sessions++
}

func (b *kanbanBoard) placePicks() {
	if b.in.picks == nil {
		return
	}
	for _, c := range b.byRef {
		p, ok := b.in.picks.Get(c.Key)
		if !ok {
			continue
		}
		c.Picked = &CardPick{At: p.At, By: p.By}
		if c.Session != nil || c.Column == ColDone || c.Column == ColReview {
			continue
		}
		age := b.in.now.Sub(p.At)
		if age <= watch.PickFresh {
			c.Column, c.Why = ColRunning, pickedWhy(p.By, age, "waiting for a session to open")
		} else if c.Column == ColRunning && c.Kind == string(watch.KindTask) {
			c.Why = pickedWhy(p.By, age, "no session came — Work on it again")
		}
	}
}

func pickedWhy(by string, age time.Duration, then string) string {
	return "picked from the " + pickedFrom(by) + " " + roughAge(age) + " ago · " + then
}

func (b *kanbanBoard) placeHandoffs() {
	for ws, list := range b.in.packets {
		for _, p := range list {
			if b.in.now.Sub(p.WrittenAt) > handoff.MaxAge {
				continue
			}
			b.placeHandoff(ws, p)
		}
	}
}

func (b *kanbanBoard) placeHandoff(ws string, p handoff.Packet) {
	c := b.card(ws, p.Ref)
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

func (b *kanbanBoard) placeWallsAndCost() {
	if b.in.fixes == nil {
		return
	}
	for _, c := range b.byRef {
		if wall, ok := b.in.fixes.Blocked(c.Workspace, c.Ref); ok && c.Column != ColDone {
			c.Column, c.Why, c.Blocked, c.BlockedBy = ColBlocked, wall.Reason, wall.Reason, wall.By
		}
		addRunCost(c, b.in.fixes.CostFor(c.Workspace, c.Ref))
	}
}

func addRunCost(c *KanbanCard, cost watch.Cost) {
	if cost.Runs == 0 {
		return
	}
	if c.Cost == nil {
		c.Cost = &CardCost{}
	}
	c.Cost.USD += cost.USD
	c.Cost.Tokens += cost.Tokens
	c.Cost.Runs = cost.Runs
}

func (b *kanbanBoard) placePulls() {
	if b.in.pulls == nil {
		return
	}
	for _, c := range b.byRef {
		st, ok := b.in.pulls.Get(cardPullLink(c))
		if !ok {
			continue
		}
		p := st
		c.Pull = &p
		if c.Column == ColReview && st.Line() != "" {
			c.Why = st.Line()
		}
	}
}

func (b *kanbanBoard) cards() []KanbanCard {
	for _, c := range b.byRef {
		c.Standing = cardStanding(c)
	}
	out := make([]KanbanCard, 0, len(b.byRef))
	emitted := map[string]bool{}
	for _, id := range b.order {
		if c, ok := b.byRef[id]; ok && !emitted[id] {
			emitted[id] = true
			out = append(out, *c)
		}
	}
	sortKanban(out)
	return out
}

func sortKanban(out []KanbanCard) {
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
}

func cardStanding(c *KanbanCard) sessions.Standing {
	f := sessions.Facts{PR: cardPullLink(c), Blocked: c.Blocked}
	if c.Pull != nil {
		p := c.Pull.Facts()
		f.Pull = &p
	}
	if c.Session != nil {
		f.Status = sessions.Status(c.Session.Status)
	}
	if c.Handoff != nil && c.Column == ColReady {
		f.Handoff = strings.TrimPrefix(c.Why, "handoff: ")
	}
	st := sessions.StandingOf(f)
	if c.Column == ColDone && st.Word != sessions.StandMerged {
		return sessions.Standing{Word: sessions.StandDone}
	}
	if st.Word == sessions.StandNew {
		switch c.Column {
		case ColRunning:
			st.Word = sessions.StandWorking
		case ColReview:
			st.Word = sessions.StandReview
		case ColBlocked:
			st = sessions.Standing{Word: sessions.StandBlocked, Why: c.Why}
		}
	}
	return st
}

func rowStanding(pull *watch.PullStatus, pr, blocked string, sess *CardSess) sessions.Standing {
	f := sessions.Facts{PR: pr, Blocked: blocked}
	if pull != nil {
		p := pull.Facts()
		f.Pull = &p
	}
	if sess != nil {
		f.Status = sessions.Status(sess.Status)
	}
	return sessions.StandingOf(f)
}

func cardPullLink(c *KanbanCard) string {
	if c.Fix != nil && len(c.Fix.PRs) > 0 {
		return c.Fix.PRs[0]
	}
	if c.Session != nil && c.Session.PR != "" {
		return c.Session.PR
	}
	if strings.HasPrefix(c.Kind, "pr.") || c.Kind == string(watch.KindCIFailed) || c.Kind == string(watch.KindReviewRequested) {
		if ref := watch.PullRef(c.URL); ref != "" {
			return ref
		}
		return c.Ref
	}
	return ""
}

func gatherKanban(dir, onlyWorkspace string, now time.Time) []KanbanCard {
	state := watch.LoadState(dir)
	in := kanbanInputs{
		events:  watch.RecentEvents(dir, 60),
		ignored: state.IsIgnored,
		moved:   watch.LoadStateLog(dir),
		fixes:   state.Fixes,
		packets: map[string][]handoff.Packet{},
		picks:   watch.LoadPicks(dir),
		pulls:   watch.LoadPullLog(dir),
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

func sessionWhy(c *CardSess, s sessions.Session) string {
	switch s.Status {
	case sessions.StatusNeedsInput:
		return kanbanSessionWord + c.Label + " needs you"
	case sessions.StatusWorking:
		return kanbanSessionWord + c.Label + " is working on it"
	case sessions.StatusDone:
		return kanbanSessionWord + c.Label + " finished a turn — check it, then move the card"
	case sessions.StatusLimited:
		return kanbanSessionWord + c.Label + " is limited; continues later"
	}
	return kanbanSessionWord + c.Label + " is on it"
}

func pickedFrom(by string) string {
	switch by {
	case "phone":
		return "phone"
	case "cli":
		return "command line"
	case "editor":
		return "editor"
	case "bar":
		return "menu bar"
	case "page", "dashboard":
		return "page"
	}
	return "board"
}
