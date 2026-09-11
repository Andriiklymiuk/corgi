package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/events"
	"andriiklymiuk/corgi/utils/agent/handoff"
	"andriiklymiuk/corgi/utils/agent/usage"
	"andriiklymiuk/corgi/utils/agent/watch"
)

// WatchSpec is one workspace's watch, resolved by cmd from config and tokens.
type WatchSpec struct {
	Workspace string
	Dir       string
	ConfigDir string
	// Isolate runs each fix in its own worktrees; see Daemon.Isolate.
	Isolate bool
	// NoRetry leaves deferred fixes to a manual run.
	NoRetry bool
	// Models picks the model per event kind, and the one to step up to
	// after a failed run.
	Models *config.ModelPolicy
	// Routines run on a clock in this workspace; see routines.go.
	Routines []config.Routine
	Project  string   // tracker key prefix: ABC-123 belongs to ABC
	Repos    []string // owner/repo
	Rules    watch.Rules
	Sources  []watch.Source
	// Skipped names the sources the rules can never use, so status can say
	// why they are not polled.
	Skipped  []string
	Interval time.Duration
	// Action is notify or fix.
	Action string
	// Lease claims the ticket on the tracker before working it, so two
	// machines watching one board do not both take it.
	Lease bool
	// ReviewStatus is the column a ticket moves to once a run opened a pull
	// request for it.
	ReviewStatus string
	// FixKinds narrows what "fix" actually runs on: empty means every kind
	// the rules matched, which is what action: fix always meant. Naming
	// kinds lets a workspace work review comments unattended while still
	// only being told about a fresh ticket.
	FixKinds []string
	// SkipPermissions lets the fix run unattended; without it claude stops
	// at the first permission prompt and the run times out.
	SkipPermissions bool
	// MaxFixesPerHour and MaxFixesPerDay cap fix starts; 0 is the default.
	MaxFixesPerHour int
	MaxFixesPerDay  int
	// Quiet is a local "HH:MM-HH:MM" window in which no fix starts.
	Quiet string
}

// blockerWindow is how long a blocking failure is believed. Long enough to
// stop a run every three minutes, short enough that a credential fixed this
// morning is forgotten by the afternoon.
const blockerWindow = 2 * time.Hour

const (
	DefaultMaxFixesPerHour = 3
	DefaultMaxFixesPerDay  = 10
	// limitRefusePercent: a fix started this close to a limit would only
	// finish the account off for everything else.
	limitRefusePercent = 95
)

// FixCaps is the workspace's per-hour and per-day fix caps, defaults applied.
func (s WatchSpec) FixCaps() (perHour, perDay int) {
	perHour, perDay = s.MaxFixesPerHour, s.MaxFixesPerDay
	if perHour <= 0 {
		perHour = DefaultMaxFixesPerHour
	}
	if perDay <= 0 {
		perDay = DefaultMaxFixesPerDay
	}
	return perHour, perDay
}

// owns says an event is this workspace's by project key or repo.
func (s WatchSpec) owns(e watch.Event) bool {
	switch e.Kind {
	case watch.KindIssueNew, watch.KindIssueComment:
		return s.Project != "" && strings.HasPrefix(strings.ToUpper(e.Ref), strings.ToUpper(s.Project)+"-")
	default:
		repo := e.Ref
		if i := strings.IndexAny(repo, "#!"); i > 0 {
			repo = repo[:i]
		}
		for _, r := range s.Repos {
			if strings.EqualFold(r, repo) {
				return true
			}
		}
	}
	return false
}

// liveSources splits the sources into the ones worth polling and the
// names of the ones the rules can never match.
func (s WatchSpec) liveSources() (live []watch.Source, dead []string) {
	for _, src := range s.Sources {
		if s.Rules.DeadSource(src.Name()) {
			dead = append(dead, src.Name())
		} else {
			live = append(live, src)
		}
	}
	return live, dead
}

// QuietHours is a daily local-time window, possibly across midnight.
type QuietHours struct {
	start, end int // minutes since midnight
}

// ParseQuiet reads "HH:MM-HH:MM"; "" is no window.
func ParseQuiet(s string) (QuietHours, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return QuietHours{}, nil
	}
	from, to, ok := strings.Cut(s, "-")
	if !ok {
		return QuietHours{}, fmt.Errorf("quiet hours %q: want HH:MM-HH:MM", s)
	}
	start, err := parseClock(from)
	if err != nil {
		return QuietHours{}, fmt.Errorf("quiet hours %q: %w", s, err)
	}
	end, err := parseClock(to)
	if err != nil {
		return QuietHours{}, fmt.Errorf("quiet hours %q: %w", s, err)
	}
	if start == end {
		return QuietHours{}, fmt.Errorf("quiet hours %q: start and end are the same", s)
	}
	return QuietHours{start: start, end: end}, nil
}

func parseClock(s string) (int, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("%q is not HH:MM", s)
	}
	return t.Hour()*60 + t.Minute(), nil
}

// Contains says t's local time of day falls in the window.
func (q QuietHours) Contains(t time.Time) bool {
	if q.start == q.end {
		return false
	}
	m := t.Local().Hour()*60 + t.Local().Minute()
	if q.start < q.end {
		return m >= q.start && m < q.end
	}
	return m >= q.start || m < q.end
}

// fixTimeout bounds one unattended claude run.
const fixTimeout = 30 * time.Minute

// claudeCommand builds the headless run; a seam for tests.
var claudeCommand = func(ctx context.Context, dir string, env []string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	// A timeout must take the tools claude spawned with it, not just claude.
	killProcessGroup(cmd)
	cmd.WaitDelay = 10 * time.Second
	return cmd
}

func (d *Daemon) loadWatchFiles() {
	if d.watchState == nil {
		d.watchState = watch.LoadState(d.Dir)
		// A fix that never reported an outcome was interrupted, not finished.
		// Close it, and forget its event so the next poll offers it again —
		// the caps still decide whether anything actually runs.
		if keys := d.watchState.Fixes.Interrupted("interrupted — the daemon stopped mid-run", time.Now()); len(keys) > 0 {
			for _, key := range keys {
				d.watchState.Unsee(key)
			}
			utils.Infof("agent: watch: %d fix(es) were interrupted; their events will be offered again\n", len(keys))
		}
	}
}

func (d *Daemon) startWatches(ctx context.Context) {
	if len(d.Watches) == 0 {
		return
	}
	d.loadWatchFiles()
	d.watchers = map[string]*watch.Watch{}
	d.fixBusy = map[string]*sync.Mutex{}
	for _, spec := range d.Watches {
		spec := spec
		live, dead := spec.liveSources()
		if len(dead) > 0 {
			utils.Infof("agent: watch %s: not polling %s — the rules take nothing they emit\n", spec.Workspace, strings.Join(dead, ", "))
		}
		w := &watch.Watch{Workspace: spec.Workspace, Rules: spec.Rules, Sources: live, Interval: spec.Interval,
			State: d.watchState, Sink: d.watchSink(spec), Log: func(line string) { utils.Info("agent:", line) },
			Round: func(now time.Time) {
				d.watchState.NewRound()
				d.releaseHeld(spec, now)
				d.retryDeferred(ctx, spec, now)
				go d.refreshInboxStates(ctx, spec)
			}}
		d.watchers[spec.Workspace] = w
		d.fixBusy[spec.Workspace] = &sync.Mutex{}
		if len(live) > 0 && spec.Interval > 0 {
			go w.Run(ctx)
		}
	}
	utils.Infof("agent: watching %d workspace(s) — tracker and review events\n", len(d.Watches))
}

// refresh is the reload button, wherever it was pressed: the process table
// again, every tracker polled now, the inbox's columns re-read — then the
// board published, so every surface reads the same fresh picture.
func (d *Daemon) refresh() {
	d.rescan()
	for _, w := range d.watchers {
		w.Nudge()
	}
}

// handleWatchEvent routes a webhook's event to the workspace it belongs to,
// else to the first one whose rules take it; the seen list keeps it single.
func (d *Daemon) handleWatchEvent(ctx context.Context, e watch.Event) {
	if d.watchers == nil {
		return
	}
	// A routine handed in by `corgi agent routine run` names its workspace
	// and skips the rules: someone asked for it now.
	if e.Kind == watch.KindRoutine {
		for _, spec := range d.Watches {
			if spec.Workspace != e.Workspace || d.watchState == nil {
				continue
			}
			if reason := fixDeferral(spec, d.watchState.Fixes, time.Now()); reason != "" {
				utils.Infof("agent: routine %s waits: %s\n", e.Title, reason)
				return
			}
			if !d.claimFix(spec.Workspace, e.Ref) {
				return
			}
			d.watchState.Fixes.StartFor(e, time.Now())
			d.spawnFix(ctx, spec, e)
			return
		}
		return
	}
	for _, spec := range d.Watches {
		if spec.owns(e) {
			d.watchers[spec.Workspace].Handle(ctx, e)
			return
		}
	}
	for _, spec := range d.Watches {
		if d.watchers[spec.Workspace].Handle(ctx, e) {
			return
		}
	}
}

// WatchIdentity is who "me" is for a source, as its poll learned it.
func (d *Daemon) WatchIdentity(source string) string {
	if d.watchState == nil {
		return ""
	}
	for key, c := range d.watchState.Cursors {
		if strings.HasSuffix(key, "/"+source) && c["me"] != "" {
			return c["me"]
		}
	}
	return ""
}

func (d *Daemon) watchSink(spec WatchSpec) watch.Sink {
	return func(ctx context.Context, e watch.Event) {
		d.appendWatchEvent(e)
		if d.Events != nil {
			d.Events.Append(spec.Workspace, events.Event{At: time.Now().UTC(), Kind: "watch", Reason: string(e.Kind) + " " + e.Ref, URL: e.URL})
		}
		// Three comments on one pull request are one thing to look at. The
		// seen index dedupes a comment against itself; this dedupes the
		// second comment against the first, within a round.
		if dup := d.watchState.SameRefThisRound(spec.Workspace, e); dup > 0 {
			return
		}
		// Dismissed from the inbox means dismissed here too.
		if d.watchState.IsIgnored(e.Key) {
			return
		}
		body := watchBody(e)
		if spec.FixesKind(e.Kind) {
			if note := d.startFix(ctx, spec, e); note != "" {
				body += " (" + note + ")"
			}
		}
		// Quiet hours mean quiet: the event is recorded and the inbox shows
		// it, but nothing buzzes until the window opens.
		if quietNow(spec, time.Now()) {
			d.watchState.HoldEvent(spec.Workspace, e.Key, body, time.Now())
			return
		}
		go d.notifyAttentionAt("corgi agent · "+spec.Workspace, body, spec.Workspace, e.URL)
	}
}

func quietNow(spec WatchSpec, now time.Time) bool {
	q, err := ParseQuiet(spec.Quiet)
	return err == nil && q.Contains(now)
}

// releaseHeld delivers, once, what quiet hours swallowed. One notification
// for the lot: waking to nine separate buzzes is its own kind of noise.
func (d *Daemon) releaseHeld(spec WatchSpec, now time.Time) {
	if d.watchState == nil || quietNow(spec, now) {
		return
	}
	held := d.watchState.TakeHeld(spec.Workspace)
	// The night moved things on: a ticket finished, a row dismissed, is not
	// news in the morning.
	moved := watch.LoadStateLog(d.Dir)
	kept := held[:0]
	for _, n := range held {
		if n.Key != "" {
			if d.watchState.IsIgnored(n.Key) {
				continue
			}
			if e, ok := watch.FindEvent(d.Dir, n.Key); ok {
				current := e.State
				if st, ok := moved.Get(n.Key); ok {
					current = st.Status
				}
				if watch.Settled(e, current) != "" {
					continue
				}
			}
		}
		kept = append(kept, n)
	}
	if len(kept) == 0 {
		return
	}
	body := fmt.Sprintf("%d while you were away:", len(kept))
	for _, n := range kept {
		body += "\n  " + n.Body
	}
	go d.notifyAttention("corgi agent · "+spec.Workspace, body, spec.Workspace)
}

// retryDeferred starts the most urgent deferred fix for a workspace once
// whatever stopped it — a cap, quiet hours, the budget — has passed. One
// per round, so a backlog drains at the pace the caps allow rather than
// all at once the minute the window resets.
func (d *Daemon) retryDeferred(ctx context.Context, spec WatchSpec, now time.Time) {
	if spec.NoRetry || spec.Action != "fix" || d.watchState == nil {
		return
	}
	var queue []watch.Event
	for _, e := range d.watchState.Fixes.DeferredEvents() {
		if e.Workspace != spec.Workspace {
			continue
		}
		if _, blocked := d.watchState.Fixes.Blocked(spec.Workspace, e.Ref); blocked {
			continue
		}
		if d.watchState.IsIgnored(e.Key) {
			continue
		}
		queue = append(queue, e)
	}
	if len(queue) == 0 {
		return
	}
	if reason := fixDeferral(spec, d.watchState.Fixes, now); reason != "" {
		return
	}
	sort.SliceStable(queue, func(i, j int) bool { return watch.Less(queue[i], queue[j]) })
	// An event waited hours in the queue; the rules may have changed, and so
	// may the ticket. One that is over now is dropped, not started: the
	// morning is not the moment to move a finished ticket back to In Progress.
	for i, e := range queue {
		if why := d.stillWorthFixing(ctx, spec, e); why != "" {
			d.watchState.Fixes.DropDeferred(e.Key)
			utils.Infof("agent: watch %s: deferred fix for %s dropped: %s\n", spec.Workspace, e.Ref, why)
			continue
		}
		if !d.claimFix(spec.Workspace, e.Ref) {
			return
		}
		d.watchState.MarkSeen(e.Key)
		d.watchState.Fixes.StartFor(e, now)
		utils.Infof("agent: watch %s: deferred fix for %s starts now (%d more waiting)\n", spec.Workspace, e.Ref, len(queue)-i-1)
		d.spawnFix(ctx, spec, e)
		return
	}
}

// stillWorthFixing says why an event that once earned a fix no longer does,
// or "" while it still does. The rules first: what they refuse today they
// refuse for an event queued yesterday. Then the ticket's column, from what
// the daemon last saw and, for the tracker kinds, from the tracker itself.
func (d *Daemon) stillWorthFixing(ctx context.Context, spec WatchSpec, e watch.Event) string {
	if spec.Rules.Enabled {
		if why := spec.Rules.Why(e); why != "" {
			return why
		}
	}
	if e.Ref == "" {
		return ""
	}
	states := watch.LoadStateLog(d.Dir)
	if known, ok := states.Get(e.Key); ok {
		if over := watch.Settled(e, known.Status); over != "" {
			return "it is " + over
		}
	}
	switch e.Kind {
	case watch.KindIssueNew, watch.KindIssueComment, watch.KindPRComment, watch.KindPRReview, watch.KindReviewRequested:
	default:
		return ""
	}
	for _, src := range spec.Sources {
		asker, ok := src.(watch.StillOpen)
		if !ok {
			continue
		}
		state := asker.RefState(ctx, e.Ref)
		if state == "" {
			continue
		}
		_ = states.Set(e.Key, state, time.Now())
		if over := watch.Settled(e, state); over != "" {
			return "it is " + over
		}
		break
	}
	return ""
}

// startFix launches the fix, or says in one short note why not. A deferred
// event leaves the seen list and waits in the fix log until the daemon can
// start it (retryDeferred) or someone runs `corgi agent watch run`.
func (d *Daemon) startFix(ctx context.Context, spec WatchSpec, e watch.Event) string {
	now := time.Now()
	if why := d.stillWorthFixing(ctx, spec, e); why != "" {
		utils.Infof("agent: watch %s: not fixing %s: %s\n", spec.Workspace, e.Ref, why)
		return "not started: " + why
	}
	if reason := fixDeferral(spec, d.watchState.Fixes, now); reason != "" {
		d.watchState.Fixes.Defer(e)
		d.watchState.Unsee(e.Key)
		utils.Infof("agent: watch %s: fix for %s deferred: %s\n", spec.Workspace, e.Ref, reason)
		return "fix deferred: " + reason
	}
	if !d.claimFix(spec.Workspace, e.Ref) {
		return "a fix for it is already running"
	}
	d.watchState.Fixes.StartFor(e, now)
	d.spawnFix(ctx, spec, e)
	return ""
}

func (d *Daemon) spawnFix(ctx context.Context, spec WatchSpec, e watch.Event) {
	d.runs.Add(1)
	go func() {
		defer d.runs.Done()
		d.runFix(ctx, spec, e)
	}()
}

// fixDeferral says why a fix must not start now; "" means go ahead.
func fixDeferral(spec WatchSpec, log *watch.FixLog, now time.Time) string {
	perHour, perDay := spec.FixCaps()
	if log.StartedSince(spec.Workspace, now.Add(-time.Hour)) >= perHour {
		return fmt.Sprintf("%d/h cap", perHour)
	}
	if log.StartedSince(spec.Workspace, now.Add(-24*time.Hour)) >= perDay {
		return fmt.Sprintf("%d/day cap", perDay)
	}
	if q, err := ParseQuiet(spec.Quiet); err == nil && q.Contains(now) {
		return "quiet hours"
	}
	if pct, ok := limitUsed(spec.ConfigDir, now); ok {
		if pct >= limitRefusePercent {
			return fmt.Sprintf("limit %d%%", pct)
		}
		// What a run here usually costs, against what is left. Ten comment
		// fixes and ten whole tickets are the same number of runs and nowhere
		// near the same spend, so a count is the wrong unit to stop on. Only
		// once there is something measured to go on.
		if typical := log.TypicalSpend(spec.Workspace); typical > 0 && pct+typical > limitRefusePercent {
			return fmt.Sprintf("a run here costs about %d%% and %d%% is used", typical, pct)
		}
	}
	return ""
}

// FixBudget is a workspace's fix count against its caps.
type FixBudget struct {
	Hour     int       `json:"hour"`
	PerHour  int       `json:"perHour"`
	Day      int       `json:"day"`
	PerDay   int       `json:"perDay"`
	Today    int       `json:"today"`
	Last     time.Time `json:"last,omitempty"`
	Deferred int       `json:"deferred"`
}

// BudgetFor reads the workspace's fix history against its caps.
func BudgetFor(spec WatchSpec, log *watch.FixLog, now time.Time) FixBudget {
	b := FixBudget{Deferred: log.DeferredCount(spec.Workspace)}
	b.PerHour, b.PerDay = spec.FixCaps()
	b.Hour = log.StartedSince(spec.Workspace, now.Add(-time.Hour))
	b.Day = log.StartedSince(spec.Workspace, now.Add(-24*time.Hour))
	local := now.Local()
	b.Today = log.StartedSince(spec.Workspace, time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location()))
	b.Last, _ = log.LastStarted(spec.Workspace)
	return b
}

// String is "2/3 this hour · 5/10 today".
func (b FixBudget) String() string {
	return fmt.Sprintf("%d/%d this hour · %d/%d today", b.Hour, b.PerHour, b.Day, b.PerDay)
}

// limitUsed is the fuller of the account's two windows; one that has
// already reset since the snapshot no longer counts.
func limitUsed(configDir string, now time.Time) (int, bool) {
	l, ok := usage.ReadLimits(configDir)
	if !ok {
		return 0, false
	}
	pct := 0
	for _, w := range []usage.Window{l.FiveHour, l.SevenDay} {
		if !w.ResetsAt.IsZero() && w.ResetsAt.Before(now) {
			continue
		}
		pct = max(pct, w.Percent)
	}
	return pct, true
}

// claimFix keeps one fix per issue or PR in flight: a second event on the
// same ref while claude is still working is reported, not run again.
func (d *Daemon) claimFix(workspace, ref string) bool {
	d.attentionMu.Lock()
	defer d.attentionMu.Unlock()
	if d.fixActive == nil {
		d.fixActive = map[string]bool{}
	}
	key := workspace + "/" + ref
	if d.fixActive[key] {
		return false
	}
	d.fixActive[key] = true
	return true
}

func (d *Daemon) releaseFix(workspace, ref string) {
	d.attentionMu.Lock()
	delete(d.fixActive, workspace+"/"+ref)
	d.attentionMu.Unlock()
}

func (d *Daemon) fixActiveFor(workspace, ref string) bool {
	d.attentionMu.Lock()
	defer d.attentionMu.Unlock()
	return d.fixActive[workspace+"/"+ref]
}

func watchBody(e watch.Event) string {
	switch e.Kind {
	case watch.KindIssueNew:
		return fmt.Sprintf("new issue %s — %s", e.Ref, e.Title)
	case watch.KindIssueComment:
		return commentLine(e)
	case watch.KindPRReview:
		return fmt.Sprintf("%s reviewed %s: %s", firstNonEmpty(e.Author, "someone"), e.Ref, firstNonEmpty(e.Body, e.State))
	case watch.KindCIFailed:
		return fmt.Sprintf("red build in %s — %s", e.Ref, e.Title)
	case watch.KindReviewRequested:
		return fmt.Sprintf("%s wants your review on %s — %s", firstNonEmpty(e.Author, "someone"), e.Ref, e.Title)
	default:
		return commentLine(e)
	}
}

// commentLine says who said what on which ticket; with no text to show, it
// says what the ticket is rather than ending in a colon and nothing.
func commentLine(e watch.Event) string {
	who := firstNonEmpty(e.Author, "someone")
	if strings.TrimSpace(e.Body) == "" {
		return fmt.Sprintf("%s commented on %s — %s", who, e.Ref, e.Title)
	}
	return fmt.Sprintf("%s commented on %s: %s", who, e.Ref, e.Body)
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// fixPrompts is what the headless claude gets, per kind. The skills carry
// the rules: one spec gate collapsed by the approval, draft PRs only,
// never merge.
var fixPrompts = map[watch.Kind]func(e watch.Event) string{
	watch.KindRoutine: func(e watch.Event) string {
		return e.Body
	},
	// A task of your own: no tracker, so the session keeps the board honest
	// itself with the task commands.
	watch.KindTask: func(e watch.Event) string {
		body := strings.TrimSpace(e.Body)
		if body == "" {
			body = "(no description — use your judgement and ask if it is unclear)"
		}
		return fmt.Sprintf("You are picking up %s from the corgi board — a task written by the person you work with, not a tracker ticket.\n\n"+
			"Title: %s\n\n%s\n\n"+
			"Do the work in this checkout on a branch named after %s. I approve all changes; when code changed, push and open a draft pull request. "+
			"Keep the board honest as you go, from the shell:\n"+
			"- the moment a draft pull request is up: corgi agent task move %s Review\n"+
			"- when there is nothing left to do and no PR was needed: corgi agent task done %s\n"+
			"- if it cannot or should not be done, say why and run: corgi agent task move %s Canceled\n"+
			"End by saying in two lines what you did and what is left.",
			e.Ref, strings.TrimSpace(e.Title), body, e.Ref, e.Ref, e.Ref, e.Ref)
	},
	watch.KindIssueNew: func(e watch.Event) string {
		return "I approve all changes; ship it and open draft PRs, then watch CI to green. /corgi:stories " + e.Ref + storyMode(e)
	},
	watch.KindIssueComment: func(e watch.Event) string {
		return fmt.Sprintf("A new comment on %s from %s says: %q. Read it and decide. "+
			"If it asks a question or for information, answer it as a comment on %s through the tracker "+
			"(the Linear or Jira MCP tools, or the REST API with the saved token) and do NOT open a PR. "+
			"If it asks for a change, apply it on the existing branch for %s — find it by the ticket key in the branch names — "+
			"and when there is no such branch run /corgi:stories %s. I approve all changes; draft PRs only.",
			e.Ref, firstNonEmpty(e.Author, "someone"), e.Body, e.Ref, e.Ref, e.Ref)
	},
	watch.KindPRComment: reviewFeedbackPrompt,
	watch.KindPRReview:  reviewFeedbackPrompt,
	watch.KindReviewRequested: func(e watch.Event) string {
		// Someone else's branch. Read it, say what you think, change nothing.
		return "Review this pull request, which " + firstNonEmpty(e.Author, "a colleague") +
			" asked me to review: " + e.URL + ". It is THEIR branch — read the diff and post a review " +
			"(a summary and inline comments). Do not push commits to it, do not resolve their threads, " +
			"and do not approve it on my behalf. If it is good, say so and say why. /corgi:review " + e.URL
	},
	watch.KindCIFailed: func(e watch.Event) string {
		// The one kind that brings its own test for "done": make it green.
		return "A build went red in " + e.Ref + ": " + e.Title + ". " +
			"Find the failing run (gh run list --repo " + e.Ref + " --status failure --limit 5, then gh run view --log-failed), " +
			"read what actually failed, and fix the cause on the branch it failed on — not by weakening the test or skipping it. " +
			"Push, then watch the run to green. If it is a flake or an outage rather than our bug, say so and change nothing. " +
			"I approve all changes."
	},
}

func reviewFeedbackPrompt(e watch.Event) string {
	return "Address the review feedback on my own PR " + e.URL + " — do not start a fresh review of it: " +
		"apply the valid comments, push back on the wrong ones, reply and resolve the threads, push the fixes. /corgi:review " + e.URL
}

// FixPrompt is the prompt an event hands to a session, so the phone can start
// the same work the daemon would have started by itself.
func FixPrompt(e watch.Event) string { return fixPrompt(e) }

// BatchPrompt is one session for several new issues at once, which is what
// the stories skill is built for: it specs them together, reuses what they
// share and opens the PRs in one pass. Only issue.new batches — a review
// comment is about one thread and has nothing to share with the next.
func BatchPrompt(events []watch.Event) string {
	if len(events) == 0 {
		return ""
	}
	if len(events) == 1 {
		return fixPrompt(events[0])
	}
	var refs []string
	for _, e := range events {
		if e.Kind != watch.KindIssueNew || e.Ref == "" {
			return ""
		}
		refs = append(refs, e.Ref)
	}
	return "I approve all changes; ship them and open draft PRs, then watch CI to green. /corgi:stories " +
		strings.Join(refs, " ")
}

// fixPrompt is the prompt for an event's kind; "" for a kind with no fix.
func fixPrompt(e watch.Event) string {
	if build := fixPrompts[e.Kind]; build != nil {
		return build(e)
	}
	return ""
}

// unattendedSuffix is what an unattended run owes the person who finds its
// work later: a trail on the pull request, and a pass over its own diff
// before it claims anyone's attention. A run someone started by hand does
// not get this — they are already reading it.
func unattendedSuffix(spec WatchSpec, e watch.Event) string {
	trail := "corgi watch · " + spec.Workspace + " · " + string(e.Kind) + " " + e.Ref
	if e.URL != "" {
		trail += " · " + e.URL
	}
	return "\n\nBefore you finish: review your own diff the way you would review someone else's, " +
		"and fix what you find — nobody has looked at this but you. " +
		"Put this line at the end of the pull request body so whoever reviews it knows where it came from: " +
		trail + "\n" +
		"Say plainly at the end what you changed and what your own review found.\n" +
		"The pull request body carries a `## Evidence` section — changed files with a reason each; the commands you ran with their results; " +
		"each test mapped to the acceptance criterion it protects; known limitations and residual risk — facts, one line each.\n" +
		"If you stop with work remaining, blocked, or unsure, leave a handoff for the next run before you end: " +
		"`corgi agent handoff --ref " + e.Ref + " --done … --remaining … --decision … --uncertain … --next … --verify \"<the check you ran>\"` " +
		"(one flag per item; short sentences; no secrets)."
}

// fixArgs is claude's argv for one event.
func fixArgs(spec WatchSpec, e watch.Event) []string {
	return fixArgsWith(spec, e, "")
}

// fixArgsWith adds what an earlier run on the same ref left behind, so a
// second attempt continues rather than starting at the ticket again.
func fixArgsWith(spec WatchSpec, e watch.Event, handover string) []string {
	prompt := fixPrompt(e) + unattendedSuffix(spec, e)
	if p, ok := packetFor(spec.Dir, e.Ref); ok {
		prompt += "\n\nAn earlier run left a handoff for this ticket. Read it first; it is typed state, not a transcript. " +
			"Its verification was re-run at the current head: " + packetTrust(spec.Dir, p) + "\n" + p.Markdown()
	} else if handover = strings.TrimSpace(handover); handover != "" {
		prompt += "\n\nAn earlier run on this stopped part-way. This is the last thing it said — " +
			"treat it as notes, not as truth, and check anything it claims before building on it:\n" + handover
	}
	args := []string{"-p", prompt, "--output-format", "json"}
	if spec.SkipPermissions {
		return append(args, "--dangerously-skip-permissions")
	}
	return append(args, "--permission-mode", "acceptEdits")
}

var prLink = regexp.MustCompile(`https://(?:github\.com/[^\s)]+/pull/\d+|[^\s)]+/-/merge_requests/\d+)`)

// runFix runs one event's fix, one at a time per workspace, and reports
// how it ended. A key runs once: the seen list already holds it.
func (d *Daemon) runFix(ctx context.Context, spec WatchSpec, e watch.Event) {
	defer d.releaseFix(spec.Workspace, e.Ref)
	mu := d.fixBusy[spec.Workspace]
	mu.Lock()
	defer mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, fixTimeout)
	defer cancel()

	logDir := filepath.Join(d.Dir, "watch", "runs")
	_ = os.MkdirAll(logDir, 0o700)
	logPath := filepath.Join(logDir, safeName(e.Key)+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		utils.Infof("agent: watch fix %s: %v\n", e.Ref, err)
		return
	}
	defer logFile.Close()

	// A ticket that is blocked — by the breaker, by a run that said so, or
	// by a person — waits for a person. Nothing is deferred and no budget is
	// spent; the inbox shows why.
	if b, ok := d.watchState.Fixes.Blocked(spec.Workspace, e.Ref); ok && e.Ref != "" {
		d.watchState.Fixes.Finish(e.Key, nil, "", "not started: blocked ("+b.By+"): "+b.Reason, time.Now())
		utils.Infof("agent: watch: %s is blocked (%s): %s — corgi agent watch unblock %s\n", e.Ref, b.By, b.Reason, e.Ref)
		return
	}
	// A wall this workspace has hit twice running is not worth a third run:
	// a missing credential or a refused permission will still be missing in
	// an hour. Deferred, so a manual run picks it up once it is fixed.
	if kind, runs := d.watchState.Fixes.RecentBlocker(spec.Workspace, blockerWindow, time.Now()); runs >= 2 {
		d.watchState.Fixes.Defer(e)
		d.watchState.Fixes.Finish(e.Key, nil, "", "not started: the last "+strconv.Itoa(runs)+" runs here failed on "+kind, time.Now())
		go d.notifyAttentionAt("corgi agent · "+spec.Workspace,
			"not working on "+e.Ref+": the last runs failed on "+kind+" — fix that and run corgi agent watch run",
			spec.Workspace, e.URL)
		return
	}
	// Two machines watching one board would otherwise both take this ticket.
	// A tracker that cannot be read leaves the claim unknown: the run goes
	// ahead, because refusing to work when the tracker is down is worse than
	// the duplicate it guards against.
	if spec.Lease && d.ClaimTicket != nil && e.Ref != "" {
		switch ok, holder, err := d.ClaimTicket(spec.Workspace, e); {
		case err != nil:
			utils.Infof("agent: could not claim %s, going ahead: %v\n", e.Ref, err)
		case !ok:
			d.watchState.Fixes.Finish(e.Key, nil, "", "not started: "+holder+" is already on it", time.Now())
			utils.Infof("agent: %s is already claimed by %s\n", e.Ref, holder)
			return
		}
	}
	if d.Pickup != nil {
		d.Pickup(spec.Workspace, e)
	}
	var env []string
	if spec.ConfigDir != "" {
		env = append(env, "CLAUDE_CONFIG_DIR="+spec.ConfigDir)
	}
	fmt.Fprintf(logFile, "=== %s %s %s\n", time.Now().Format(time.RFC3339), e.Kind, e.Ref)
	// What the window was at before the run, so the receipt is the difference.
	before, hadBefore := usage.ReadLimits(spec.ConfigDir)
	handover := d.watchState.Fixes.LastHandover(spec.Workspace, e.Ref)
	started := time.Now()
	args := fixArgsWith(spec, e, handover)
	if model := fixModel(spec, e, d.watchState.Fixes.FailedInARow(spec.Workspace, e.Ref)); model != "" {
		args = append(args, "--model", model)
		fmt.Fprintf(logFile, "=== model: %s\n", model)
	}
	if spec.Isolate && d.Isolate != nil {
		branch := FixBranch(e.Ref)
		trees, err := d.Isolate(spec.Dir, branch)
		if err != nil {
			fmt.Fprintf(logFile, "\n=== could not isolate: %v\n", err)
			d.watchState.Fixes.Finish(e.Key, nil, "", "could not create worktrees: "+err.Error(), time.Now())
			go d.notifyAttentionAt("corgi agent · "+spec.Workspace,
				fmt.Sprintf("fix for %s did not start: could not create worktrees: %v", e.Ref, err), spec.Workspace, e.URL)
			return
		}
		d.watchState.Fixes.SetBranch(e.Key, branch)
		args[1] += isolationNote(branch, trees)
		fmt.Fprintf(logFile, "=== worktrees on %s: %s\n", branch, strings.Join(trees, ", "))
	}
	cmd := claudeCommand(ctx, spec.Dir, env, args...)
	cmd.Stdin = nil
	raw, runErr := cmd.Output()
	// json output carries what the run said plus what it cost; the log keeps
	// the words, the record keeps the numbers. Output that is not the JSON
	// envelope (an older claude, a crash mid-line) is used as it came.
	out, receipt := unwrapResult(raw)
	logFile.Write(out)
	if receipt.ok {
		fmt.Fprintf(logFile, "\n=== cost: $%.4f · %d tokens · %d turns\n", receipt.costUSD, receipt.tokens, receipt.turns)
		d.watchState.Fixes.SetCost(e.Key, receipt.costUSD, receipt.tokens)
	}
	if runErr != nil {
		fmt.Fprintf(logFile, "\n=== failed: %v\n", runErr)
		d.watchState.Fixes.Finish(e.Key, nil, "", runErr.Error(), time.Now())
		// What it managed to say before it stopped is worth more than the
		// error on its own: the next attempt starts from there.
		d.watchState.Fixes.SetHandover(e.Key, runHandover(spec.Dir, e.Ref, started, string(out)), time.Now())
		d.mirrorHandoff(spec, e.Ref, started)
		d.tripBreaker(spec, e, runErr.Error())
		d.routineReport(spec, e, string(out), runErr)
		go d.notifyAttentionAt("corgi agent · "+spec.Workspace,
			fmt.Sprintf("fix for %s failed: %v — log: %s", e.Ref, runErr, logPath), spec.Workspace, e.URL)
		return
	}
	links := uniqueStrings(prLink.FindAllString(string(out), -1))
	note := ""
	body := "fixed " + e.Ref
	if len(links) > 0 {
		body += " — " + strings.Join(links, " ")
	} else if last := lastLine(string(out)); last != "" {
		note = clipText(last, 160)
		body += " — " + note
	}
	d.watchState.Fixes.Finish(e.Key, links, note, "", time.Now())
	d.watchState.Fixes.SetHandover(e.Key, runHandover(spec.Dir, e.Ref, started, string(out)), time.Now())
	d.mirrorHandoff(spec, e.Ref, started)
	d.blockIfRunSaidSo(spec, e, started)
	d.routineReport(spec, e, string(out), nil)
	// It opened something, so the ticket is no longer being worked on — it is
	// waiting on a reviewer, and the board should say so without anyone
	// dragging it.
	if len(links) > 0 && d.Delivered != nil {
		d.Delivered(spec.Workspace, e, links)
	}
	if after, ok := usage.ReadLimits(spec.ConfigDir); ok && hadBefore {
		d.watchState.Fixes.SetSpent(e.Key, after.FiveHour.Percent-before.FiveHour.Percent)
	}
	// The pull request it opened is where to go, if it opened one.
	target := e.URL
	if len(links) > 0 {
		target = links[0]
	}
	go d.notifyAttentionAt("corgi agent · "+spec.Workspace, body, spec.Workspace, target)
}

// Probe is one synthesized event's walk through the pipeline, for
// `corgi agent watch test`: every gate a real event passes, and the claude
// run it would start, without starting it or recording anything.
type Probe struct {
	Workspace string    `json:"workspace"`
	Matched   bool      `json:"matched"`
	Why       string    `json:"why,omitempty"`
	Seen      bool      `json:"seen"`
	Action    string    `json:"action"`
	Busy      bool      `json:"busy,omitempty"`
	Deferred  string    `json:"deferred,omitempty"`
	Budget    FixBudget `json:"budget"`
	Prompt    string    `json:"prompt,omitempty"`
	Args      []string  `json:"args,omitempty"`
}

// ProbeEvent routes e the way handleWatchEvent does and reports each gate.
// ok is false when no watched workspace takes the event.
func (d *Daemon) ProbeEvent(e watch.Event, now time.Time) (Probe, bool) {
	d.loadWatchFiles()
	var spec *WatchSpec
	for i := range d.Watches {
		if d.Watches[i].owns(e) {
			spec = &d.Watches[i]
			break
		}
	}
	if spec == nil {
		for i := range d.Watches {
			if d.Watches[i].Rules.Match(e) {
				spec = &d.Watches[i]
				break
			}
		}
	}
	if spec == nil {
		return Probe{}, false
	}
	p := Probe{Workspace: spec.Workspace, Why: spec.Rules.Why(e), Seen: d.watchState.IsSeen(e.Key), Action: spec.Action}
	p.Matched = p.Why == ""
	// A kind --auto-for did not name is reported, not worked on. This read
	// p.Matched before it was set, so the dry run always echoed the
	// workspace's action and said "fix" for a kind the daemon would only
	// have told you about — wrong in exactly the tool people use to check.
	if p.Matched && spec.Action == "fix" && !spec.FixesKind(e.Kind) {
		p.Action = "notify"
	}
	if !p.Matched || p.Action != "fix" {
		return p, true
	}
	p.Deferred = fixDeferral(*spec, d.watchState.Fixes, now)
	p.Budget = BudgetFor(*spec, d.watchState.Fixes, now)
	p.Busy = d.fixActiveFor(spec.Workspace, e.Ref)
	p.Prompt = fixPrompt(e)
	p.Args = fixArgs(*spec, e)
	return p, true
}

func (d *Daemon) appendWatchEvent(e watch.Event) {
	path := filepath.Join(d.Dir, "watch", "events.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	if data, err := json.Marshal(e); err == nil {
		f.Write(append(data, '\n'))
	}
}

func safeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			return t
		}
	}
	return ""
}

func clipText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// FixesKind says an arriving event is one this workspace works on its own,
// rather than one it only reports.
func (s WatchSpec) FixesKind(kind watch.Kind) bool {
	if s.Action != "fix" {
		return false
	}
	if len(s.FixKinds) == 0 {
		return true
	}
	for _, want := range s.FixKinds {
		if strings.EqualFold(strings.TrimSpace(want), string(kind)) {
			return true
		}
	}
	return false
}

// refreshInboxStates asks each source whether the things still sitting in the
// inbox are still open. An event is recorded once and never revisited, so a
// merge request merged an hour later, or a review someone has since given,
// stayed on the phone looking like work. Cheap: only what is still listed,
// only the pull-request kinds, once a round.
func (d *Daemon) refreshInboxStates(ctx context.Context, spec WatchSpec) {
	if d.watchState == nil {
		return
	}
	states := watch.LoadStateLog(d.Dir)
	for _, e := range watch.RecentEvents(d.Dir, 25) {
		if e.Workspace != spec.Workspace || e.Ref == "" {
			continue
		}
		switch e.Kind {
		case watch.KindPRComment, watch.KindPRReview, watch.KindReviewRequested,
			watch.KindIssueNew, watch.KindIssueComment:
		default:
			continue
		}
		if d.watchState.IsIgnored(e.Key) {
			continue
		}
		if known, ok := states.Get(e.Key); ok && watch.Settled(e, known.Status) != "" {
			continue // already known to be over
		}
		for _, src := range spec.Sources {
			asker, ok := src.(watch.StillOpen)
			if !ok {
				continue
			}
			if state := asker.RefState(ctx, e.Ref); state != "" {
				_ = states.Set(e.Key, state, time.Now())
				break
			}
		}
	}
}

// packetFor is the handoff an earlier run left for a ticket, if it is
// recent enough to still describe the code.
func packetFor(dir, ref string) (handoff.Packet, bool) {
	p, err := handoff.Read(dir, ref)
	if err != nil || time.Since(p.WrittenAt) > handoff.MaxAge {
		return handoff.Packet{}, false
	}
	return p, true
}

// packetTrust re-runs the packet's own check so the next run knows whether
// to build on it or to start from the ticket and the diff.
func packetTrust(dir string, p handoff.Packet) string {
	if p.Verification == nil {
		return "it recorded no check, so trust nothing in it you have not confirmed."
	}
	v, ok := handoff.Verify(worktreeOf(dir, p), p, runShellQuiet)
	if ok {
		return fmt.Sprintf("`%s` passes at %s, so its done list can be trusted.", v.Cmd, shortSHA(v.At))
	}
	if n, err := handoff.CommitsSince(worktreeOf(dir, p), p.Where.Head); err == nil && n > 0 {
		return fmt.Sprintf("`%s` exits %d and the branch moved %d commit(s) since — start from the ticket and the diff, not the packet.", v.Cmd, v.Exit, n)
	}
	return fmt.Sprintf("`%s` exits %d now — start from the ticket and the diff, not the packet.", v.Cmd, v.Exit)
}

// runHandover is what a run leaves for the next one: the packet it wrote
// during the run when it wrote one, else the last lines it said.
func runHandover(dir, ref string, started time.Time, out string) string {
	if p, err := handoff.Read(dir, ref); err == nil && !p.WrittenAt.Before(started) {
		return "handoff: " + p.Summary() + " — " + handoff.MarkdownPath(dir, ref)
	}
	return watch.TailLines(out, 6)
}

func worktreeOf(dir string, p handoff.Packet) string {
	if p.Where.Worktree != "" {
		return filepath.Join(dir, p.Where.Worktree)
	}
	return dir
}

// verifyTimeout bounds a packet's verification command: a check that hangs
// must not hold the runner before claude has even started.
const verifyTimeout = 5 * time.Minute

func runShellQuiet(dir, command string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), verifyTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, "sh", "-c", command)
	c.Dir = dir
	killProcessGroup(c)
	c.WaitDelay = 10 * time.Second
	err := c.Run()
	if err == nil {
		return 0, nil
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), err
	}
	return 1, err
}

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// mirrorHandoff puts the packet a run wrote onto the ticket's workpad, so a
// machine or account without this checkout can still pick the work up.
func (d *Daemon) mirrorHandoff(spec WatchSpec, ref string, started time.Time) {
	if d.Workpad == nil {
		return
	}
	p, err := handoff.Read(spec.Dir, ref)
	if err != nil || p.WrittenAt.Before(started) {
		return
	}
	go d.Workpad(spec.Workspace, ref, "Handoff", p.Markdown())
}

// FixBranch is the branch an isolated run works on: one per ticket, so a
// second attempt lands in the same worktrees and undo knows what to remove.
func FixBranch(ref string) string {
	ref = strings.ToLower(strings.TrimSpace(ref))
	ref = strings.NewReplacer("/", "-", "#", "-", " ", "-", ":", "-").Replace(ref)
	return "corgi/" + ref
}

// isolationNote tells the run where to work. The stories skill would make
// its own branch and worktrees; here they exist already.
func isolationNote(branch string, trees []string) string {
	return "\n\nThis run is isolated: every repository already has a worktree on branch `" + branch +
		"`, created off its current HEAD. Work only in these directories and open the pull requests from this branch; " +
		"do not create another branch and do not edit the main checkouts:\n- " + strings.Join(trees, "\n- ")
}

// tripBreaker blocks a ticket after BreakerAfter failed runs in a row. A
// third try at a wall costs a run and buys nothing; a person looks instead.
func (d *Daemon) tripBreaker(spec WatchSpec, e watch.Event, lastErr string) {
	if e.Ref == "" || d.watchState.Fixes.FailedInARow(spec.Workspace, e.Ref) < watch.BreakerAfter {
		return
	}
	reason := fmt.Sprintf("%d runs failed in a row; last: %s", watch.BreakerAfter, clipText(lastErr, 120))
	d.watchState.Fixes.Block(spec.Workspace, e.Ref, reason, watch.BlockedByBreaker, time.Now())
	if d.Workpad != nil {
		go d.Workpad(spec.Workspace, e.Ref, "Blocked", reason+"\n\n`corgi agent watch unblock "+e.Ref+"` once it is fixed.")
	}
	go d.notifyAttentionAt("corgi agent · "+spec.Workspace, e.Ref+" is blocked: "+reason, spec.Workspace, e.URL)
}

// blockIfRunSaidSo honours a run's own handoff: a packet in state blocked
// or auth-required names a missing tool, credential or secret, and no run
// can supply that. The reason goes on the ticket for whoever can.
func (d *Daemon) blockIfRunSaidSo(spec WatchSpec, e watch.Event, started time.Time) {
	if e.Ref == "" {
		return
	}
	p, err := handoff.Read(spec.Dir, e.Ref)
	if err != nil || p.WrittenAt.Before(started) || (p.State != handoff.StateBlocked && p.State != handoff.StateAuthRequired) {
		return
	}
	reason := p.Blocked
	if reason == "" {
		reason = "the run needs a credential it does not have"
	}
	d.watchState.Fixes.Block(spec.Workspace, e.Ref, reason, watch.BlockedByRun, time.Now())
	if d.Workpad != nil {
		go d.Workpad(spec.Workspace, e.Ref, "Blocked", reason+"\n\n`corgi agent watch unblock "+e.Ref+"` once it is fixed.")
	}
	go d.notifyAttentionAt("corgi agent · "+spec.Workspace, e.Ref+" is blocked: "+reason, spec.Workspace, e.URL)
}

// RunLog is the last n lines of a run's log, for a surface that cannot open
// the file. "" when there is no log.
func RunLog(agentDir, key string, n int) string {
	data, err := os.ReadFile(filepath.Join(agentDir, "watch", "runs", safeName(key)+".log"))
	if err != nil {
		return ""
	}
	return watch.TailLines(string(data), n)
}

// runReceipt is what `claude -p --output-format json` says about a run
// beyond its words.
type runReceipt struct {
	ok      bool
	costUSD float64
	tokens  int64
	turns   int
}

// unwrapResult takes the JSON envelope apart: the result text for the log
// and the PR scan, the cost for the record. Anything else passes through.
func unwrapResult(raw []byte) ([]byte, runReceipt) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return raw, runReceipt{}
	}
	var env struct {
		Type     string  `json:"type"`
		Result   string  `json:"result"`
		CostUSD  float64 `json:"total_cost_usd"`
		NumTurns int     `json:"num_turns"`
		Usage    struct {
			Input      int64 `json:"input_tokens"`
			Output     int64 `json:"output_tokens"`
			CacheRead  int64 `json:"cache_read_input_tokens"`
			CacheWrite int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(trimmed, &env) != nil || env.Type != "result" {
		return raw, runReceipt{}
	}
	u := env.Usage
	return []byte(env.Result), runReceipt{ok: true, costUSD: env.CostUSD, turns: env.NumTurns,
		tokens: u.Input + u.Output + u.CacheRead + u.CacheWrite}
}

// fixModel is the model for one run: the policy's choice for the kind, or
// the escalation after a failed run on the same ticket — a harder problem
// gets the stronger model rather than another try with the same one.
func fixModel(spec WatchSpec, e watch.Event, failedBefore int) string {
	if failedBefore > 0 {
		return spec.Models.ForEscalation()
	}
	return spec.Models.ForKind(string(e.Kind))
}

// storyMode tells the stories skill which lane the ticket's own labels put
// it in, so a bug goes logs-first and a feature gets a plan; nothing when
// the labels say nothing.
func storyMode(e watch.Event) string {
	for _, l := range e.Labels {
		switch strings.ToLower(strings.TrimSpace(l)) {
		case "bug", "defect", "regression", "incident", "hotfix":
			return " --mode bug"
		case "feature", "story", "epic", "design":
			return " --mode feature"
		}
	}
	return ""
}
