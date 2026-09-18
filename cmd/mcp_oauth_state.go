package cmd

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"andriiklymiuk/corgi/utils/atomicfile"
)

const (
	oauthStateName    = "oauth.json"
	oauthStateVersion = 1
	maxOAuthClients   = 200
)

type oauthClient struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	RedirectURIs []string  `json:"redirectUris"`
	CreatedAt    time.Time `json:"createdAt"`
}

type rotatedHash struct {
	Hash string    `json:"hash"`
	At   time.Time `json:"at"`
}

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

type oauthState struct {
	Version  int           `json:"version"`
	Clients  []oauthClient `json:"clients"`
	Families []tokenFamily `json:"families"`
}

func oauthStatePath(agentDir string) string { return filepath.Join(agentDir, oauthStateName) }

func loadOAuthState(path string) (*oauthState, error) {
	st := &oauthState{}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return nil, err
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		return nil, fmt.Errorf("%s is readable by others (mode %04o); chmod 600 it", path, mode)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, st); err != nil {
		return nil, err
	}
	return st, nil
}

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

func randomToken(prefix string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b), nil
}
