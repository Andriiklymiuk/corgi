package daemon

import (
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
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/events"
	"andriiklymiuk/corgi/utils/agent/handoff"
	"andriiklymiuk/corgi/utils/agent/harness"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/usage"
	"andriiklymiuk/corgi/utils/agent/watch"
)

type WatchSpec struct {
	Workspace string
	Dir       string
	ConfigDir string
	AgentDir  string
	// Kind and Bin pick the harness (claude, codex) that runs fixes here;
	// Agents is the order to try when it cannot (utils/agent/daemon/harnesses.go).
	Kind            string
	Agents          []string
	Bin             string
	Isolate         bool
	PruneAfter      time.Duration
	RerunCI         bool
	Silent          bool
	Slots           int
	Batch           int
	NoRetry         bool
	Models          *config.ModelPolicy
	Routines        []config.Routine
	Project         string
	Repos           []string
	Rules           watch.Rules
	Chat            *config.SlackWatch
	Sources         []watch.Source
	Skipped         []string
	Interval        time.Duration
	Action          string
	DoneWhen        []string
	PlanReview      string
	Approve         bool
	Lease           bool
	ReviewStatus    string
	FixKinds        []string
	SkipPermissions bool
	MaxFixesPerHour int
	MaxFixesPerDay  int
	MaxFixesTotal   int
	CapSince        time.Time
	Quiet           string
	DaysOff         []time.Weekday
}

func ParseDaysOff(list []string) ([]time.Weekday, error) {
	names := map[string]time.Weekday{
		"sun": time.Sunday, "sunday": time.Sunday, "mon": time.Monday, "monday": time.Monday,
		"tue": time.Tuesday, "tues": time.Tuesday, "tuesday": time.Tuesday, "wed": time.Wednesday, "wednesday": time.Wednesday,
		"thu": time.Thursday, "thur": time.Thursday, "thurs": time.Thursday, "thursday": time.Thursday,
		"fri": time.Friday, "friday": time.Friday, "sat": time.Saturday, "saturday": time.Saturday,
	}
	seen := map[time.Weekday]bool{}
	var out []time.Weekday
	add := func(d time.Weekday) {
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	for _, raw := range list {
		for _, part := range strings.Split(raw, ",") {
			key := strings.ToLower(strings.TrimSpace(part))
			switch key {
			case "", "none", "off":
				continue
			case "weekend", "weekends":
				add(time.Saturday)
				add(time.Sunday)
				continue
			}
			d, ok := names[key]
			if !ok {
				return nil, fmt.Errorf("day %q: want mon…sun, or weekends", part)
			}
			add(d)
		}
	}
	if len(out) == 7 {
		return nil, fmt.Errorf("every day off is no watch at all — corgi agent watch disable")
	}
	sort.Slice(out, func(i, j int) bool { return (out[i]+6)%7 < (out[j]+6)%7 })
	return out, nil
}

func DaysOffWords(days []time.Weekday) string {
	var w []string
	for _, d := range days {
		w = append(w, strings.ToLower(d.String()[:3]))
	}
	return strings.Join(w, ", ")
}

func DayOff(spec WatchSpec, now time.Time) bool { return dayOff(spec, now) }

func dayOff(spec WatchSpec, now time.Time) bool {
	wd := now.Local().Weekday()
	for _, d := range spec.DaysOff {
		if d == wd {
			return true
		}
	}
	return false
}

const blockerWindow = 2 * time.Hour

const (
	DefaultMaxFixesPerHour = 3
	DefaultMaxFixesPerDay  = 10
	limitRefusePercent     = 95
)

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

type QuietHours struct {
	start, end int
}

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

const botTimeout = 30 * time.Minute

// A story builds, tests, opens pull requests and watches CI to green; a
// review or a comment answer is a fraction of that.
func fixTimeoutFor(e watch.Event) time.Duration {
	if e.Kind == watch.KindIssueNew {
		return time.Duration(1+len(e.Riders)) * 3 * time.Hour
	}
	return 45 * time.Minute
}

var claudeCommand = func(ctx context.Context, dir string, env []string, args ...string) *exec.Cmd {
	return harnessCommand(ctx, harness.For("", ""), dir, env, args...)
}

var harnessCommand = func(ctx context.Context, h harness.Harness, dir string, env []string, args ...string) *exec.Cmd {
	cmd := h.Command(ctx, dir, env, args...)
	killProcessGroup(cmd)
	cmd.WaitDelay = 10 * time.Second
	return cmd
}

// runCommand is one unattended run under the picked harness. Claude goes
// through claudeCommand so a test can stand in for it.
func (s WatchSpec) runCommand(ctx context.Context, h harness.Harness, dir string, env []string, args ...string) *exec.Cmd {
	if h.Name == harness.Claude {
		return claudeCommand(ctx, dir, env, args...)
	}
	return harnessCommand(ctx, h, dir, env, args...)
}

func (d *Daemon) loadWatchFiles() {
	if d.watchState == nil {
		d.watchState = watch.LoadState(d.Dir)
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
	onHot = func() {
		go d.notifyAttentionAt("corgi agent", "the laptop is hot — no fix starts until it cools", "", "")
	}
	onLowDisk = func() {
		go d.notifyAttentionAt("corgi agent", "under 10 GB of disk left — no fix starts until there is room (corgi agent watch prune, docker system prune)", "", "")
	}
	d.watchers = map[string]*watch.Watch{}
	d.fixBusy = map[string]chan struct{}{}
	for _, spec := range d.Watches {
		spec := spec
		spec.AgentDir = d.Dir
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
				d.advancePlans(ctx)
				go d.refreshInboxStates(ctx, spec)
			},
			Asleep: func(now time.Time) bool { return dayOff(spec, now) }}
		d.watchers[spec.Workspace] = w
		d.fixBusy[spec.Workspace] = make(chan struct{}, slotsOf(spec))
		if len(live) > 0 && spec.Interval > 0 {
			go w.Run(ctx)
		}
	}
	utils.Infof("agent: watching %d workspace(s) — tracker and review events\n", len(d.Watches))
	for _, spec := range d.Watches {
		if order := spec.agents(); len(order) > 1 {
			utils.Infof("agent: %s runs through %s; %s when it cannot\n", spec.Workspace, order[0], strings.Join(order[1:], ", then "))
		}
	}
}

func (d *Daemon) refresh() {
	d.rescan()
	d.sampleAccounts(time.Now())
	go d.pulsePeersOnce(context.Background())
	for _, w := range d.watchers {
		w.Nudge()
	}
}

func (d *Daemon) handleWatchEvent(ctx context.Context, e watch.Event) {
	if d.watchers == nil {
		return
	}
	if e.Kind == watch.KindRoutine {
		for _, spec := range d.Watches {
			if spec.Workspace != e.Workspace || d.watchState == nil {
				continue
			}
			if reason := fixDeferral(spec, d.watchState.Fixes, time.Now()); reason != "" {
				utils.Infof("agent: routine %s waits: %s\n", e.Title, reason)
				return
			}
			if d.peerLeads(spec) != "" {
				return
			}
			if !d.claimFix(spec.Workspace, e.Ref) {
				return
			}
			d.startRoutine(ctx, spec, routineFor(spec, e), e)
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
		if d.watchState.SameRefThisRound(spec.Workspace, e) > 0 {
			if isFeedback(e.Kind) && spec.FixesKind(e.Kind) {
				d.settleFix(ctx, spec, e)
			}
			return
		}
		if d.watchState.IsIgnored(e.Key) {
			return
		}
		// Another laptop leads this tracker: it fixes and rings; here the
		// event is on record and nothing more.
		if d.peerLeads(spec) != "" {
			return
		}
		body := watchBody(e)
		if e.Kind == watch.KindCIFailed && spec.RerunCI {
			if note, ok := d.rerunRedBuild(ctx, spec, e); ok {
				body += " (" + note + ")"
				if quietNow(spec, time.Now()) {
					d.watchState.HoldEvent(spec.Workspace, e.Key, body, time.Now())
					return
				}
				go d.notifyAttentionKey(notifyTitlePrefix+spec.Workspace, body, spec.Workspace, e.URL, e.Key)
				return
			}
		}
		if spec.FixesKind(e.Kind) {
			if why := chatRunRefusal(spec, e); why != "" {
				body += " (no run: " + why + ")"
			} else if isFeedback(e.Kind) {
				d.settleFix(ctx, spec, e)
				body += " (fix starts once the comments settle)"
			} else if note := d.startFix(ctx, spec, e); note != "" {
				body += " (" + note + ")"
			}
		}
		if e.Mine && (e.Kind == watch.KindPRReview || e.Kind == watch.KindPRComment) && strings.TrimSpace(e.Body) != "" {
			who := e.Author
			if who == "" {
				who = "someone"
			}
			d.learn(spec, string(e.Kind)+" "+e.Ref+" ("+who+")", e.Body)
		}
		d.startBots(ctx, spec, e)
		if d.handOverEvent(ctx, spec, e) {
			body += " (handed to the session on it)"
		}
		if e.Self && e.Kind == watch.KindIssueNew {
			utils.Infof("agent: (mine) %s\n", body)
			return
		}
		if quietNow(spec, time.Now()) {
			d.watchState.HoldEvent(spec.Workspace, e.Key, body, time.Now())
			return
		}
		go d.notifyAttentionKey(notifyTitlePrefix+spec.Workspace, body, spec.Workspace, e.URL, e.Key)
	}
}

func (d *Daemon) rerunRedBuild(ctx context.Context, spec WatchSpec, e watch.Event) (string, bool) {
	if d.RerunCI == nil || e.Ref == "" {
		return "", false
	}
	since := e.At.Add(-30 * time.Minute)
	if e.At.IsZero() {
		since = time.Now().Add(-30 * time.Minute)
	}
	r, err := d.RerunCI(ctx, spec.Workspace, e.Ref, since)
	if err != nil {
		utils.Infof("agent: %s: not rerunning the red build: %v\n", e.Ref, err)
		return "", false
	}
	if r.RunID == 0 {
		return "", false
	}
	utils.Infof("agent: %s: rerunning the failed jobs of run %d once\n", e.Ref, r.RunID)
	return "rerunning its failed jobs once — a second red is handed on", true
}

func quietNow(spec WatchSpec, now time.Time) bool {
	if dayOff(spec, now) {
		return true
	}
	q, err := ParseQuiet(spec.Quiet)
	return err == nil && q.Contains(now)
}

func (d *Daemon) releaseHeld(spec WatchSpec, now time.Time) {
	if d.watchState == nil || quietNow(spec, now) || d.peerLeads(spec) != "" {
		return
	}
	held := d.watchState.TakeHeld(spec.Workspace)
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
	go d.notifyAttention(notifyTitlePrefix+spec.Workspace, body, spec.Workspace)
}

func (d *Daemon) retryDeferred(ctx context.Context, spec WatchSpec, now time.Time) {
	if spec.NoRetry || spec.Action != "fix" || d.watchState == nil || d.peerLeads(spec) != "" {
		return
	}
	var queue []watch.Event
	for _, e := range d.watchState.Fixes.DeferredEvents() {
		if e.Workspace != spec.Workspace || now.Before(e.NotBefore) {
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
	if fixDeferral(spec, d.watchState.Fixes, now) != "" {
		return
	}
	sort.SliceStable(queue, func(i, j int) bool { return watch.Less(queue[i], queue[j]) })
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

func (d *Daemon) stillWorthFixing(ctx context.Context, spec WatchSpec, e watch.Event) string {
	if spec.Rules.Enabled {
		if why := spec.Rules.Why(e); why != "" {
			return why
		}
	}
	if e.Ref == "" {
		return ""
	}
	if who := d.sessionOnTicket(e.Ref); who != "" {
		return who + " is on it now"
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
		asker, ok := src.(watch.RefStater)
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

// sessionOnTicket names the live session — here or on a peer laptop — whose
// ticket or branch is ref, so an unattended run never doubles a person's work.
func (d *Daemon) sessionOnTicket(ref string) string {
	if d.Sessions == nil || ref == "" {
		return ""
	}
	want := strings.ToUpper(strings.TrimSpace(ref))
	for _, s := range d.Sessions.Sessions() {
		if s.Status == sessions.StatusGone || s.Status == sessions.StatusStale {
			continue
		}
		if ticketOf(s.Ticket, s.TicketKey, s.Branch) == want {
			return firstNonEmpty(s.Display, s.Label)
		}
	}
	for _, p := range d.Sessions.Peers() {
		if !p.Alive {
			continue
		}
		for _, s := range p.Sessions {
			if s.Status == string(sessions.StatusGone) || s.Status == string(sessions.StatusStale) {
				continue
			}
			if ticketOf(s.Ticket, "", s.Branch) == want {
				return p.Name + "'s " + firstNonEmpty(s.Display, s.Label)
			}
		}
	}
	return ""
}

func ticketOf(ticket, key, branch string) string {
	if t := strings.ToUpper(strings.TrimSpace(ticket)); t != "" {
		return t
	}
	if k := strings.ToUpper(strings.TrimSpace(key)); k != "" {
		if i := strings.LastIndex(k, ":"); i >= 0 {
			return k[i+1:]
		}
		return k
	}
	return sessions.TicketInBranch(branch)
}

func (d *Daemon) startFix(ctx context.Context, spec WatchSpec, e watch.Event) string {
	now := time.Now()
	if why := chatRunRefusal(spec, e); why != "" {
		d.watchState.Fixes.DropDeferred(e.Key)
		return "not started: " + why
	}
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
		if isFeedback(e.Kind) {
			d.queueFollowUp(spec, e)
			return "queued for after the fix already running"
		}
		return "a fix for it is already running"
	}
	if spec.Batch > 1 && e.Kind == watch.KindIssueNew && e.Source != "slack" {
		if n := d.holdForBatch(ctx, spec, e); n > 0 {
			return fmt.Sprintf("waits %s for tickets to batch (%d so far)", batchSettle, n)
		}
		return ""
	}
	d.watchState.Fixes.StartFor(e, now)
	d.spawnFix(ctx, spec, e)
	return ""
}

var batchSettle = 90 * time.Second

// Tickets that arrive within a short window ride in one run: one preflight,
// one stack, one context. A full batch leaves at once; the rest leave when
// the window closes. Returns how many are waiting, or 0 when the run left.
func (d *Daemon) holdForBatch(ctx context.Context, spec WatchSpec, e watch.Event) int {
	d.attentionMu.Lock()
	if d.fixBatch == nil {
		d.fixBatch = map[string][]watch.Event{}
		d.fixBatchAt = map[string]*time.Timer{}
	}
	ws := spec.Workspace
	d.fixBatch[ws] = append(d.fixBatch[ws], e)
	if len(d.fixBatch[ws]) >= spec.Batch {
		if t := d.fixBatchAt[ws]; t != nil {
			t.Stop()
		}
		d.attentionMu.Unlock()
		d.flushBatch(ctx, spec)
		return 0
	}
	if d.fixBatchAt[ws] == nil {
		d.fixBatchAt[ws] = time.AfterFunc(batchSettle, func() { d.flushBatch(ctx, spec) })
	}
	n := len(d.fixBatch[ws])
	d.attentionMu.Unlock()
	return n
}

func (d *Daemon) flushBatch(ctx context.Context, spec WatchSpec) {
	d.attentionMu.Lock()
	batch := d.fixBatch[spec.Workspace]
	delete(d.fixBatch, spec.Workspace)
	delete(d.fixBatchAt, spec.Workspace)
	d.attentionMu.Unlock()
	if len(batch) == 0 {
		return
	}
	now := time.Now()
	leader := batch[0]
	leader.Riders = append([]watch.Event(nil), batch[1:]...)
	for _, e := range batch {
		d.watchState.Fixes.StartFor(e, now)
	}
	d.spawnFix(ctx, spec, leader)
}

var commentSettle = time.Minute

func isFeedback(kind watch.Kind) bool {
	return kind == watch.KindPRComment || kind == watch.KindPRReview || kind == watch.KindIssueComment
}

func (d *Daemon) settleFix(ctx context.Context, spec WatchSpec, e watch.Event) {
	key := spec.Workspace + "/" + e.Ref
	d.attentionMu.Lock()
	defer d.attentionMu.Unlock()
	if d.fixSettle == nil {
		d.fixSettle = map[string]*time.Timer{}
	}
	if t := d.fixSettle[key]; t != nil {
		t.Stop()
	}
	d.fixSettle[key] = time.AfterFunc(commentSettle, func() {
		d.attentionMu.Lock()
		delete(d.fixSettle, key)
		d.attentionMu.Unlock()
		if note := d.startFix(ctx, spec, e); note != "" {
			utils.Infof("agent: watch %s: %s: %s\n", spec.Workspace, e.Ref, note)
		}
	})
}

func (d *Daemon) queueFollowUp(spec WatchSpec, e watch.Event) {
	d.attentionMu.Lock()
	defer d.attentionMu.Unlock()
	if d.fixFollowUp == nil {
		d.fixFollowUp = map[string]watch.Event{}
	}
	d.fixFollowUp[spec.Workspace+"/"+e.Ref] = e
}

func (d *Daemon) takeFollowUp(spec WatchSpec, ref string) (watch.Event, bool) {
	d.attentionMu.Lock()
	defer d.attentionMu.Unlock()
	e, ok := d.fixFollowUp[spec.Workspace+"/"+ref]
	delete(d.fixFollowUp, spec.Workspace+"/"+ref)
	return e, ok
}

func (d *Daemon) spawnFix(ctx context.Context, spec WatchSpec, e watch.Event) {
	if e.Source == "slack" {
		d.say(ctx, spec, e, "", "eyes")
	}
	d.runs.Add(1)
	go func() {
		defer d.runs.Done()
		d.runFix(ctx, spec, e)
		d.advancePlans(ctx)
		if follow, ok := d.takeFollowUp(spec, e.Ref); ok && ctx.Err() == nil {
			d.settleFix(ctx, spec, follow)
		}
	}()
}

func fixDeferral(spec WatchSpec, log *watch.FixLog, now time.Time) string {
	perHour, perDay := spec.FixCaps()
	if log.StartedSince(spec.Workspace, now.Add(-time.Hour)) >= perHour {
		return fmt.Sprintf("%d/h cap", perHour)
	}
	if log.StartedSince(spec.Workspace, now.Add(-24*time.Hour)) >= perDay {
		return fmt.Sprintf("%d/day cap", perDay)
	}
	if spec.MaxFixesTotal > 0 && log.StartedSince(spec.Workspace, spec.CapSince) >= spec.MaxFixesTotal {
		return fmt.Sprintf("trip cap %d", spec.MaxFixesTotal)
	}
	if dayOff(spec, now) {
		return "day off"
	}
	if q, err := ParseQuiet(spec.Quiet); err == nil && q.Contains(now) {
		return "quiet hours"
	}
	if tooHot(now) {
		return "too hot"
	}
	if lowDisk(now, firstNonEmpty(spec.AgentDir, ".")) {
		return "low disk"
	}
	if pct, ok := limitUsed(spec.ConfigDir, now); ok && !hasFallback(spec, log, now) {
		if pct >= limitRefusePercent {
			return fmt.Sprintf("limit %d%%", pct)
		}
		if typical := log.TypicalSpend(spec.Workspace); typical > 0 && pct+typical > limitRefusePercent {
			return fmt.Sprintf("a run here costs about %d%% and %d%% is used", typical, pct)
		}
	}
	return ""
}

type FixBudget struct {
	Hour     int       `json:"hour"`
	PerHour  int       `json:"perHour"`
	Day      int       `json:"day"`
	PerDay   int       `json:"perDay"`
	Today    int       `json:"today"`
	Trip     int       `json:"trip,omitempty"`
	PerTrip  int       `json:"perTrip,omitempty"`
	Last     time.Time `json:"last,omitempty"`
	Deferred int       `json:"deferred"`
}

func BudgetFor(spec WatchSpec, log *watch.FixLog, now time.Time) FixBudget {
	b := FixBudget{Deferred: log.DeferredCount(spec.Workspace)}
	b.PerHour, b.PerDay = spec.FixCaps()
	b.Hour = log.StartedSince(spec.Workspace, now.Add(-time.Hour))
	b.Day = log.StartedSince(spec.Workspace, now.Add(-24*time.Hour))
	local := now.Local()
	b.Today = log.StartedSince(spec.Workspace, time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location()))
	b.Last, _ = log.LastStarted(spec.Workspace)
	if spec.MaxFixesTotal > 0 {
		b.PerTrip = spec.MaxFixesTotal
		b.Trip = log.StartedSince(spec.Workspace, spec.CapSince)
	}
	return b
}

func (b FixBudget) String() string {
	s := fmt.Sprintf("%d/%d this hour · %d/%d today", b.Hour, b.PerHour, b.Day, b.PerDay)
	if b.PerTrip > 0 {
		s += fmt.Sprintf(" · %d/%d this trip", b.Trip, b.PerTrip)
	}
	return s
}

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
		if e.Source == "slack" {
			return fmt.Sprintf("%s posted %d pull request(s) for review in %s — %s",
				firstNonEmpty(e.Author, "someone"), len(e.Links), firstNonEmpty(e.State, "chat"), clipText(e.Body, 120))
		}
		return fmt.Sprintf("%s wants your review on %s — %s", firstNonEmpty(e.Author, "someone"), e.Ref, e.Title)
	case watch.KindChatMention, watch.KindChatMessage:
		return fmt.Sprintf("%s in %s: %s", firstNonEmpty(e.Author, "someone"),
			firstNonEmpty(e.State, "chat"), clipText(e.Body, 160))
	default:
		return commentLine(e)
	}
}

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

var fixPrompts = map[watch.Kind]func(e watch.Event) string{
	watch.KindRoutine: func(e watch.Event) string {
		return e.Body
	},
	watch.KindTask: func(e watch.Event) string {
		body := strings.TrimSpace(e.Body)
		if body == "" {
			body = "(no description — use your judgement)"
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
		p := "I approve all changes; ship it and open draft PRs, then watch CI to green. /corgi:stories " + strings.Join(e.Refs(), " ") + storyMode(e)
		if e.Parent != "" {
			p += fmt.Sprintf("\n%s is a subtask of %s (%q). Read the parent for context — the bug report, the acceptance criteria, "+
				"the earlier pull requests — but the change is scoped to %s alone.", e.Ref, e.Parent, e.ParentTitle, e.Ref)
		}
		return p
	},
	watch.KindIssueComment: func(e watch.Event) string {
		return fmt.Sprintf("A new comment on %s from %s says: %q. Read it and decide. "+
			"If it asks a question or for information, answer it as a comment on %s through the tracker "+
			"(the Linear or Jira MCP tools, or the REST API with the saved token) and do NOT open a PR. "+
			"If it asks for a change, apply it on the existing branch for %s — find it by the ticket key in the branch names — "+
			"and when there is no such branch run /corgi:stories %s. I approve all changes; draft PRs only.",
			e.Ref, firstNonEmpty(e.Author, "someone"), e.Body, e.Ref, e.Ref, e.Ref)
	},
	watch.KindPRComment:   reviewFeedbackPrompt,
	watch.KindPRReview:    reviewFeedbackPrompt,
	watch.KindChatMention: chatPrompt,
	watch.KindChatMessage: chatPrompt,
	watch.KindReviewRequested: func(e watch.Event) string {
		if e.Source == "slack" {
			return chatPrompt(e)
		}
		return "Review this pull request, which " + firstNonEmpty(e.Author, "a colleague") +
			" asked me to review: " + e.URL + ". It is THEIR branch — read the diff and post a review " +
			"(a summary and inline comments). Do not push commits to it, do not resolve their threads" +
			approveClause + ". If it is good, say so and say why. /corgi:review " + e.URL
	},
	watch.KindCIFailed: func(e watch.Event) string {
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

func FixPrompt(e watch.Event) string { return fixPrompt(e) }

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

const approveClause = "__APPROVE__"

func withApprove(prompt string, approve bool) string {
	if approve {
		return strings.Replace(prompt, approveClause, ", and approve it on my behalf only when the review has no blocking finding and the risk card says auto-approve: yes — otherwise post the findings and leave it unapproved", 1)
	}
	return strings.Replace(prompt, approveClause, ", and do not approve it on my behalf", 1)
}

func fixPrompt(e watch.Event) string {
	if build := fixPrompts[e.Kind]; build != nil {
		return build(e)
	}
	return ""
}

func unattendedSuffix(spec WatchSpec, e watch.Event) string {
	s := "\n\nNobody is reading this run as it happens. Never ask a question, never offer options or wait for a choice: " +
		"pick the recommended option yourself, say which you picked and why, and go on. " +
		"If you truly cannot proceed, leave a handoff with `--blocked <reason>` (the question goes in `--uncertain`) and stop.\n" +
		"Before you finish: review your own diff the way you would review someone else's, " +
		"and fix what you find — nobody has looked at this but you. "
	ownPR := e.Kind != watch.KindReviewRequested
	if ownPR {
		trail := "corgi watch · " + spec.Workspace + " · " + string(e.Kind) + " " + e.Ref
		if e.URL != "" {
			trail += " · " + e.URL
		}
		s += "Put this line at the end of the pull request body so whoever reviews it knows where it came from: " + trail + "\n"
	}
	s += "Say plainly at the end what you changed and what your own review found.\n"
	if ownPR {
		s += "The pull request body carries a `## Evidence` section — changed files with a reason each; the commands you ran with their results; " +
			"each test mapped to the acceptance criterion it protects; known limitations and residual risk — facts, one line each.\n"
	}
	return s + "If you stop with work remaining, blocked, or unsure, leave a handoff for the next run before you end: " +
		"`corgi agent handoff --ref " + e.Ref + " --done … --remaining … --decision … --uncertain … --next … --verify \"<the check you ran>\"` " +
		"(one flag per item; short sentences; no secrets)."
}

// A VPN client that wants a browser sign-in cannot be driven when nobody is
// there, so a headless run leaves it out of the stack preflight. Claude Code
// does not update itself under a run either: a broken update would crash
// every run until somebody is back.
func headlessEnv(configDir, omit string) []string {
	var env []string
	if configDir != "" {
		env = append(env, "CLAUDE_CONFIG_DIR="+configDir)
	}
	keys := []string{}
	for _, k := range strings.Split(omit, ",") {
		if k = strings.TrimSpace(k); k != "" && k != "useAwsVpn" {
			keys = append(keys, k)
		}
	}
	return append(env, "CORGI_OMIT="+strings.Join(append(keys, "useAwsVpn"), ","), "DISABLE_AUTOUPDATER=1")
}

// harnessEnv drops the first agent's config dir when another agent takes
// the run: CLAUDE_CONFIG_DIR means nothing to codex, and its own home has
// its own login.
func harnessEnv(spec WatchSpec, h harness.Harness, env []string) []string {
	if h.Name == spec.agents()[0] || spec.ConfigDir == "" {
		return env
	}
	out := env[:0:0]
	for _, kv := range env {
		if !strings.HasPrefix(kv, "CLAUDE_CONFIG_DIR=") {
			out = append(out, kv)
		}
	}
	return out
}

func fixArgs(spec WatchSpec, e watch.Event) []string {
	return fixArgsWith(spec, e, "")
}

func fixArgsWith(spec WatchSpec, e watch.Event, handover string) []string {
	prompt := withApprove(fixPrompt(e), spec.Approve) + unattendedSuffix(spec, e)
	if p, ok := packetFor(spec.Dir, e.Ref); ok {
		prompt += "\n\nAn earlier run left a handoff for this ticket. Read it first; it is typed state, not a transcript. " +
			"Its verification was re-run at the current head: " + packetTrust(spec.Dir, p, spec.DoneWhen) + "\n" + p.Markdown()
	} else if handover = strings.TrimSpace(handover); handover != "" {
		prompt += "\n\nAn earlier run on this stopped part-way. This is the last thing it said — " +
			"treat it as notes, not as truth, and check anything it claims before building on it:\n" + handover
	}
	return harness.For("", "").PrintArgs(harness.Print{Prompt: prompt, SkipPermissions: spec.SkipPermissions})
}

// fixPrint is the unattended run for one event, for whichever harness the
// spec names; fixArgsWith is its Claude shape, kept for the tests that
// read the prompt at index one.
func fixPrint(spec WatchSpec, e watch.Event, handover string) harness.Print {
	args := fixArgsWith(spec, e, handover)
	return harness.Print{Prompt: args[1], SkipPermissions: spec.SkipPermissions}
}

var prLink = regexp.MustCompile(`https://(?:github\.com/[^\s)]+/pull/\d+|[^\s)]+/-/merge_requests/\d+)`)

func slotsOf(spec WatchSpec) int {
	if spec.Slots > 1 && spec.Isolate {
		return spec.Slots
	}
	return 1
}

func (d *Daemon) takeSlot(workspace string) func() {
	sem := d.fixBusy[workspace]
	sem <- struct{}{}
	return func() { <-sem }
}

func (d *Daemon) runFix(ctx context.Context, spec WatchSpec, e watch.Event) {
	defer d.releaseFix(spec.Workspace, e.Ref)
	for _, r := range e.Riders {
		defer d.releaseFix(spec.Workspace, r.Ref)
	}
	defer d.takeSlot(spec.Workspace)()
	daemonCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, fixTimeoutFor(e))
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

	if b, ok := d.watchState.Fixes.Blocked(spec.Workspace, e.Ref); ok && e.Ref != "" {
		d.watchState.Fixes.Finish(e.Key, nil, "", "not started: blocked ("+b.By+"): "+b.Reason, time.Now())
		utils.Infof("agent: watch: %s is blocked (%s): %s — corgi agent watch unblock %s\n", e.Ref, b.By, b.Reason, e.Ref)
		return
	}
	if kind, runs := d.watchState.Fixes.RecentBlocker(spec.Workspace, blockerWindow, time.Now()); runs >= 2 {
		d.watchState.Fixes.Defer(e)
		d.watchState.Fixes.Finish(e.Key, nil, "", "not started: the last "+strconv.Itoa(runs)+" runs here failed on "+kind, time.Now())
		go d.notifyAttentionAt(notifyTitlePrefix+spec.Workspace,
			"not working on "+e.Ref+": the last runs failed on "+kind+" — fix that and run corgi agent watch run",
			spec.Workspace, e.URL)
		return
	}
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
	env := headlessEnv(spec.ConfigDir, os.Getenv("CORGI_OMIT"))
	fmt.Fprintf(logFile, "=== %s %s %s\n", time.Now().Format(time.RFC3339), e.Kind, e.Ref)
	before, hadBefore := usage.ReadLimits(spec.ConfigDir)
	handover := d.watchState.Fixes.LastHandover(spec.Workspace, e.Ref)
	started := time.Now()
	print := fixPrint(spec, e, handover)
	if model := fixModel(spec, e, d.watchState.Fixes.FailedInARow(spec.Workspace, e.Ref)); model != "" {
		print.Model = model
		fmt.Fprintf(logFile, "=== model: %s\n", model)
	}
	if spec.Isolate && d.Isolate != nil && !watch.ReadOnlyRoutine(e) {
		branch := FixBranch(e.Ref)
		trees, err := d.Isolate(spec.Dir, branch)
		if err != nil {
			fmt.Fprintf(logFile, "\n=== could not isolate: %v\n", err)
			d.watchState.Fixes.Finish(e.Key, nil, "", "could not create worktrees: "+err.Error(), time.Now())
			go d.notifyAttentionAt(notifyTitlePrefix+spec.Workspace,
				fmt.Sprintf("fix for %s did not start: could not create worktrees: %v", e.Ref, err), spec.Workspace, e.URL)
			return
		}
		d.watchState.Fixes.SetBranch(e.Key, branch)
		print.Prompt += IsolationNote(branch, trees)
		fmt.Fprintf(logFile, "=== worktrees on %s: %s\n", branch, strings.Join(trees, ", "))
	}
	h := d.pickHarnessFor(spec)
	if h.Name != harness.Claude {
		fmt.Fprintf(logFile, "=== harness: %s\n", h.Name)
	}
	d.watchState.Fixes.SetHarness(e.Key, h.Name)
	env = harnessEnv(spec, h, env)
	cmd := spec.runCommand(ctx, h, spec.Dir, env, h.PrintArgs(print)...)
	cmd.Stdin = nil
	raw, runErr := cmd.Output()
	out, receipt := unwrapWith(h, raw)
	logFile.Write(out)
	if receipt.ok {
		fmt.Fprintf(logFile, "\n=== cost: $%.4f · %d tokens · %d turns\n", receipt.costUSD, receipt.tokens, receipt.turns)
		d.watchState.Fixes.SetCost(e.Key, receipt.costUSD, receipt.tokens)
	}
	if runErr != nil && daemonCtx.Err() != nil {
		// The daemon is stopping, not the run failing: the record stays open
		// and the next start offers the event again. Nothing rings.
		fmt.Fprintf(logFile, "\n=== interrupted: the daemon stopped mid-run; offered again at the next start\n")
		return
	}
	if runErr != nil {
		fmt.Fprintf(logFile, "\n=== failed: %v\n", runErr)
		d.watchState.Fixes.Finish(e.Key, nil, "", runErr.Error(), time.Now())
		for _, r := range e.Riders {
			d.watchState.Fixes.Finish(r.Key, nil, "", runErr.Error(), time.Now())
			d.retryOnceLater(spec, r)
		}
		d.watchState.Fixes.SetHandover(e.Key, runHandover(spec.Dir, e.Ref, started, string(out)), time.Now())
		d.mirrorHandoff(spec, e.Ref, started)
		d.tripBreaker(spec, e, runErr.Error())
		d.routineReport(spec, e, string(out), runErr)
		if e.Source == "slack" {
			d.say(ctx, spec, e, chatOutcome(nil, "", runErr.Error()), "x")
		}
		body := fmt.Sprintf("fix for %s failed: %v — log: %s", e.Ref, runErr, logPath)
		if d.retryOnceLater(spec, e) {
			body += fmt.Sprintf(" — one retry in %s", retryCrashAfter)
		}
		go d.notifyAttentionAt(notifyTitlePrefix+spec.Workspace, body, spec.Workspace, e.URL)
		return
	}
	links := uniqueStrings(prLink.FindAllString(string(out), -1))
	note := ""
	body := "fixed " + strings.Join(e.Refs(), " + ")
	for _, r := range e.Riders {
		d.watchState.Fixes.Finish(r.Key, nil, "in one run with "+e.Ref, "", time.Now())
	}
	if t := strings.TrimSpace(e.Title); t != "" {
		body += " — " + t
	}
	if len(links) > 0 {
		body = watch.PullLines(body, links)
	} else if last := lastLine(string(out)); last != "" {
		note = clipText(last, 160)
		body += " — " + note
	}
	d.watchState.Fixes.Finish(e.Key, links, note, "", time.Now())
	d.watchState.Fixes.SetHandover(e.Key, runHandover(spec.Dir, e.Ref, started, string(out)), time.Now())
	d.mirrorHandoff(spec, e.Ref, started)
	d.blockIfRunSaidSo(spec, e, started)
	d.routineReport(spec, e, string(out), nil)
	if len(links) > 0 && d.Delivered != nil {
		d.Delivered(spec.Workspace, e, links)
	}
	if after, ok := usage.ReadLimits(spec.ConfigDir); ok && hadBefore {
		d.watchState.Fixes.SetSpent(e.Key, after.FiveHour.Percent-before.FiveHour.Percent)
	}
	if e.Source == "slack" {
		said := chatOutcome(links, note, "")
		if lines := d.chatReviewReply(ctx, spec, e); lines != "" {
			said = lines
		}
		d.say(ctx, spec, e, said, d.chatMark(ctx, spec, e))
	}
	target := e.URL
	if len(links) > 0 {
		target = links[0]
	}
	go d.notifyAttentionAt(notifyTitlePrefix+spec.Workspace, body, spec.Workspace, target)
}

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
			continue
		}
		for _, src := range spec.Sources {
			asker, ok := src.(watch.RefStater)
			if !ok {
				continue
			}
			if state := asker.RefState(ctx, e.Ref); state != "" {
				_ = states.Set(e.Key, state, time.Now())
				break
			}
		}
	}
	d.refreshPulls(ctx, spec)
}

const pullFresh = 2 * time.Minute

func (d *Daemon) refreshPulls(ctx context.Context, spec WatchSpec) {
	refs := map[string]bool{}
	links := map[string]string{}
	add := func(link string) {
		if ref := watch.PullRef(link); ref != "" {
			refs[ref] = true
			if i := strings.Index(link, "#"); i > 0 {
				link = link[:i]
			}
			links[ref] = link
		}
	}
	for _, e := range watch.RecentEvents(d.Dir, 40) {
		if e.Workspace != spec.Workspace || d.watchState.IsIgnored(e.Key) {
			continue
		}
		switch e.Kind {
		case watch.KindPRComment, watch.KindPRReview, watch.KindReviewRequested, watch.KindCIFailed:
			if e.Ref != "" {
				refs[e.Ref] = true
			}
			add(e.URL)
		}
	}
	for _, r := range watch.LoadFixLog(d.Dir).RecentFixes("", 50) {
		if r.Workspace != spec.Workspace {
			continue
		}
		for _, link := range r.PRs {
			add(link)
		}
	}
	if d.Sessions != nil {
		for _, s := range d.Sessions.Sessions() {
			if s.PR != "" && (s.Label == spec.Workspace || filepath.Base(s.Folder) == spec.Workspace) {
				add(s.PR)
			}
		}
	}
	if len(refs) == 0 {
		return
	}
	pulls := watch.LoadPullLog(d.Dir)
	now := time.Now()
	for ref := range refs {
		was, known := pulls.Get(ref)
		if known && now.Sub(was.At) < pullFresh {
			continue
		}
		if known && (was.State == "merged" || was.State == "closed") {
			continue
		}
		for _, src := range spec.Sources {
			asker, ok := src.(watch.PullAsker)
			if !ok {
				continue
			}
			if st, ok := asker.PullStatus(ctx, ref); ok {
				_ = pulls.Set(ref, st)
				d.pullChanged(ctx, spec, ref, links[ref], was, st, known)
				break
			}
		}
	}
}

func packetFor(dir, ref string) (handoff.Packet, bool) {
	p, err := handoff.Read(dir, ref)
	if err != nil || time.Since(p.WrittenAt) > handoff.MaxAge {
		return handoff.Packet{}, false
	}
	return p, true
}

func packetTrust(dir string, p handoff.Packet, trusted []string) string {
	if p.Verification == nil {
		return "it recorded no check, so trust nothing in it you have not confirmed."
	}
	if !handoff.TrustedCommand(p.Verification.Cmd, trusted) {
		return fmt.Sprintf("its check `%s` is not one of this workspace's doneWhen commands, so it was not re-run — trust nothing in it you have not confirmed.", p.Verification.Cmd)
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

func runHandover(dir, ref string, started time.Time, out string) string {
	if p, err := handoff.Read(dir, ref); err == nil && !p.WrittenAt.Before(started) {
		return "handoff: " + p.Summary() + " — " + handoff.MarkdownPath(dir, ref)
	}
	return watch.TailLines(out, 6)
}

func worktreeOf(dir string, p handoff.Packet) string { return handoff.WorktreeDir(dir, p) }

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

func FixBranch(ref string) string {
	ref = strings.ToLower(strings.TrimSpace(ref))
	ref = strings.NewReplacer("/", "-", "#", "-", " ", "-", ":", "-").Replace(ref)
	return "corgi/" + ref
}

func IsolationNote(branch string, trees []string) string {
	return "\n\nThis run is isolated: every repository already has a worktree on branch `" + branch +
		"`, created off its current HEAD. Work only in these directories and open the pull requests from this branch; " +
		"do not create another branch and do not edit the main checkouts:\n- " + strings.Join(trees, "\n- ")
}

const retryCrashAfter = 30 * time.Minute

// A run that crashed once gets a second go later; the second crash trips
// the breaker, so there is never a third.
func (d *Daemon) retryOnceLater(spec WatchSpec, e watch.Event) bool {
	if e.Ref == "" || spec.NoRetry || d.watchState.Fixes.FailedInARow(spec.Workspace, e.Ref) != 1 {
		return false
	}
	if _, blocked := d.watchState.Fixes.Blocked(spec.Workspace, e.Ref); blocked {
		return false
	}
	e.NotBefore = time.Now().Add(retryCrashAfter)
	d.watchState.Fixes.Defer(e)
	return true
}

func (d *Daemon) tripBreaker(spec WatchSpec, e watch.Event, lastErr string) {
	if e.Ref == "" || d.watchState.Fixes.FailedInARow(spec.Workspace, e.Ref) < watch.BreakerAfter {
		return
	}
	reason := fmt.Sprintf("%d runs failed in a row; last: %s", watch.BreakerAfter, clipText(lastErr, 120))
	d.watchState.Fixes.Block(spec.Workspace, e.Ref, reason, watch.BlockedByBreaker, time.Now())
	if d.Workpad != nil {
		go d.Workpad(spec.Workspace, e.Ref, "Blocked", reason+"\n\n`corgi agent watch unblock "+e.Ref+"` once it is fixed.")
	}
	go d.notifyAttentionAt(notifyTitlePrefix+spec.Workspace, e.Ref+" is blocked: "+reason, spec.Workspace, e.URL)
}

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
	go d.notifyAttentionAt(notifyTitlePrefix+spec.Workspace, e.Ref+" is blocked: "+reason, spec.Workspace, e.URL)
}

func RunLog(agentDir, key string, n int) string {
	data, err := os.ReadFile(filepath.Join(agentDir, "watch", "runs", safeName(key)+".log"))
	if err != nil {
		return ""
	}
	return watch.TailLines(string(data), n)
}

type runReceipt struct {
	ok      bool
	costUSD float64
	tokens  int64
	turns   int
}

func unwrapResult(raw []byte) ([]byte, runReceipt) { return unwrapWith(harness.For("", ""), raw) }

func unwrapWith(h harness.Harness, raw []byte) ([]byte, runReceipt) {
	out, r := h.Unwrap(raw)
	return out, runReceipt{ok: r.OK, costUSD: r.CostUSD, tokens: r.Tokens, turns: r.Turns}
}

func fixModel(spec WatchSpec, e watch.Event, failedBefore int) string {
	if failedBefore > 0 {
		return spec.Models.ForEscalation()
	}
	return spec.Models.ForKind(string(e.Kind))
}

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
