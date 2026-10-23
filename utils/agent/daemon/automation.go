package daemon

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/events"
	"andriiklymiuk/corgi/utils/agent/lessons"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/watch"
	"andriiklymiuk/corgi/utils/atomicfile"
)

type Policy struct {
	Workspace string
	AutoAllow string
	DoneWhen  []string
	CompactAt int
	HandOver  bool
	Rebase    bool
	Lessons   bool
	AutoCarry bool
	Headless  bool
	DayCap    int64
}

func (d *Daemon) automation(spec WatchSpec) (handOver, autoMerge bool) {
	wc := d.watchConfig(spec)
	if wc == nil {
		return false, false
	}
	return wc.HandOver, wc.AutoMerge
}

func (d *Daemon) watchConfig(spec WatchSpec) *config.WatchConfig {
	dir := spec.AgentDir
	if dir == "" {
		dir = d.Dir
	}
	user, err := config.LoadUser(filepath.Join(dir, "config.yml"))
	if err != nil || user == nil {
		return nil
	}
	repo, _ := config.LoadRepo(spec.Dir)
	return config.Resolve(spec.Workspace, repo, user).Watch
}

func (d *Daemon) learn(spec WatchSpec, source, text string) {
	if wc := d.watchConfig(spec); wc == nil || !wc.Lessons {
		return
	}
	if err := lessons.Add(d.Dir, spec.Workspace, lessons.Lesson{At: time.Now(), Source: source, Text: text}); err != nil {
		utils.Infof("agent: lesson for %s: %v\n", spec.Workspace, err)
	}
}

func (d *Daemon) sessionOnPull(link string) (sessions.Session, bool) {
	if link == "" || d.Sessions == nil {
		return sessions.Session{}, false
	}
	for _, s := range d.Sessions.Sessions() {
		if s.Status == sessions.StatusGone || s.Status == sessions.StatusStale || s.PR == "" {
			continue
		}
		if strings.HasPrefix(s.PR, link) {
			return s, true
		}
	}
	return sessions.Session{}, false
}

func (d *Daemon) handOverEvent(ctx context.Context, spec WatchSpec, e watch.Event) bool {
	on, _ := d.automation(spec)
	if !on || chatRunRefusal(spec, e) != "" {
		return false
	}
	line := watch.HandoverLine(e)
	if line == "" {
		return false
	}
	s, ok := d.sessionOnPull(watch.PullLinkOf(e))
	if !ok {
		return false
	}
	hands := watch.LoadHands(d.Dir)
	if _, done := hands.Get(e.Key); done {
		return true
	}
	label := s.Display
	if label == "" {
		label = s.Label
	}
	_ = hands.Set(e.Key, watch.Hand{At: time.Now(), To: s.ID, Label: label, By: "daemon"})
	d.sendToSession(ctx, s.ID, line, true)
	if d.Events != nil {
		d.Events.Append(spec.Workspace, events.Event{At: time.Now().UTC(), Kind: "handover", Reason: string(e.Kind) + " " + e.Ref + " → " + label, URL: e.URL})
	}
	utils.Infof("agent: handed %s to %s\n", e.Ref, label)
	return true
}

func (d *Daemon) pullChanged(ctx context.Context, spec WatchSpec, ref, link string, was, now watch.PullStatus, known bool) {
	handOver, autoMerge := d.automation(spec)
	if handOver && link != "" && now.Checks == "failing" && (!known || was.Checks != "failing") {
		if s, ok := d.sessionOnPull(link); ok {
			hands := watch.LoadHands(d.Dir)
			key := "checks:" + ref + ":" + now.At.Format(time.RFC3339)
			label := s.Display
			if label == "" {
				label = s.Label
			}
			_ = hands.Set(key, watch.Hand{At: time.Now(), To: s.ID, Label: label, By: "daemon"})
			d.sendToSession(ctx, s.ID, "The checks went red on "+link+". Read the failing job, fix it on this branch, run the tests, and push.", true)
			utils.Infof("agent: red checks on %s handed to %s\n", ref, label)
		}
	}
	if link != "" && known && now.Checks == "passing" && was.Checks != "" && was.Checks != "passing" {
		d.creditGreen(spec, ref, link)
	}
	if link != "" && now.Checks == "passing" && (!known || was.Checks != "passing") {
		d.tickLeft(spec, link, "checks ✓", watch.LeftCI)
	}
	if autoMerge && link != "" && now.Ready() && now.Checks == "passing" && now.Mine && d.MergePull != nil && d.mergeWorthTrying(link, time.Now()) {
		if err := d.MergePull(ctx, spec.Workspace, link); err != nil {
			utils.Infof("agent: auto-merge %s: %v\n", ref, err)
			if body, first := d.rememberMergeFailure(link, err, time.Now()); first {
				go d.notifyAttentionAt(notifyTitlePrefix+spec.Workspace, body, spec.Workspace, link)
			}
			return
		}
		d.forgetMergeFailure(link)
		_ = watch.LoadPullLog(d.Dir).Set(ref, watch.PullStatus{State: "merged", Checks: now.Checks, Review: now.Review, At: time.Now()})
		if d.Events != nil {
			d.Events.Append(spec.Workspace, events.Event{At: time.Now().UTC(), Kind: "merged", Reason: "merged " + ref + " - checks ✓, approved", URL: link})
		}
		go d.notifyAttentionAt(notifyTitlePrefix+spec.Workspace, "merged "+link+" - checks ✓ · approved", spec.Workspace, link)
		now = watch.PullStatus{State: "merged", Checks: now.Checks, Review: now.Review, At: now.At}
	}
	if now.State == "merged" && (!known || was.State != "merged") {
		d.tickLeft(spec, link, "merged", watch.LeftCI, watch.LeftMerge)
		d.ticketAfterMerge(ctx, spec, link)
	}
}

type mergeFailure struct {
	At        time.Time `json:"at"`
	Msg       string    `json:"msg"`
	Permanent bool      `json:"permanent"`
}

// A merge that failed is tried again after a while; one the forge refused
// because the token cannot do it waits a day, across restarts, since asking
// again changes nothing until the token does.
const (
	mergeRetryAfter   = 30 * time.Minute
	mergeRefusedAfter = 24 * time.Hour
)

func (d *Daemon) mergeRefusedPath() string {
	return filepath.Join(d.Dir, "watch", "merge-refused.json")
}

func (d *Daemon) loadMergeFailuresLocked() {
	if d.mergeFailed != nil {
		return
	}
	d.mergeFailed = map[string]mergeFailure{}
	if data, err := os.ReadFile(d.mergeRefusedPath()); err == nil {
		_ = json.Unmarshal(data, &d.mergeFailed)
	}
}

func (d *Daemon) saveMergeRefusalsLocked() {
	kept := map[string]mergeFailure{}
	for link, f := range d.mergeFailed {
		if f.Permanent {
			kept[link] = f
		}
	}
	if data, err := json.Marshal(kept); err == nil && os.MkdirAll(filepath.Dir(d.mergeRefusedPath()), 0o700) == nil {
		_ = atomicfile.Write(d.mergeRefusedPath(), data, 0o600)
	}
}

func (d *Daemon) mergeWorthTrying(link string, now time.Time) bool {
	d.mergeMu.Lock()
	defer d.mergeMu.Unlock()
	d.loadMergeFailuresLocked()
	f, ok := d.mergeFailed[link]
	if !ok {
		return true
	}
	if f.Permanent {
		return now.Sub(f.At) >= mergeRefusedAfter
	}
	return now.Sub(f.At) >= mergeRetryAfter
}

// rememberMergeFailure keeps the refusal and says whether it is news: the same
// error on the same pull request rings once, not every poll.
func (d *Daemon) rememberMergeFailure(link string, err error, now time.Time) (body string, first bool) {
	d.mergeMu.Lock()
	defer d.mergeMu.Unlock()
	d.loadMergeFailuresLocked()
	prev, had := d.mergeFailed[link]
	f := mergeFailure{At: now, Msg: err.Error(), Permanent: watch.Permanent(err)}
	d.mergeFailed[link] = f
	if f.Permanent || prev.Permanent {
		d.saveMergeRefusalsLocked()
	}
	body = "could not merge " + link + ": " + f.Msg
	if fix := watch.WriteFix(err); fix != "" {
		body = "could not merge " + link + " - " + fix + " (auto-merge tries it again in a day)"
	}
	return body, !had || prev.Msg != f.Msg
}

func (d *Daemon) forgetMergeFailure(link string) {
	d.mergeMu.Lock()
	defer d.mergeMu.Unlock()
	d.loadMergeFailuresLocked()
	if d.mergeFailed[link].Permanent {
		delete(d.mergeFailed, link)
		d.saveMergeRefusalsLocked()
		return
	}
	delete(d.mergeFailed, link)
}

// Once the last pull request of a run is merged, its ticket moves to the
// column the workspace named; a subtask may have a column of its own.
func (d *Daemon) ticketAfterMerge(ctx context.Context, spec WatchSpec, link string) {
	wc := d.watchConfig(spec)
	if wc == nil || strings.TrimSpace(wc.AfterMerge) == "" || d.MoveTicket == nil || d.watchState == nil {
		return
	}
	rec, ok := d.watchState.Fixes.RunThatOpened(spec.Workspace, link)
	if !ok || rec.Ref == "" {
		return
	}
	pulls := watch.LoadPullLog(d.Dir)
	for _, pr := range rec.PRs {
		if pr == link {
			continue
		}
		if st, ok := pulls.Get(watch.PullRef(pr)); !ok || st.State != "merged" {
			return
		}
	}
	status := strings.TrimSpace(wc.AfterMerge)
	if rec.Parent != "" && strings.TrimSpace(wc.AfterMergeSubtasks) != "" {
		status = strings.TrimSpace(wc.AfterMergeSubtasks)
	}
	ticket := ticketOfRun(rec)
	if ticket == "" {
		return
	}
	if err := d.MoveTicket(ctx, spec.Workspace, ticket, status); err != nil {
		utils.Infof("agent: %s after merge: %v\n", ticket, err)
		go d.notifyAttentionAt(notifyTitlePrefix+spec.Workspace, "could not move "+ticket+" to "+status+": "+err.Error(), spec.Workspace, rec.URL)
		return
	}
	if d.Events != nil {
		d.Events.Append(spec.Workspace, events.Event{At: time.Now().UTC(), Kind: "moved", Reason: ticket + " → " + status + " - every pull request merged", URL: rec.URL})
	}
	go d.notifyAttentionAt(notifyTitlePrefix+spec.Workspace, ticket+" → "+status+" - every pull request merged", spec.Workspace, rec.URL)
}

// ticketOfRun is the tracker key a run worked on: its ref for a ticket run,
// the key in its title or branch for a run on a pull request; "" for neither.
func ticketOfRun(rec watch.FixRecord) string {
	if !strings.ContainsAny(rec.Ref, "#!/") && !strings.HasPrefix(rec.Ref, "slack-") {
		return rec.Ref
	}
	return storyInTitle.FindString(rec.Title + " " + rec.Branch)
}

func (d *Daemon) allowsByPolicy(s sessions.Session) bool {
	if d.Policy == nil || s.Pending == nil || s.Pending.Risk != config.AutoAllowReads || s.Pending.Tool == "Bash" {
		return false
	}
	if s.Host.Kind != sessions.HostITerm && s.Host.Kind != sessions.HostTmux {
		return false
	}
	return d.Policy(s).AutoAllow == config.AutoAllowReads
}

func (d *Daemon) autoAllow(s sessions.Session) {
	d.swaps.Add(1)
	defer d.swaps.Done()
	time.Sleep(autoAllowDelay)
	keys, err := d.Sessions.PendingAnswer(s.ID, "allow")
	if err != nil {
		return
	}
	target, err := d.Sessions.Focus(s.ID)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), focusBudget)
	defer cancel()
	if err := d.deliverText(ctx, target, keys, false); err != nil {
		utils.Infof("agent: allow %s for %s: %v\n", s.Pending.Tool, s.Label, err)
		return
	}
	label := s.Display
	if label == "" {
		label = s.Label
	}
	utils.Infof("agent: allowed %s %s for %s - the workspace's reads policy\n", s.Pending.Tool, s.Pending.Subject, label)
	d.Sessions.AutoAllowed(s.ID)
	d.flushSessions()
}

var autoAllowDelay = 400 * time.Millisecond

func (d *Daemon) gateDone(s sessions.Session) {
	if d.Policy == nil || s.Cwd == "" || d.Sessions == nil {
		return
	}
	cmds := d.Policy(s).DoneWhen
	if len(cmds) == 0 || !hasWork(s) || s.Detail == "interrupted" {
		return
	}
	if !d.claimGate(s.ID) {
		return
	}
	d.runs.Add(1)
	go func() {
		defer d.runs.Done()
		defer d.releaseGate(s.ID)
		d.runGate(s, cmds)
	}()
}

func (d *Daemon) claimGate(id string) bool {
	d.gateMu.Lock()
	defer d.gateMu.Unlock()
	if d.gating == nil {
		d.gating = map[string]bool{}
	}
	if d.gating[id] {
		return false
	}
	d.gating[id] = true
	return true
}

func (d *Daemon) releaseGate(id string) {
	d.gateMu.Lock()
	delete(d.gating, id)
	d.gateMu.Unlock()
}

func (d *Daemon) runGate(s sessions.Session, cmds []string) {
	ctx, cancel := context.WithTimeout(context.Background(), gateBudget)
	defer cancel()
	for _, cmd := range cmds {
		out, err := d.shell(ctx, s.Cwd, cmd)
		if err != nil {
			d.gateRed(ctx, s, cmd, string(out), err)
			return
		}
	}
	d.Sessions.SetGate(s.ID, true, "", time.Now())
	d.flushSessions()
}

func (d *Daemon) gateRed(ctx context.Context, s sessions.Session, cmd, out string, err error) {
	fails := d.Sessions.SetGate(s.ID, false, cmd, time.Now())
	d.flushSessions()
	utils.Infof("agent: %s stopped, but %q failed (%d in a row)\n", label(s), cmd, fails)
	if fails <= gateTries {
		d.sendToSession(ctx, s.ID, gateMessage(cmd, out, err), true)
		return
	}
	go d.notifyAttentionAt(notifyTitlePrefix+label(s), "not done: "+cmd+" still red after "+strconv.Itoa(fails)+" tries", s.Label, "")
	if p := d.Policy(s); p.Lessons && p.Workspace != "" {
		_ = lessons.Add(d.Dir, p.Workspace, lessons.Lesson{At: time.Now(), Source: "done-when", Text: gateLesson(s, cmd, fails, out)})
	}
}

func gateLesson(s sessions.Session, cmd string, fails int, out string) string {
	where := s.Branch
	if where == "" {
		where = label(s)
	}
	return "`" + cmd + "` stayed red after " + strconv.Itoa(fails) + " tries on " + where + " - " + lastLine(out)
}

func hasWork(s sessions.Session) bool {
	return (s.Changes != nil && s.Changes.Files > 0) || s.Tests != nil
}

func (d *Daemon) shell(ctx context.Context, dir, cmd string) ([]byte, error) {
	if d.Shell != nil {
		return d.Shell(ctx, dir, cmd)
	}
	c := exec.CommandContext(ctx, "sh", "-c", cmd)
	c.Dir = dir
	return c.CombinedOutput()
}

func gateMessage(cmd, out string, err error) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) > gateTail {
		lines = lines[len(lines)-gateTail:]
	}
	tail := strings.TrimSpace(strings.Join(lines, "\n"))
	if tail == "" {
		tail = err.Error()
	}
	return "Not done yet: `" + cmd + "` failed after you stopped.\n\n" + tail + "\n\nFix it, run `" + cmd + "` again, and stop when it is green."
}

const (
	gateBudget = 10 * time.Minute
	gateTries  = 3
	gateTail   = 12
)

func (d *Daemon) compactIfFull(s sessions.Session) {
	if d.Policy == nil || s.Context == nil || d.Sessions == nil {
		return
	}
	at := d.Policy(s).CompactAt
	if at <= 0 || s.Context.Percent < at || s.Detail == "interrupted" {
		return
	}
	if !s.CompactedAt.IsZero() && time.Since(s.CompactedAt) < compactCooldown {
		return
	}
	label := s.Display
	if label == "" {
		label = s.Label
	}
	utils.Infof("agent: %s is %d%% full - /compact\n", label, s.Context.Percent)
	d.Sessions.Compacted(s.ID, time.Now())
	d.sendToSession(context.Background(), s.ID, "/compact", true)
}

const compactCooldown = 10 * time.Minute

func (d *Daemon) fixLog() *watch.FixLog {
	if d.watchState != nil && d.watchState.Fixes != nil {
		return d.watchState.Fixes
	}
	return watch.LoadFixLog(d.Dir)
}

func (d *Daemon) tickLeft(spec WatchSpec, link, why string, kinds ...watch.LeftKind) {
	if n := d.fixLog().TickLeft(spec.Workspace, link, kinds, why, time.Now()); n > 0 {
		utils.Infof("agent: watch %s: %d left for you on %s done - %s\n", spec.Workspace, n, link, why)
	}
}

func (d *Daemon) creditGreen(spec WatchSpec, ref, link string) {
	r, ok := d.fixLog().MarkGreen(spec.Workspace, link, time.Now())
	if !ok {
		return
	}
	utils.Infof("agent: watch %s: %s is green after the %s run\n", spec.Workspace, ref, r.Kind)
	if d.Events != nil {
		d.Events.Append(spec.Workspace, events.Event{At: time.Now().UTC(), Kind: "green", Reason: "checks ✓ on " + ref + " after the " + r.Kind + " run", URL: link})
	}
}
