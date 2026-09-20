// Package peers lets two or more laptops that watch the same trackers agree
// on which one acts. Each laptop pairs with the other exactly the way a
// phone pairs with it — a device token and an end-to-end key — and the
// daemons pulse each other once a minute. For every tracker two laptops
// share, one of them leads: it starts the fixes and rings the phone; the
// other stays quiet and takes over when the leader goes silent.
package peers

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils/agent/pairing"
	"andriiklymiuk/corgi/utils/atomicfile"
)

const (
	// Role is what a peer laptop is in the other laptop's devices.json.
	Role = "peer"
	// PulseEvery is how often a daemon calls each peer.
	PulseEvery = time.Minute
	// Silence is how long without a pulse before a peer no longer counts.
	Silence = 3 * time.Minute
)

// Peer is another laptop this one pairs with.
type Peer struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	// Token is what this laptop presents to the peer; Key is the shared
	// end-to-end key, base64. Both came out of pairing with the peer.
	Token string    `json:"token"`
	Key   string    `json:"key"`
	Since time.Time `json:"since"`
	// What the last pulse said about the peer.
	SeenAt  time.Time `json:"seenAt,omitempty"`
	Watches []string  `json:"watches,omitempty"`
	Lead    bool      `json:"lead,omitempty"`
	Version string    `json:"version,omitempty"`
	// Budget is the percent of the five-hour window still free there; -1 unknown.
	Budget int `json:"budget,omitempty"`
	// Unwell is why the peer cannot run fixes right now ("no-credential":
	// its agent wants a login, "limit": its window is spent); "" when fine.
	Unwell   string        `json:"unwell,omitempty"`
	Sessions []PeerSession `json:"sessions,omitempty"`
	Runs     []PeerRun     `json:"runs,omitempty"`
	Ignored  []string      `json:"ignored,omitempty"`
}

// PeerSession is one row of the other laptop's board, enough to show it
// and to notice two laptops on the same ticket or branch.
type PeerSession struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Display string `json:"display,omitempty"`
	Status  string `json:"status"`
	Ticket  string `json:"ticket,omitempty"`
	Branch  string `json:"branch,omitempty"`
	Pending string `json:"pending,omitempty"`
	Detail  string `json:"detail,omitempty"`
	Agent   string `json:"agent,omitempty"`
	Since   int64  `json:"since,omitempty"`
}

// PeerRun is an unattended run the other laptop started lately: running,
// done, failed or blocked, so this laptop knows what broke over there.
type PeerRun struct {
	Ref       string `json:"ref"`
	Workspace string `json:"workspace"`
	State     string `json:"state"`
	Reason    string `json:"reason,omitempty"`
	At        int64  `json:"at"`
}

// Alive is whether the peer pulsed recently enough to count.
func (p Peer) Alive(now time.Time) bool { return !p.SeenAt.IsZero() && now.Sub(p.SeenAt) < Silence }

// Store is peers.json: the peers, and whether this laptop asks to lead.
type Store struct {
	Version int    `json:"version"`
	Lead    bool   `json:"lead,omitempty"`
	Peers   []Peer `json:"peers"`
}

func Path(agentDir string) string { return filepath.Join(agentDir, "peers.json") }

var mu sync.Mutex

func Load(path string) (*Store, error) {
	mu.Lock()
	defer mu.Unlock()
	return load(path)
}

func load(path string) (*Store, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Store{Version: 1}, nil
	}
	if err != nil {
		return nil, err
	}
	var s Store
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if s.Version == 0 {
		s.Version = 1
	}
	return &s, nil
}

func Save(path string, s *Store) error {
	mu.Lock()
	defer mu.Unlock()
	return save(path, s)
}

func save(path string, s *Store) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(path, data, 0o600)
}

// Update loads, edits and saves under one lock, so a pulse landing while a
// join writes does not lose either.
func Update(path string, edit func(*Store)) error {
	mu.Lock()
	defer mu.Unlock()
	s, err := load(path)
	if err != nil {
		return err
	}
	edit(s)
	return save(path, s)
}

func (s *Store) Find(name string) (Peer, bool) {
	for _, p := range s.Peers {
		if strings.EqualFold(p.Name, name) {
			return p, true
		}
	}
	return Peer{}, false
}

func (s *Store) put(p Peer) {
	for i, o := range s.Peers {
		if strings.EqualFold(o.Name, p.Name) {
			s.Peers[i] = p
			return
		}
	}
	s.Peers = append(s.Peers, p)
	sort.Slice(s.Peers, func(i, j int) bool { return s.Peers[i].Name < s.Peers[j].Name })
}

func (s *Store) Remove(name string) bool {
	for i, o := range s.Peers {
		if strings.EqualFold(o.Name, name) {
			s.Peers = append(s.Peers[:i], s.Peers[i+1:]...)
			return true
		}
	}
	return false
}

// Me is the name this laptop goes by among its peers.
func Me() string {
	host, _ := os.Hostname()
	host = strings.TrimSpace(host)
	if host == "" {
		return "a-laptop"
	}
	return host
}

// Leader picks, for a set of tracker identities, which laptop acts: among
// this one and every live peer that watches any of them, the one that asked
// to lead, else the first by name. An empty answer means nobody else
// competes and this laptop acts.
func Leader(s *Store, me string, mine []string, now time.Time) string {
	return LeaderWithBudget(s, me, mine, -1, now)
}

// LeaderWithBudget is Leader with this laptop's own free budget in hand.
func LeaderWithBudget(s *Store, me string, mine []string, myBudget int, now time.Time) string {
	return LeaderAmong(s, me, mine, myBudget, "", now)
}

// LeaderAmong is the full rule: a laptop that cannot work right now (its
// agent wants a login, its window is spent) never leads while another can,
// however it was marked — a laptop alone in a room for weeks must not
// hold the lead with an expired login. myUnwell is this laptop's own state.
func LeaderAmong(s *Store, me string, mine []string, myBudget int, myUnwell string, now time.Time) string {
	type cand struct {
		name   string
		lead   bool
		budget int
		unwell bool
	}
	cands := []cand{{me, s.Lead, myBudget, myUnwell != ""}}
	for _, p := range s.Peers {
		if !p.Alive(now) || !shares(p.Watches, mine) {
			continue
		}
		cands = append(cands, cand{p.Name, p.Lead, p.Budget, p.Unwell != ""})
	}
	if len(cands) == 1 {
		return ""
	}
	// Able first; then asked to lead; else the one with clearly more of its
	// five-hour window left (a ten-point gap, so the lead does not flap);
	// else by name.
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].unwell != cands[j].unwell {
			return !cands[i].unwell
		}
		if cands[i].lead != cands[j].lead {
			return cands[i].lead
		}
		bi, bj := cands[i].budget, cands[j].budget
		if bi > 0 && bj > 0 && abs(bi-bj) >= 10 {
			return bi > bj
		}
		return strings.ToLower(cands[i].name) < strings.ToLower(cands[j].name)
	})
	if strings.EqualFold(cands[0].name, me) {
		return ""
	}
	return cands[0].name
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func shares(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if strings.EqualFold(x, y) {
				return true
			}
		}
	}
	return false
}

// Pulse is what one daemon tells another once a minute, and what it hears back.
type Pulse struct {
	Name    string   `json:"name"`
	Version string   `json:"version,omitempty"`
	Watches []string `json:"watches"`
	Lead    bool     `json:"lead,omitempty"`
	At      int64    `json:"at"`
	// Since 2.29.1: the board, the runs, the ignored tickets and the
	// budget travel too; an older peer sends none and that is fine.
	Budget   int           `json:"budget,omitempty"`
	Unwell   string        `json:"unwell,omitempty"`
	Sessions []PeerSession `json:"sessions,omitempty"`
	Runs     []PeerRun     `json:"runs,omitempty"`
	Ignored  []string      `json:"ignored,omitempty"`
}

// Absorb records what a pulse said about the peer that sent it.
func (p *Peer) Absorb(in Pulse, now time.Time) {
	p.SeenAt, p.Watches, p.Lead, p.Version = now, in.Watches, in.Lead, in.Version
	p.Budget, p.Unwell, p.Sessions, p.Runs, p.Ignored = in.Budget, in.Unwell, in.Sessions, in.Runs, in.Ignored
	if len(p.Sessions) > 50 {
		p.Sessions = p.Sessions[:50]
	}
	if len(p.Runs) > 50 {
		p.Runs = p.Runs[:50]
	}
	if len(p.Ignored) > 500 {
		p.Ignored = p.Ignored[:500]
	}
}

// Client talks to one peer as a paired device: bearer token, sealed body.
type Client struct {
	Peer Peer
	HTTP *http.Client
}

func NewClient(p Peer) *Client {
	return &Client{Peer: p, HTTP: &http.Client{Timeout: 15 * time.Second}}
}

func (c *Client) key() ([]byte, error) {
	k, err := base64.StdEncoding.DecodeString(c.Peer.Key)
	if err != nil || len(k) != 32 {
		return nil, fmt.Errorf("peer %s has no usable key; pair again", c.Peer.Name)
	}
	return k, nil
}

// Call POSTs a JSON body to the peer's launch endpoint, sealed, and opens the answer.
func (c *Client) Call(ctx context.Context, path string, body any, out any) error {
	key, err := c.key()
	if err != nil {
		return err
	}
	plain, err := json.Marshal(body)
	if err != nil {
		return err
	}
	sealed, err := pairing.Seal(key, http.MethodPost, path, plain, time.Now())
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.Peer.URL, "/")+path, bytes.NewReader(sealed))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Peer.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(pairing.E2EHeader, "1")
	req.Header.Set("ngrok-skip-browser-warning", "1")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return err
	}
	if res.Header.Get(pairing.E2EHeader) == "" {
		if res.StatusCode >= 300 {
			return fmt.Errorf("%s answered %d", c.Peer.Name, res.StatusCode)
		}
		return fmt.Errorf("%s answered in plaintext", c.Peer.Name)
	}
	opened, err := pairing.Open(key, http.MethodPost, path, raw, time.Now())
	if err != nil {
		return err
	}
	if res.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(opened, &e)
		if e.Error == "" {
			e.Error = fmt.Sprintf("%d", res.StatusCode)
		}
		return fmt.Errorf("%s: %s", c.Peer.Name, e.Error)
	}
	if out != nil {
		return json.Unmarshal(opened, out)
	}
	return nil
}

// Join pairs this laptop with the peer at url using a one-time code the
// peer minted, the way a phone pairs: this laptop's static key is its
// device key, so the shared secret is the same one both ends can derive
// again. What comes back is stored as a peer.
func Join(ctx context.Context, agentDir, rawURL, code string) (Peer, error) {
	base, err := cleanURL(rawURL)
	if err != nil {
		return Peer{}, err
	}
	priv, err := pairing.LoadOrCreateServerKey(pairing.ServerKeyPath(agentDir))
	if err != nil {
		return Peer{}, err
	}
	body, _ := json.Marshal(map[string]string{"code": pairing.NormalizeCode(code), "device": Me(), "pubKey": pairing.PublicKeyString(priv.PublicKey())})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/pair", bytes.NewReader(body))
	if err != nil {
		return Peer{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ngrok-skip-browser-warning", "1")
	res, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return Peer{}, err
	}
	defer res.Body.Close()
	var ans struct {
		Token        string `json:"token"`
		Daemon       string `json:"daemon"`
		Version      string `json:"version"`
		ServerPubKey string `json:"serverPubKey"`
		Role         string `json:"role"`
		PublicURL    string `json:"publicUrl"`
		Error        string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&ans); err != nil {
		return Peer{}, fmt.Errorf("%s did not answer a pairing: %w", base, err)
	}
	if res.StatusCode >= 300 {
		return Peer{}, fmt.Errorf("%s refused: %s", base, firstNonEmpty(ans.Error, res.Status))
	}
	if ans.Role != Role {
		return Peer{}, fmt.Errorf("%s opened a window for %s, not for a laptop — on it run: corgi agent peers invite", base, firstNonEmpty(ans.Role, "a phone"))
	}
	serverPub, err := pairing.ParsePublicKey(ans.ServerPubKey)
	if err != nil || serverPub == nil {
		return Peer{}, fmt.Errorf("%s gave no encryption key; it runs corgi before 2.20.12", base)
	}
	shared, err := pairing.SharedKeyOnDevice(priv, serverPub)
	if err != nil {
		return Peer{}, err
	}
	p := Peer{Name: firstNonEmpty(ans.Daemon, base), URL: firstNonEmpty(ans.PublicURL, base), Token: ans.Token, Key: base64.StdEncoding.EncodeToString(shared), Since: time.Now(), Version: ans.Version}
	if bytes.Equal(serverPub.Bytes(), priv.PublicKey().Bytes()) {
		return Peer{}, fmt.Errorf("that is this laptop")
	}
	err = Update(Path(agentDir), func(s *Store) { s.put(p) })
	return p, err
}

func cleanURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("%q is not a laptop's address", raw)
	}
	return u.Scheme + "://" + u.Host, nil
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// PublicKeyOf is the device public key this laptop pairs with, for tests
// and for a peer that wants to check who it talks to.
func PublicKeyOf(agentDir string) (*ecdh.PublicKey, error) {
	priv, err := pairing.LoadOrCreateServerKey(pairing.ServerKeyPath(agentDir))
	if err != nil {
		return nil, err
	}
	return priv.PublicKey(), nil
}
