package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/peers"
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
}

var peerLogMu sync.Mutex
var peerLogged = map[string]time.Time{}

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
		return ""
	}
	leader := peers.Leader(store, peers.Me(), ids, time.Now())
	if leader == "" {
		return ""
	}
	key := spec.Workspace + "|" + leader
	peerLogMu.Lock()
	last, ok := peerLogged[key]
	if !ok || time.Since(last) > time.Hour {
		peerLogged[key] = time.Now()
		utils.Infof("agent: %s leads %s — this laptop stays quiet there\n", leader, spec.Workspace)
	}
	peerLogMu.Unlock()
	return leader
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
		return
	}
	mine := peers.Pulse{Name: peers.Me(), Version: d.Version, Watches: d.allWatchIdentities(), Lead: store.Lead, At: time.Now().UnixMilli()}
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
						s.Peers[i].SeenAt, s.Peers[i].Watches, s.Peers[i].Lead, s.Peers[i].Version = now, heard.Watches, heard.Lead, heard.Version
					}
				}
			})
		}()
	}
	wg.Wait()
}
