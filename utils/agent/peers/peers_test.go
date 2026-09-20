package peers

import (
	"testing"
	"time"
)

func TestLeaderPrefersLeadThenName(t *testing.T) {
	now := time.Now()
	s := &Store{Peers: []Peer{
		{Name: "zeta", SeenAt: now, Watches: []string{"linear/api"}},
		{Name: "alpha", SeenAt: now, Watches: []string{"linear/api"}},
		{Name: "old", SeenAt: now.Add(-Silence - time.Second), Watches: []string{"linear/api"}},
	}}
	if got := Leader(s, "mike", []string{"linear/api"}, now); got != "alpha" {
		t.Fatalf("first by name leads, got %q", got)
	}
	if got := Leader(s, "aaron", []string{"linear/api"}, now); got != "" {
		t.Fatalf("this laptop is first by name, got %q", got)
	}
	s.Peers[0].Lead = true
	if got := Leader(s, "aaron", []string{"linear/api"}, now); got != "zeta" {
		t.Fatalf("the peer that asked to lead wins, got %q", got)
	}
	s.Lead = true
	if got := Leader(s, "mike", []string{"linear/api"}, now); got != "" {
		t.Fatalf("both ask: first by name of those, got %q", got)
	}
	if got := Leader(s, "mike", []string{"github/other"}, now); got != "" {
		t.Fatalf("no peer watches this tracker, got %q", got)
	}
	if got := Leader(s, "mike", nil, now); got != "" {
		t.Fatalf("nothing shared, got %q", got)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	path := Path(t.TempDir())
	if err := Update(path, func(s *Store) { s.put(Peer{Name: "B", URL: "https://b", Token: "t", Key: "k"}) }); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil || len(s.Peers) != 1 {
		t.Fatalf("load: %v %+v", err, s)
	}
	if _, ok := s.Find("b"); !ok {
		t.Fatal("find is case-blind")
	}
	if !s.Remove("B") || len(s.Peers) != 0 {
		t.Fatal("remove")
	}
}
