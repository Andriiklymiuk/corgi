package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/peers"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/usage"
	"andriiklymiuk/corgi/utils/agent/watch"
	"andriiklymiuk/corgi/utils/atomicfile"
)

// Peer laptops: once a minute this daemon tells each peer what it watches
// and hears the same back. Before a watch event turns into a fix or a
// push, peerLeads says whether another laptop leads that tracker; if so,
// the event is recorded here and nothing else happens.

func WatchIdentitiesPath(dir string) string { return filepath.Join(dir, "watch", "identities.json") }

// Identities names the trackers a watch spec reads, the way two laptops
// can compare them: source/project and source/repo, never a local path.
func (s WatchSpec) Identities() []string {
	seen := map[string]bool{}
	var out []string
	add := func(id string) {
		id = strings.ToLower(strings.TrimSpace(id))
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	for _, src := range s.Sources {
		name := src.Name()
		if p := strings.TrimSpace(s.Project); p != "" {
			add(name + "/" + p)
		}
		for _, r := range s.Repos {
			add(name + "/" + r)
		}
	}
	sort.Strings(out)
	return out
}

func (d *Daemon) allWatchIdentities() []string {
	seen := map[string]bool{}
	var out []string
	for _, spec := range d.Watches {
		for _, id := range spec.Identities() {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	sort.Strings(out)
	if out == nil {
		out = []string{}
	}
	return out
}

func (d *Daemon) publishWatchIdentities() {
	ids := d.allWatchIdentities()
	data, _ := json.Marshal(ids)
	_ = os.MkdirAll(filepath.Dir(WatchIdentitiesPath(d.Dir)), 0o700)
	_ = atomicfile.Write(WatchIdentitiesPath(d.Dir), data, 0o600)
	d.publishWatchAgents()
}

// WatchAgentsPath is each watched workspace's agent order and config dir,
// so a pulse answered from the files knows whether a fallback agent could
// take the runs a spent window keeps claude from.
func WatchAgentsPath(dir string) string { return filepath.Join(dir, "watch", "agents.json") }

type watchAgents struct {
	Agents    []string `json:"agents"`
	ConfigDir string   `json:"configDir,omitempty"`
}

func (d *Daemon) publishWatchAgents() {
	out := map[string]watchAgents{}
	for _, spec := range d.Watches {
		out[spec.Workspace] = watchAgents{Agents: spec.agents(), ConfigDir: spec.ConfigDir}
	}
	data, _ := json.Marshal(out)
	_ = atomicfile.Write(WatchAgentsPath(d.Dir), data, 0o600)
}

// unwellFromFiles is laptopUnwell for a pulse answered outside the daemon:
// the first agent's reason, unless some watched workspace has another
// agent that can take its runs.
func unwellFromFiles(dir string, log *watch.FixLog, budget int, now time.Time) string {
	why := unwellFrom(log, budget, now)
	if why == "" {
		return ""
	}
	var specs map[string]watchAgents
	if raw, err := os.ReadFile(WatchAgentsPath(dir)); err != nil || json.Unmarshal(raw, &specs) != nil {
		return why
	}
	for ws, a := range specs {
		spec := WatchSpec{Workspace: ws, Agents: a.Agents, ConfigDir: a.ConfigDir}
		if len(spec.agents()) > 1 && hasFallback(spec, log, now) {
			return ""
		}
	}
	return why
}

// peerNotes is what the daemon remembers about its peers between ticks:
// who led each workspace, when that changed, what was rung. One per
// daemon, so a test daemon starts blank.
type peerNotes struct {
	mu            sync.Mutex
	logged        map[string]time.Time
	leaderWas     map[string]string
	leaderChanged map[string]time.Time
	rang          map[string]time.Time
}

func (d *Daemon) notes() *peerNotes {
	d.peerOnce.Do(func() {
		d.peer = &peerNotes{logged: map[string]time.Time{}, leaderWas: map[string]string{}, leaderChanged: map[string]time.Time{}, rang: map[string]time.Time{}}
	})
	return d.peer
}

// peerLeads is the name of the peer that leads this spec's trackers, or ""
// when this laptop acts. Logged once an hour per tracker so the log does
// not fill with the same line.
func (d *Daemon) peerLeads(spec WatchSpec) string {
	ids := spec.Identities()
	if len(ids) == 0 {
		return ""
	}
	store, err := peers.Load(peers.Path(d.Dir))
	if err != nil || len(store.Peers) == 0 {
		d.noteLeader(spec.Workspace, "")
		return ""
	}
	budget := freeBudget(spec.ConfigDir)
	var log *watch.FixLog
	if d.watchState != nil {
		log = d.watchState.Fixes
	}
	leader := peers.LeaderAmong(store, peers.Me(), ids, budget, laptopUnwell(spec, log, time.Now()), time.Now())
	if leader == "" {
		return ""
	}
	d.noteLeader(spec.Workspace, leader)
	return leader
}

// noteLeader logs a leader once an hour, and rings once when the lead
// moves - to a peer, or back to this laptop - so a traveller knows which
// machine is working the tracker now.
func (d *Daemon) noteLeader(workspace, leader string) {
	n := d.notes()
	n.mu.Lock()
	defer n.mu.Unlock()
	prev, seen := n.leaderWas[workspace]
	n.leaderWas[workspace] = leader
	if seen && prev != leader {
		who := leader
		if who == "" {
			who = "this laptop"
		}
		was := prev
		if was == "" {
			was = "this laptop"
		}
		// A lead that flaps (a laptop dozing on and off) is logged, not rung.
		if time.Since(n.leaderChanged[workspace]) > 15*time.Minute {
			go d.notifyAttention("corgi agent", who+" leads "+workspace+" now (was "+was+")", workspace)
		} else {
			utils.Infof("agent: %s leads %s now (was %s)\n", who, workspace, was)
		}
		n.leaderChanged[workspace] = time.Now()
	}
	if leader == "" {
		return
	}
	key := workspace + "|" + leader
	if last, ok := n.logged[key]; !ok || time.Since(last) > time.Hour {
		n.logged[key] = time.Now()
		utils.Infof("agent: %s leads %s - this laptop stays quiet there\n", leader, workspace)
	}
}

// pulsePeers runs for the daemon's life: once a minute, every peer hears
// what this laptop watches and whether it asks to lead.
func (d *Daemon) pulsePeers(ctx context.Context) {
	d.publishWatchIdentities()
	t := time.NewTicker(peers.PulseEvery)
	defer t.Stop()
	for {
		d.pulsePeersOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (d *Daemon) pulsePeersOnce(ctx context.Context) {
	path := peers.Path(d.Dir)
	store, err := peers.Load(path)
	if err != nil || len(store.Peers) == 0 {
		d.absorbPeers()
		return
	}
	defer d.absorbPeers()
	mine := LocalPulse(d.Dir, d.Version)
	mine.Watches = d.allWatchIdentities()
	var wg sync.WaitGroup
	for _, p := range store.Peers {
		p := p
		wg.Add(1)
		go func() {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			var heard peers.Pulse
			if err := peers.NewClient(p).Call(cctx, "/launch/peers/pulse", mine, &heard); err != nil {
				return
			}
			now := time.Now()
			_ = peers.Update(path, func(s *peers.Store) {
				for i := range s.Peers {
					if strings.EqualFold(s.Peers[i].Name, p.Name) {
						s.Peers[i].Absorb(heard, now)
					}
				}
			})
		}()
	}
	wg.Wait()
}

// ---- what a pulse carries, and what this laptop does with what it hears --

// LocalPulse is this laptop as its peers should see it, read from the
// files the daemon keeps - so the MCP server answers a pulse the same way
// the daemon sends one.
func LocalPulse(dir, version string) peers.Pulse {
	store, _ := peers.Load(peers.Path(dir))
	p := peers.Pulse{Name: peers.Me(), Version: version, Watches: []string{}, At: time.Now().UnixMilli(), Budget: freeBudget(""), Ignored: watch.IgnoredKeys(dir)}
	if store != nil {
		p.Lead = store.Lead
	}
	if raw, err := os.ReadFile(WatchIdentitiesPath(dir)); err == nil {
		_ = json.Unmarshal(raw, &p.Watches)
	}
	if raw, err := os.ReadFile(SessionsPath(dir)); err == nil {
		var st sessions.State
		if json.Unmarshal(raw, &st) == nil {
			p.Sessions = peerSessionsOf(st.Sessions)
		}
	}
	if until := MutedUntil(dir); !until.IsZero() {
		p.MutedUntil = until.UnixMilli()
	}
	log := watch.LoadFixLog(dir)
	p.Runs = peerRunsOf(log, time.Now())
	p.Unwell = unwellFromFiles(dir, log, p.Budget, time.Now())
	if p.Ignored == nil {
		p.Ignored = []string{}
	}
	return p
}

// freeBudget is the percent of the five-hour window still free for the
// account in configDir ("" = the default one); -1 when unknown.
func freeBudget(configDir string) int {
	lim, ok := usage.ReadLimits(configDir)
	if !ok || lim.FetchedAt.IsZero() || time.Since(lim.FetchedAt) > 2*time.Hour {
		return -1
	}
	free := 100 - lim.FiveHour.Percent
	if free < 1 {
		free = 1
	}
	return free
}

// unwellFrom says why this laptop cannot run fixes: its newest finished
// run of the day failed on a login or a permission and nothing has worked
// since, or the five-hour window is all but spent.
func unwellFrom(log *watch.FixLog, budget int, now time.Time) string {
	if budget >= 0 && budget <= 2 {
		return "limit"
	}
	if log == nil {
		return ""
	}
	records := log.Records()
	for i := len(records) - 1; i >= 0; i-- {
		r := records[i]
		if r.FinishedAt.IsZero() {
			continue
		}
		if now.Sub(r.FinishedAt) > 24*time.Hour {
			break
		}
		if watch.Blocking(r.Failure) {
			return r.Failure
		}
		return ""
	}
	return ""
}

func peerSessionsOf(list []sessions.Session) []peers.PeerSession {
	out := []peers.PeerSession{}
	for _, s := range list {
		if s.Status == sessions.StatusGone {
			continue
		}
		ps := peers.PeerSession{ID: s.ID, Label: s.Label, Display: s.Display, Status: string(s.Status), Ticket: s.Ticket, Branch: s.Branch, Detail: s.Detail, Agent: s.Agent, Since: s.StartedAt.UnixMilli()}
		if s.Pending != nil {
			ps.Pending = strings.TrimSpace(s.Pending.Tool + " " + s.Pending.Subject)
		}
		out = append(out, ps)
		if len(out) == 50 {
			break
		}
	}
	return out
}

// peerRunsOf is the last day of unattended runs: running, done, failed
// (with why) or blocked, newest first.
func peerRunsOf(log *watch.FixLog, now time.Time) []peers.PeerRun {
	out := []peers.PeerRun{}
	if log == nil {
		return out
	}
	blocks := log.AllBlocks()
	records := log.Records()
	for i := len(records) - 1; i >= 0 && len(out) < 50; i-- {
		r := records[i]
		at := r.FinishedAt
		if at.IsZero() {
			at = r.StartedAt
		}
		if now.Sub(at) > 24*time.Hour {
			continue
		}
		pr := peers.PeerRun{Ref: r.Ref, Workspace: r.Workspace, State: "done", At: at.UnixMilli()}
		switch {
		case r.FinishedAt.IsZero():
			pr.State = "running"
		case r.Failure != "" || r.Error != "":
			pr.State = "failed"
			pr.Reason = firstNonEmpty(r.Failure, r.Error)
		}
		if b, ok := blocks[r.Workspace+"/"+r.Ref]; ok {
			pr.State, pr.Reason = "blocked", b.Reason
		}
		out = append(out, pr)
	}
	return out
}

// absorbPeers is what this daemon does with what the peers last said:
// their boards go on this board, their ignored tickets are ignored here,
// a session on the same ticket or branch as one of theirs rings once, and
// a run that failed over there is logged once.
func (d *Daemon) absorbPeers() {
	store, err := peers.Load(peers.Path(d.Dir))
	if err != nil || len(store.Peers) == 0 {
		if d.Sessions != nil {
			d.Sessions.SetPeers(nil)
		}
		return
	}
	now := time.Now()
	boards := make([]sessions.PeerBoard, 0, len(store.Peers))
	for _, p := range store.Peers {
		boards = append(boards, sessions.PeerBoard{Name: p.Name, Alive: p.Alive(now), Lead: p.Lead, Budget: p.Budget, SeenAt: p.SeenAt, Sessions: p.Sessions, Runs: p.Runs})
		if !p.Alive(now) {
			continue
		}
		// A mute set over there quiets this laptop for the same span; a
		// longer mute here stays.
		if p.MutedUntil > 0 {
			if until := time.UnixMilli(p.MutedUntil); until.After(now) && until.After(MutedUntil(d.Dir).Add(time.Minute)) {
				_ = SetMute(d.Dir, until)
				utils.Infof("agent: muted until %s, as %s is\n", until.Local().Format("15:04"), p.Name)
			}
		}
		if d.watchState != nil {
			for _, key := range p.Ignored {
				if !d.watchState.IsIgnored(key) {
					_ = d.watchState.Ignore(key)
				}
			}
		}
		if p.Unwell != "" && d.rangOnce("unwell|"+p.Name+"|"+p.Unwell, now) {
			why := "its agent wants a login"
			if p.Unwell == "limit" {
				why = "its five-hour window is spent"
			}
			go d.notifyAttention("corgi agent", p.Name+" cannot run fixes ("+why+") - this laptop leads the trackers you share until it can", "")
		}
		for _, r := range p.Runs {
			if r.State != "failed" && r.State != "blocked" {
				continue
			}
			if d.rangOnce("run|"+p.Name+"|"+r.Ref+"|"+r.State+"|"+strconv.FormatInt(r.At, 10), now) {
				utils.Infof("agent: on %s, %s %s: %s\n", p.Name, r.Ref, r.State, r.Reason)
			}
		}
		d.ringPeerCrossings(p, now)
	}
	if d.Sessions != nil {
		d.Sessions.SetPeers(boards)
	}
}

// ringPeerCrossings rings once when a session here and one on the peer
// work the same ticket, or the same branch of the same repo.
func (d *Daemon) ringPeerCrossings(p peers.Peer, now time.Time) {
	if d.Sessions == nil {
		return
	}
	for _, mine := range d.Sessions.Sessions() {
		switch mine.Status {
		case sessions.StatusWorking, sessions.StatusNeedsInput, sessions.StatusDone:
		default:
			continue
		}
		for _, theirs := range p.Sessions {
			sameTicket := mine.Ticket != "" && strings.EqualFold(mine.Ticket, theirs.Ticket)
			sameBranch := mine.Branch != "" && mine.Branch == theirs.Branch && strings.EqualFold(mine.Label, theirs.Label) && !isMainBranch(mine.Branch)
			if !sameTicket && !sameBranch {
				continue
			}
			what := "ticket " + mine.Ticket
			if !sameTicket {
				what = "branch " + mine.Branch
			}
			if d.rangOnce("cross|"+mine.ID+"|"+p.Name+"|"+theirs.ID, now) {
				go d.notifySession(notifyTitlePrefix+label(mine), "crossing laptops: "+p.Name+" is also on "+what+" ("+firstNonEmpty(theirs.Display, theirs.Label)+", "+theirs.Status+")", mine)
			}
		}
	}
}

func isMainBranch(b string) bool {
	switch strings.ToLower(b) {
	case "main", "master", "develop", "dev", "trunk":
		return true
	}
	return false
}

// rangOnce is true the first time a key is seen (and again after a day).
func (d *Daemon) rangOnce(key string, now time.Time) bool {
	n := d.notes()
	n.mu.Lock()
	defer n.mu.Unlock()
	if at, ok := n.rang[key]; ok && now.Sub(at) < 24*time.Hour {
		return false
	}
	n.rang[key] = now
	if len(n.rang) > 5000 {
		for k, at := range n.rang {
			if now.Sub(at) > 24*time.Hour {
				delete(n.rang, k)
			}
		}
	}
	return true
}
