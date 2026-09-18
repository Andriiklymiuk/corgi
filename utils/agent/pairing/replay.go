package pairing

import (
	"encoding/json"
	"sync"
	"time"
)

type ReplayGuard struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

func NewReplayGuard() *ReplayGuard { return &ReplayGuard{seen: map[string]time.Time{}} }

func (g *ReplayGuard) Seen(nonce string, now time.Time) bool {
	if nonce == "" {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for n, at := range g.seen {
		if now.Sub(at) > 2*e2eSkew {
			delete(g.seen, n)
		}
	}
	if _, ok := g.seen[nonce]; ok {
		return true
	}
	g.seen[nonce] = now
	return false
}

func EnvelopeNonce(envelope []byte) string {
	var env Envelope
	if err := json.Unmarshal(envelope, &env); err != nil {
		return ""
	}
	return env.N
}
