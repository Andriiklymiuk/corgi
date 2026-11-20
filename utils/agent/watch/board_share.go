package watch

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils/atomicfile"
)

const (
	maxBoardShares     = 20
	maxBoardShareLabel = 60
)

type BoardShare struct {
	ID        string    `json:"id"`
	KeyHash   string    `json:"keyHash"`
	Workspace string    `json:"workspace,omitempty"`
	Label     string    `json:"label,omitempty"`
	Write     bool      `json:"write,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	Expires   time.Time `json:"expires,omitzero"`
}

func (s BoardShare) Open(now time.Time) bool {
	return s.Expires.IsZero() || now.Before(s.Expires)
}

type BoardShares struct {
	mu     sync.Mutex
	path   string
	Shares []BoardShare `json:"shares"`
}

func LoadBoardShares(agentDir string) *BoardShares {
	l := &BoardShares{path: filepath.Join(agentDir, "watch", "board_shares.json")}
	if data, err := os.ReadFile(l.path); err == nil {
		_ = json.Unmarshal(data, l)
	}
	return l
}

func (l *BoardShares) save() error {
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(l.path, data, 0o600)
}

func boardKeyHash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func (l *BoardShares) Add(workspace, label string, lasts time.Duration, write bool, now time.Time) (BoardShare, string, error) {
	label = strings.TrimSpace(label)
	if len(label) > maxBoardShareLabel {
		return BoardShare{}, "", fmt.Errorf("a label is under %d characters", maxBoardShareLabel)
	}
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return BoardShare{}, "", fmt.Errorf("could not make a key: %w", err)
	}
	key := hex.EncodeToString(raw)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.dropExpired(now)
	if len(l.Shares) >= maxBoardShares {
		return BoardShare{}, "", fmt.Errorf("%d board links are open already - close one first", maxBoardShares)
	}
	share := BoardShare{ID: key[:8], KeyHash: boardKeyHash(key[8:]), Workspace: strings.TrimSpace(workspace), Label: label, Write: write, CreatedAt: now}
	if lasts > 0 {
		share.Expires = now.Add(lasts)
	}
	l.Shares = append(l.Shares, share)
	return share, key, l.save()
}

func (l *BoardShares) dropExpired(now time.Time) {
	kept := l.Shares[:0]
	for _, s := range l.Shares {
		if s.Open(now) {
			kept = append(kept, s)
		}
	}
	l.Shares = kept
}

func (l *BoardShares) Find(key string, now time.Time) (BoardShare, bool) {
	if len(key) != 48 {
		return BoardShare{}, false
	}
	id, hash := key[:8], boardKeyHash(key[8:])
	l.mu.Lock()
	defer l.mu.Unlock()
	var found BoardShare
	ok := false
	for _, s := range l.Shares {
		if s.ID == id && subtle.ConstantTimeCompare([]byte(s.KeyHash), []byte(hash)) == 1 && s.Open(now) {
			found, ok = s, true
		}
	}
	return found, ok
}

func (l *BoardShares) Close(id string) (int, error) {
	id = strings.TrimSpace(id)
	l.mu.Lock()
	defer l.mu.Unlock()
	kept := l.Shares[:0]
	closed := 0
	for _, s := range l.Shares {
		if id == "all" || s.ID == id {
			closed++
			continue
		}
		kept = append(kept, s)
	}
	l.Shares = kept
	if closed == 0 {
		return 0, fmt.Errorf("no open board link %q", id)
	}
	return closed, l.save()
}

func (l *BoardShares) List(now time.Time) []BoardShare {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := []BoardShare{}
	for _, s := range l.Shares {
		if s.Open(now) {
			out = append(out, s)
		}
	}
	return out
}
