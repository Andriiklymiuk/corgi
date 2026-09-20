package daemon

import (
	"context"
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
