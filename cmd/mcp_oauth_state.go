package cmd

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"andriiklymiuk/corgi/utils/atomicfile"
)

const (
	oauthStateName    = "oauth.json"
	oauthStateVersion = 1
	// maxOAuthClients bounds the DCR store: Claude registers a client per
	// connection and never unregisters.
	maxOAuthClients = 200
)

// oauthClient is one registered client, from DCR or a CIMD document.
type oauthClient struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	RedirectURIs []string  `json:"redirectUris"`
	CreatedAt    time.Time `json:"createdAt"`
}

// rotatedHash is a refresh token that was exchanged; kept a day so a replay
// of it is recognised as theft.
type rotatedHash struct {
	Hash string    `json:"hash"`
	At   time.Time `json:"at"`
}

// tokenFamily is one grant: the refresh token that is current, the ones it
// replaced, and the access tokens it issued (hashes only).
type tokenFamily struct {
	ID            string        `json:"id"`
	ClientID      string        `json:"clientId"`
	ClientName    string        `json:"clientName"`
	RefreshHash   string        `json:"refreshHash"`
	RotatedHashes []rotatedHash `json:"rotatedHashes,omitempty"`
	AccessHashes  []string      `json:"accessHashes,omitempty"`
	CreatedAt     time.Time     `json:"createdAt"`
	ExpiresAt     time.Time     `json:"expiresAt"`
}

// oauthState is what oauth.json holds.
type oauthState struct {
	Version  int           `json:"version"`
	Clients  []oauthClient `json:"clients"`
	Families []tokenFamily `json:"families"`
}

func oauthStatePath(agentDir string) string { return filepath.Join(agentDir, oauthStateName) }

// loadOAuthState reads the file; missing is empty.
func loadOAuthState(path string) (*oauthState, error) {
	st := &oauthState{}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, st); err != nil {
		return nil, err
	}
	return st, nil
}

// saveOAuthState writes the file atomically, owner-only.
func saveOAuthState(path string, st *oauthState) error {
	st.Version = oauthStateVersion
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(path, data, 0o600)
}

func (st *oauthState) findClient(id string) (oauthClient, bool) {
	for _, c := range st.Clients {
		if c.ID == id {
			return c, true
		}
	}
	return oauthClient{}, false
}

// addClient stores c and, past the cap, evicts the oldest clients that have
// no live family.
func (st *oauthState) addClient(c oauthClient, now time.Time) {
	st.Clients = append(st.Clients, c)
	if len(st.Clients) <= maxOAuthClients {
		return
	}
	live := map[string]bool{}
	for _, f := range st.Families {
		if now.Before(f.ExpiresAt) {
			live[f.ClientID] = true
		}
	}
	sort.SliceStable(st.Clients, func(i, j int) bool { return st.Clients[i].CreatedAt.Before(st.Clients[j].CreatedAt) })
	kept := st.Clients[:0]
	excess := len(st.Clients) - maxOAuthClients
	for _, c := range st.Clients {
		if excess > 0 && !live[c.ID] {
			excess--
			continue
		}
		kept = append(kept, c)
	}
	st.Clients = kept
}

// randomToken is prefix plus 32 random bytes, base64url without padding.
func randomToken(prefix string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b), nil
}
