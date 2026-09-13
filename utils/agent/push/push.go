// Package push sends a notification to the phones paired with this laptop,
// through Expo's push service — so the laptop holds no Apple or Google key,
// and a phone that is revoked stops getting anything the moment its token
// is dropped. The payload is small on purpose: a title, a line, and ids
// the app uses to fetch the rest over the tunnel.
package push

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils/atomicfile"
)

// ExpoEndpoint is Expo's push API; a variable so a test can point it at a
// local server.
var ExpoEndpoint = "https://exp.host/--/api/v2/push/send"

// Token is one phone's push token, by the device name pairing gave it,
// with what that phone asked to hear and when.
type Token struct {
	Device string    `json:"device"`
	Token  string    `json:"token"`
	At     time.Time `json:"at"`
	// Quiet is a local "HH:MM-HH:MM" window in which only a permission —
	// the one thing a phone is for at night — gets through; "" is none.
	Quiet string `json:"quiet,omitempty"`
	// Only is "needs": only what needs a person (a permission, a red
	// build, a review asked for); the rest waits for the app. "" is all.
	Only string `json:"only,omitempty"`
}

// Prefs is what a phone asks to hear.
type Prefs struct {
	Quiet string
	Only  string
}

// Store is the token file: <agentDir>/push.json, 0600.
type Store struct {
	mu     sync.Mutex
	path   string
	Tokens []Token `json:"tokens"`
}

func Load(agentDir string) *Store {
	s := &Store{path: filepath.Join(agentDir, "push.json")}
	if data, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(data, s)
	}
	return s
}

func (s *Store) save() error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	return atomicfile.Write(s.path, data, 0o600)
}

// Set records a device's token, replacing an older one for the same device.
func (s *Store) Set(device, token string) error {
	return s.SetWith(device, token, Prefs{})
}

// SetWith records the token and what the phone asked to hear.
func (s *Store) SetWith(device, token string, p Prefs) error {
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, "ExponentPushToken[") && !strings.HasPrefix(token, "ExpoPushToken[") {
		return errors.New("that is not an Expo push token")
	}
	if p.Quiet != "" {
		if _, err := ParseQuiet(p.Quiet); err != nil {
			return err
		}
	}
	if p.Only != "" && p.Only != "needs" {
		return errors.New("only is \"needs\" or empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.Tokens[:0]
	for _, t := range s.Tokens {
		if t.Device != device && t.Token != token {
			kept = append(kept, t)
		}
	}
	s.Tokens = append(kept, Token{Device: device, Token: token, At: time.Now(), Quiet: p.Quiet, Only: p.Only})
	return s.save()
}

// Quiet is a local window, "HH:MM-HH:MM", that may cross midnight.
type Quiet struct{ from, to int }

// ParseQuiet reads "23:00-07:00".
func ParseQuiet(s string) (Quiet, error) {
	parts := strings.Split(strings.TrimSpace(s), "-")
	if len(parts) != 2 {
		return Quiet{}, fmt.Errorf("quiet hours are HH:MM-HH:MM, not %q", s)
	}
	minutes := func(hm string) (int, error) {
		var h, m int
		if _, err := fmt.Sscanf(strings.TrimSpace(hm), "%d:%d", &h, &m); err != nil || h < 0 || h > 23 || m < 0 || m > 59 {
			return 0, fmt.Errorf("quiet hours are HH:MM-HH:MM, not %q", s)
		}
		return h*60 + m, nil
	}
	from, err := minutes(parts[0])
	if err != nil {
		return Quiet{}, err
	}
	to, err := minutes(parts[1])
	if err != nil {
		return Quiet{}, err
	}
	return Quiet{from: from, to: to}, nil
}

// Contains says whether now falls in the window.
func (q Quiet) Contains(now time.Time) bool {
	m := now.Hour()*60 + now.Minute()
	if q.from <= q.to {
		return m >= q.from && m < q.to
	}
	return m >= q.from || m < q.to
}

// wants says whether this phone hears this message now: a permission
// always; the rest not in quiet hours and not when it asked for only what
// needs it — unless the message says it does (Data["needs"]).
func (t Token) wants(m Message, now time.Time) bool {
	if m.Category == "permission" {
		return true
	}
	needs := m.Data["needs"] == "1"
	if t.Only == "needs" && !needs {
		return false
	}
	if t.Quiet != "" {
		if q, err := ParseQuiet(t.Quiet); err == nil && q.Contains(now) && !needs {
			return false
		}
	}
	return true
}

// Remove drops a device's token: on revoke, or when Expo says the device
// is gone.
func (s *Store) Remove(device string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.Tokens[:0]
	for _, t := range s.Tokens {
		if t.Device != device {
			kept = append(kept, t)
		}
	}
	if len(kept) == len(s.Tokens) {
		return nil
	}
	s.Tokens = kept
	return s.save()
}

func (s *Store) removeToken(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.Tokens[:0]
	for _, t := range s.Tokens {
		if t.Token != token {
			kept = append(kept, t)
		}
	}
	s.Tokens = kept
	_ = s.save()
}

func (s *Store) List() []Token {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Token(nil), s.Tokens...)
}

// Message is one notification. Category picks the buttons the phone shows
// ("permission" gets Allow / Deny); Data is what the app needs to act.
type Message struct {
	Title    string            `json:"title"`
	Body     string            `json:"body"`
	Category string            `json:"categoryId,omitempty"`
	Data     map[string]string `json:"data,omitempty"`
	// Thread groups notifications about one session or workspace.
	Thread string `json:"-"`
}

type expoMessage struct {
	To         string            `json:"to"`
	Title      string            `json:"title"`
	Body       string            `json:"body"`
	Sound      string            `json:"sound"`
	Priority   string            `json:"priority"`
	CategoryID string            `json:"categoryId,omitempty"`
	Data       map[string]string `json:"data,omitempty"`
	ThreadID   string            `json:"threadId,omitempty"`
	ChannelID  string            `json:"channelId,omitempty"`
}

type expoReceipt struct {
	Data []struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		Details struct {
			Error string `json:"error"`
		} `json:"details"`
	} `json:"data"`
}

// Send delivers one message to every token. A token Expo reports as no
// longer registered is dropped. Errors are for the log: a push is never
// worth blocking anything on.
func (s *Store) Send(ctx context.Context, m Message) error {
	tokens := s.List()
	if len(tokens) == 0 {
		return nil
	}
	// A phone paired with two laptops answers the one that asked: the
	// hostname is what pairing told it this laptop is called.
	data := make(map[string]string, len(m.Data)+1)
	for k, v := range m.Data {
		data[k] = v
	}
	if _, ok := data["laptop"]; !ok {
		if host, err := os.Hostname(); err == nil && host != "" {
			data["laptop"] = host
		}
	}
	msgs := make([]expoMessage, 0, len(tokens))
	now := time.Now()
	sent := tokens[:0]
	for _, t := range tokens {
		if !t.wants(m, now) {
			continue
		}
		sent = append(sent, t)
		msgs = append(msgs, expoMessage{To: t.Token, Title: m.Title, Body: m.Body, Sound: "default", Priority: "high",
			CategoryID: m.Category, Data: data, ThreadID: m.Thread, ChannelID: channelFor(m.Category)})
	}
	tokens = sent
	if len(msgs) == 0 {
		return nil
	}
	raw, err := json.Marshal(msgs)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ExpoEndpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("expo push: %s", resp.Status)
	}
	var rc expoReceipt
	if json.NewDecoder(resp.Body).Decode(&rc) != nil {
		return nil
	}
	for i, r := range rc.Data {
		if i < len(tokens) && r.Status == "error" && r.Details.Error == "DeviceNotRegistered" {
			s.removeToken(tokens[i].Token)
		}
	}
	return nil
}

func channelFor(category string) string {
	if category == "permission" {
		return "permission"
	}
	return "inbox"
}
