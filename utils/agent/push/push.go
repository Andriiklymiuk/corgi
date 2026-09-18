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

var ExpoEndpoint = "https://exp.host/--/api/v2/push/send"

type Token struct {
	Device string    `json:"device"`
	Token  string    `json:"token"`
	At     time.Time `json:"at"`
	Quiet  string    `json:"quiet,omitempty"`
	Only   string    `json:"only,omitempty"`
}

type Prefs struct {
	Quiet string
	Only  string
}

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

func (s *Store) Set(device, token string) error {
	return s.SetWith(device, token, Prefs{})
}

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

type Quiet struct{ from, to int }

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

func (q Quiet) Contains(now time.Time) bool {
	m := now.Hour()*60 + now.Minute()
	if q.from <= q.to {
		return m >= q.from && m < q.to
	}
	return m >= q.from || m < q.to
}

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

type Message struct {
	Title    string            `json:"title"`
	Body     string            `json:"body"`
	Category string            `json:"categoryId,omitempty"`
	Data     map[string]string `json:"data,omitempty"`
	Thread   string            `json:"-"`
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

func (s *Store) Send(ctx context.Context, m Message) error {
	tokens, msgs := expoMessages(s.List(), m, time.Now())
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

func expoMessages(tokens []Token, m Message, now time.Time) ([]Token, []expoMessage) {
	if len(tokens) == 0 {
		return nil, nil
	}
	data := messageData(m)
	msgs := make([]expoMessage, 0, len(tokens))
	sent := tokens[:0]
	for _, t := range tokens {
		if !t.wants(m, now) {
			continue
		}
		sent = append(sent, t)
		msgs = append(msgs, expoMessage{To: t.Token, Title: m.Title, Body: m.Body, Sound: "default", Priority: "high",
			CategoryID: m.Category, Data: data, ThreadID: m.Thread, ChannelID: channelFor(m.Category)})
	}
	return sent, msgs
}

func messageData(m Message) map[string]string {
	data := make(map[string]string, len(m.Data)+1)
	for k, v := range m.Data {
		data[k] = v
	}
	if _, ok := data["laptop"]; !ok {
		if host, err := os.Hostname(); err == nil && host != "" {
			data["laptop"] = host
		}
	}
	return data
}

func channelFor(category string) string {
	if category == "permission" {
		return "permission"
	}
	return "inbox"
}
