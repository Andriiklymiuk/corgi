package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/peers"
	"andriiklymiuk/corgi/utils/agent/watch"
)

type namedSource struct{ name string }

func (n namedSource) Name() string { return n.name }
func (n namedSource) Poll(context.Context, watch.Cursor) ([]watch.Event, watch.Cursor, error) {
	return nil, nil, nil
}

func TestWatchIdentitiesAndPeerGate(t *testing.T) {
	spec := WatchSpec{Workspace: "api", Project: "ENG", Repos: []string{"acme/api", "acme/web"}, Sources: []watch.Source{namedSource{"linear"}, namedSource{"github"}}}
	ids := spec.Identities()
	want := []string{"github/acme/api", "github/acme/web", "github/eng", "linear/acme/api", "linear/acme/web", "linear/eng"}
	if len(ids) != len(want) {
		t.Fatalf("%v", ids)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("%v", ids)
		}
	}
	d := New("test", t.TempDir())
	if d.peerLeads(spec) != "" {
		t.Fatal("no peers: this laptop acts")
	}
	_ = peers.Update(peers.Path(d.Dir), func(s *peers.Store) {
		s.Peers = []peers.Peer{{Name: "AAA-home", SeenAt: time.Now(), Watches: []string{"linear/eng"}}}
	})
	if got := d.peerLeads(spec); got != "AAA-home" {
		t.Fatalf("a live peer first by name leads: %q", got)
	}
	_ = peers.Update(peers.Path(d.Dir), func(s *peers.Store) { s.Peers[0].SeenAt = time.Now().Add(-peers.Silence) })
	if d.peerLeads(spec) != "" {
		t.Fatal("a silent peer no longer leads")
	}
	_ = peers.Update(peers.Path(d.Dir), func(s *peers.Store) {
		s.Peers[0].SeenAt = time.Now()
		s.Lead = true
	})
	if d.peerLeads(spec) != "" {
		t.Fatal("asking to lead wins over the name")
	}
	if d.peerLeads(WatchSpec{Workspace: "docs"}) != "" {
		t.Fatal("a workspace with no tracker is nobody's to share")
	}
}

func TestLocalPulseAndAbsorb(t *testing.T) {
	d := New("test", t.TempDir())
	log := &watch.FixLog{Started: []watch.FixRecord{
		{Key: "k1", Workspace: "api", Ref: "ENG-1", StartedAt: time.Now().Add(-2 * time.Hour), FinishedAt: time.Now().Add(-time.Hour), Failure: watch.FailureNoAuth, Error: "please run /login"},
		{Key: "k2", Workspace: "api", Ref: "ENG-2", StartedAt: time.Now().Add(-10 * time.Minute)},
	}}
	runs := peerRunsOf(log, time.Now())
	p := LocalPulse(d.Dir, "test")
	p.Runs, p.Unwell = runs, unwellFrom(log, p.Budget, time.Now())
	if p.Name == "" || len(p.Runs) != 2 || p.Runs[0].State != "running" || p.Runs[1].State != "failed" || p.Runs[1].Reason == "" {
		t.Fatalf("%+v", p.Runs)
	}
	if p.Unwell != watch.FailureNoAuth {
		t.Fatalf("the newest finished run wanted a login: %q", p.Unwell)
	}
	// A peer that ignored a ticket: ignored here too; its board lands on ours.
	_ = peers.Update(peers.Path(d.Dir), func(s *peers.Store) {
		s.Peers = []peers.Peer{{Name: "home", SeenAt: time.Now(), Ignored: []string{"linear:ENG-9"}, Sessions: []peers.PeerSession{{ID: "x", Label: "api", Status: "working", Ticket: "ENG-3"}}}}
	})
	d.watchState = watch.LoadState(d.Dir)
	d.absorbPeers()
	if !d.watchState.IsIgnored("linear:ENG-9") {
		t.Fatal("a peer's ignore is ours")
	}
	st := d.Sessions.Snapshot(time.Now())
	if len(st.Peers) != 1 || st.Peers[0].Name != "home" || len(st.Peers[0].Sessions) != 1 || !st.Peers[0].Alive {
		t.Fatalf("board carries the peer: %+v", st.Peers)
	}
	raw, _ := json.Marshal(st)
	if strings.Contains(string(raw), "token") {
		t.Fatal("no secrets on the board")
	}
}

func TestLeadChangeRingsOnceAndMuteFollowsThePeer(t *testing.T) {
	d := New("test", t.TempDir())
	var mu sync.Mutex
	var rang []string
	d.Notify = func(title, body string) { mu.Lock(); rang = append(rang, body); mu.Unlock() }
	count := func() int { mu.Lock(); defer mu.Unlock(); return len(rang) }
	waitFor := func(n int) {
		for i := 0; i < 100 && count() < n; i++ {
			time.Sleep(20 * time.Millisecond)
		}
	}
	spec := WatchSpec{Workspace: "api", Project: "ENG", Sources: []watch.Source{namedSource{"linear"}}}
	d.peerLeads(spec) // first look: nothing to compare with
	_ = peers.Update(peers.Path(d.Dir), func(s *peers.Store) {
		s.Peers = []peers.Peer{{Name: "aaa-home", SeenAt: time.Now(), Watches: []string{"linear/eng"}, MutedUntil: time.Now().Add(30 * time.Minute).UnixMilli()}}
	})
	if d.peerLeads(spec) != "aaa-home" {
		t.Fatal("home leads")
	}
	waitFor(1)
	mu.Lock()
	if len(rang) != 1 || !strings.Contains(rang[0], "aaa-home leads api now") {
		t.Fatalf("one ring for the hand-over: %v", rang)
	}
	mu.Unlock()
	d.peerLeads(spec)
	time.Sleep(50 * time.Millisecond)
	if count() != 1 {
		t.Fatal("no ring while nothing changes")
	}
	d.absorbPeers()
	if MutedUntil(d.Dir).IsZero() {
		t.Fatal("the peer's mute is ours too")
	}
}
