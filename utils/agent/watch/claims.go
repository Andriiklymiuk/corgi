package watch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils/atomicfile"
)

// A claim is a session saying "these files are mine for now" — the
// auth middleware while it rewrites the refresh path — so a second
// session in the same repository is told at its start, and the daemon
// rings when one edits a file another claimed. Advisory: nothing is
// locked, nobody is stopped. A claim lives until it is released, its
// session leaves the board, or a day passes.

// FileClaim is one file held by one session.
type FileClaim struct {
	// Repo is the repository root the path is relative to.
	Repo    string    `json:"repo"`
	Path    string    `json:"path"`
	Session string    `json:"session"`
	Label   string    `json:"label,omitempty"`
	At      time.Time `json:"at"`
}

// FileClaimFor is how long a claim holds with nobody releasing it.
const FileClaimFor = 24 * time.Hour

type FileClaimLog struct {
	mu     sync.Mutex
	path   string
	Claims []FileClaim `json:"claims,omitempty"`
}

func claimsPath(agentDir string) string { return filepath.Join(agentDir, "watch", "claims.json") }

func LoadFileClaims(agentDir string) *FileClaimLog {
	l := &FileClaimLog{path: claimsPath(agentDir)}
	if data, err := os.ReadFile(l.path); err == nil {
		_ = json.Unmarshal(data, l)
	}
	return l
}

func (l *FileClaimLog) save() error {
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	return atomicfile.Write(l.path, data, 0o600)
}

// Set claims paths for a session in a repository; a path another session
// held is taken over, and the caller is told whose it was.
func (l *FileClaimLog) Set(repo, session, label string, paths []string, now time.Time) (taken []FileClaim, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	kept := l.Claims[:0]
	for _, c := range l.Claims {
		if c.Repo == repo && containsPath(paths, c.Path) {
			if c.Session != session {
				taken = append(taken, c)
			}
			continue
		}
		kept = append(kept, c)
	}
	l.Claims = kept
	for _, p := range paths {
		l.Claims = append(l.Claims, FileClaim{Repo: repo, Path: p, Session: session, Label: label, At: now})
	}
	return taken, l.save()
}

// Release drops a session's claims — the paths named, or all of them.
func (l *FileClaimLog) Release(session string, paths []string) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	kept := l.Claims[:0]
	dropped := 0
	for _, c := range l.Claims {
		if c.Session == session && (len(paths) == 0 || containsPath(paths, c.Path)) {
			dropped++
			continue
		}
		kept = append(kept, c)
	}
	l.Claims = kept
	if dropped == 0 {
		return 0, nil
	}
	return dropped, l.save()
}

// Live is every claim still holding at now, held by a session in live —
// the rest are forgotten on the way.
func (l *FileClaimLog) Live(live map[string]bool, now time.Time) []FileClaim {
	l.mu.Lock()
	defer l.mu.Unlock()
	kept := l.Claims[:0]
	for _, c := range l.Claims {
		if now.Sub(c.At) <= FileClaimFor && (live == nil || live[c.Session]) {
			kept = append(kept, c)
		}
	}
	if len(kept) != len(l.Claims) {
		l.Claims = kept
		_ = l.save()
	}
	out := append([]FileClaim(nil), l.Claims...)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// Crossed is which of a session's touched paths another session claims,
// in the same repository.
func Crossed(claims []FileClaim, repo, session string, touched []string) []FileClaim {
	var out []FileClaim
	for _, c := range claims {
		if c.Repo != repo || c.Session == session {
			continue
		}
		if containsPath(touched, c.Path) {
			out = append(out, c)
		}
	}
	return out
}

func containsPath(list []string, p string) bool {
	p = filepath.ToSlash(strings.TrimPrefix(p, "./"))
	for _, x := range list {
		if filepath.ToSlash(strings.TrimPrefix(x, "./")) == p {
			return true
		}
	}
	return false
}
