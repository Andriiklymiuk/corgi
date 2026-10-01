// Package sendgate is the one door every outbound chat message goes through.
// It counts attempts, not successes, so a caller that retries a failing send
// spends its budget instead of hammering the API: whatever loops upstream, a
// destination never gets more than the caps below.
package sendgate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils/atomicfile"
)

var ErrThrottled = errors.New("send held back")

type Limits struct {
	PerTarget       int
	PerFamily       int
	Window          time.Duration
	PerDay          int
	DuplicateWithin time.Duration
	MinPause        time.Duration
	MaxPause        time.Duration
}

var Default = Limits{
	PerTarget:       20,
	PerFamily:       60,
	Window:          time.Hour,
	PerDay:          300,
	DuplicateWithin: time.Hour,
	MinPause:        time.Minute,
	MaxPause:        time.Hour,
}

// ReadLimits meter what corgi asks Slack, not what it says: 300 calls a day
// in all, no more than 25 in any hour so a day's budget is not gone by
// breakfast.
var ReadLimits = Limits{
	PerFamily: 25,
	Window:    time.Hour,
	PerDay:    300,
	MinPause:  time.Minute,
	MaxPause:  time.Hour,
}

type send struct {
	At     time.Time `json:"at"`
	Family string    `json:"family"`
	Target string    `json:"target"`
	Hash   string    `json:"hash"`
}

type ledger struct {
	Sends       []send               `json:"sends"`
	PausedUntil map[string]time.Time `json:"pausedUntil,omitempty"`
}

// Gate keeps its ledger in a file when it has a path, so the daemon and every
// `corgi agent chat` a run starts share one budget; without one it is per process.
type Gate struct {
	Limits Limits
	Now    func() time.Time

	path string
	mu   sync.Mutex
	mem  ledger
}

func New(path string) *Gate { return &Gate{Limits: Default, path: path} }

func InMemory() *Gate { return &Gate{Limits: Default} }

func PathIn(agentDir string) string { return filepath.Join(agentDir, "watch", "sends.json") }

var (
	sharedMu sync.Mutex
	shared   = map[string]*Gate{}
)

// ReadsFor is the read budget kept in the same ledger as the sends.
func ReadsFor(agentDir string) *Gate {
	if agentDir == "" {
		g := InMemory()
		g.Limits = ReadLimits
		return g
	}
	path := PathIn(agentDir)
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if g, ok := shared[path+"#reads"]; ok {
		return g
	}
	g := New(path)
	g.Limits = ReadLimits
	shared[path+"#reads"] = g
	return g
}

// For hands out one gate per ledger path, so callers in one process also share
// the in-process lock.
func For(agentDir string) *Gate {
	if agentDir == "" {
		return InMemory()
	}
	path := PathIn(agentDir)
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if g, ok := shared[path]; ok {
		return g
	}
	g := New(path)
	shared[path] = g
	return g
}

func (g *Gate) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

// Allow reserves one send of text to target, or says why not. The send is
// counted whether or not it then reaches the other side.
func (g *Gate) Allow(family, target, text string) error {
	return g.update(func(l *ledger, now time.Time) error {
		lim := g.Limits
		if until, ok := l.PausedUntil[family]; ok && now.Before(until) {
			return fmt.Errorf("%w: %s is paused until %s after it pushed back", ErrThrottled, family, until.Format(time.Kitchen))
		}
		hash := digest(family, target, text)
		perTarget, perFamily, perDay := 0, 0, 0
		for _, s := range l.Sends {
			if s.Family != family {
				continue
			}
			age := now.Sub(s.At)
			if s.Hash == hash && age < lim.DuplicateWithin {
				return fmt.Errorf("%w: the same message went to %s %s ago", ErrThrottled, target, age.Round(time.Second))
			}
			if age < lim.Window {
				perFamily++
				if s.Target == target {
					perTarget++
				}
			}
			if age < 24*time.Hour {
				perDay++
			}
		}
		switch {
		case lim.PerTarget > 0 && perTarget >= lim.PerTarget:
			return fmt.Errorf("%w: %d sends to %s in the last %s", ErrThrottled, perTarget, target, lim.Window)
		case lim.PerFamily > 0 && perFamily >= lim.PerFamily:
			return fmt.Errorf("%w: %d %s sends in the last %s", ErrThrottled, perFamily, family, lim.Window)
		case lim.PerDay > 0 && perDay >= lim.PerDay:
			return fmt.Errorf("%w: %d %s sends today", ErrThrottled, perDay, family)
		}
		l.Sends = append(l.Sends, send{At: now, Family: family, Target: target, Hash: hash})
		return nil
	})
}

// Pause stops a whole family, as when the API answered 429; the wait is
// clamped so a missing or absurd Retry-After neither skips nor freezes it.
func (g *Gate) Pause(family string, wait time.Duration) {
	_ = g.update(func(l *ledger, now time.Time) error {
		if wait < g.Limits.MinPause {
			wait = g.Limits.MinPause
		}
		if g.Limits.MaxPause > 0 && wait > g.Limits.MaxPause {
			wait = g.Limits.MaxPause
		}
		until := now.Add(wait)
		if l.PausedUntil == nil {
			l.PausedUntil = map[string]time.Time{}
		}
		if until.After(l.PausedUntil[family]) {
			l.PausedUntil[family] = until
		}
		return nil
	})
}

func digest(family, target, text string) string {
	sum := sha256.Sum256([]byte(family + "\x00" + target + "\x00" + text))
	return hex.EncodeToString(sum[:12])
}

func (g *Gate) update(fn func(*ledger, time.Time) error) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	if g.path == "" {
		prune(&g.mem, now)
		return fn(&g.mem, now)
	}
	unlock, err := lockFile(g.path + ".lock")
	if err != nil {
		// Fail closed: a message lost is cheaper than an unmetered one.
		return fmt.Errorf("%w: send ledger busy: %v", ErrThrottled, err)
	}
	defer unlock()
	var l ledger
	if data, err := os.ReadFile(g.path); err == nil {
		_ = json.Unmarshal(data, &l)
	}
	prune(&l, now)
	ferr := fn(&l, now)
	data, err := json.Marshal(l)
	if err == nil {
		err = atomicfile.Write(g.path, data, 0o600)
	}
	if err != nil && ferr == nil {
		return fmt.Errorf("%w: could not record the send: %v", ErrThrottled, err)
	}
	return ferr
}

func prune(l *ledger, now time.Time) {
	kept := l.Sends[:0]
	for _, s := range l.Sends {
		if now.Sub(s.At) < 24*time.Hour {
			kept = append(kept, s)
		}
	}
	l.Sends = kept
	for k, until := range l.PausedUntil {
		if !now.Before(until) {
			delete(l.PausedUntil, k)
		}
	}
}

const lockStale = 30 * time.Second

func lockFile(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = f.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		if info, serr := os.Stat(path); serr == nil && time.Since(info.ModTime()) > lockStale {
			_ = os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, errors.New("lock held")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
