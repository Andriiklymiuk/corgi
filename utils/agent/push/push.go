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

// Token is one phone's push token, by the device name pairing gave it.
type Token struct {
	Device string    `json:"device"`
	Token  string    `json:"token"`
	At     time.Time `json:"at"`
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
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, "ExponentPushToken[") && !strings.HasPrefix(token, "ExpoPushToken[") {
		return errors.New("that is not an Expo push token")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.Tokens[:0]
	for _, t := range s.Tokens {
		if t.Device != device && t.Token != token {
			kept = append(kept, t)
		}
	}
	s.Tokens = append(kept, Token{Device: device, Token: token, At: time.Now()})
	return s.save()
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
	for _, t := range tokens {
		msgs = append(msgs, expoMessage{To: t.Token, Title: m.Title, Body: m.Body, Sound: "default", Priority: "high",
			CategoryID: m.Category, Data: data, ThreadID: m.Thread, ChannelID: channelFor(m.Category)})
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
