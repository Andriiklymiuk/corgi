package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils"
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
	Project   string   // tracker key prefix: ABC-123 belongs to ABC
	Repos     []string // owner/repo
	Rules     watch.Rules
	Sources   []watch.Source
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

// handleWatchEvent routes a webhook's event to the workspace it belongs to,
// else to the first one whose rules take it; the seen list keeps it single.
func (d *Daemon) handleWatchEvent(ctx context.Context, e watch.Event) {
	if d.watchers == nil {
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
			d.watchState.Hold(spec.Workspace, body, time.Now())
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
	if len(held) == 0 {
		return
	}
	body := fmt.Sprintf("%d while you were away:", len(held))
	for _, n := range held {
		body += "\n  " + n.Body
	}
	go d.notifyAttention("corgi agent · "+spec.Workspace, body, spec.Workspace)
}

// startFix launches the fix, or says in one short note why not. A deferred
// event leaves the seen list and waits in the fix log for a manual
// `corgi agent watch run`; the daemon never retries it by itself.
func (d *Daemon) startFix(ctx context.Context, spec WatchSpec, e watch.Event) string {
	now := time.Now()
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
	go d.runFix(ctx, spec, e)
	return ""
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
		return fmt.Sprintf("%s commented on %s: %s", firstNonEmpty(e.Author, "someone"), e.Ref, e.Body)
	case watch.KindPRReview:
		return fmt.Sprintf("%s reviewed %s: %s", firstNonEmpty(e.Author, "someone"), e.Ref, firstNonEmpty(e.Body, e.State))
	case watch.KindCIFailed:
		return fmt.Sprintf("red build in %s — %s", e.Ref, e.Title)
	case watch.KindReviewRequested:
		return fmt.Sprintf("%s wants your review on %s — %s", firstNonEmpty(e.Author, "someone"), e.Ref, e.Title)
	default:
		return fmt.Sprintf("%s commented on %s: %s", firstNonEmpty(e.Author, "someone"), e.Ref, e.Body)
	}
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
	watch.KindIssueNew: func(e watch.Event) string {
		return "I approve all changes; ship it and open draft PRs, then watch CI to green. /corgi:stories " + e.Ref
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
	args := []string{"-p", prompt, "--output-format", "text"}
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
	cmd := claudeCommand(ctx, spec.Dir, env, fixArgsWith(spec, e, handover)...)
	cmd.Stdin = nil
	out, runErr := cmd.Output()
	logFile.Write(out)
	if runErr != nil {
		fmt.Fprintf(logFile, "\n=== failed: %v\n", runErr)
		d.watchState.Fixes.Finish(e.Key, nil, "", runErr.Error(), time.Now())
		// What it managed to say before it stopped is worth more than the
		// error on its own: the next attempt starts from there.
		d.watchState.Fixes.SetHandover(e.Key, runHandover(spec.Dir, e.Ref, started, string(out)), time.Now())
		d.mirrorHandoff(spec, e.Ref, started)
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

func runShellQuiet(dir, command string) (int, error) {
	c := exec.Command("sh", "-c", command)
	c.Dir = dir
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
