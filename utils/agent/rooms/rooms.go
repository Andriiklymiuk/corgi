package rooms

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	maxText   = 16 << 10
	maxData   = 64 << 10
	lockStale = 30 * time.Second
	ReadLimit = 200
)

type Message struct {
	ID     string            `json:"id"`
	Room   string            `json:"room"`
	Thread string            `json:"thread,omitempty"`
	By     string            `json:"by"`
	Text   string            `json:"text,omitempty"`
	Shape  string            `json:"shape,omitempty"`
	Data   json.RawMessage   `json:"data,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
	To     []string          `json:"to,omitempty"`
	Via    string            `json:"via,omitempty"`
	At     time.Time         `json:"at"`
}

type Info struct {
	Name     string    `json:"name"`
	Messages int       `json:"messages"`
	Last     time.Time `json:"last,omitzero"`
	LastBy   string    `json:"lastBy,omitempty"`
}

var roomName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}(/[a-z0-9][a-z0-9-]{0,39}){0,3}$`)

func CleanName(name string) (string, error) {
	name = strings.Trim(strings.ToLower(strings.TrimSpace(name)), "/")
	if !roomName.MatchString(name) {
		return "", fmt.Errorf("a room is lower-case letters, digits and dashes, up to four parts: security/findings, not %q", name)
	}
	return name, nil
}

func Dir(agentDir string) string { return filepath.Join(agentDir, "rooms") }

func roomPath(agentDir, room string) string {
	return filepath.Join(Dir(agentDir), "rooms", filepath.FromSlash(room)+".jsonl")
}

func lock(agentDir string) (func(), error) {
	path := filepath.Join(Dir(agentDir), "rooms.lock")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(5 * time.Second)
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
			return nil, errors.New("the rooms are busy, try again")
		}
		time.Sleep(15 * time.Millisecond)
	}
}

func readAll(agentDir, room string) ([]Message, error) {
	f, err := os.Open(roomPath(agentDir, room))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []Message
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 0, 64<<10), maxText+maxData+(8<<10))
	for scan.Scan() {
		var m Message
		if json.Unmarshal(scan.Bytes(), &m) == nil && m.ID != "" {
			out = append(out, m)
		}
	}
	return out, scan.Err()
}

func number(id string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(id, "m"))
	return n
}

func Post(agentDir string, m Message, now time.Time) (Message, error) {
	room, err := CleanName(m.Room)
	if err != nil {
		return Message{}, err
	}
	m.Room, m.By, m.Text, m.Thread = room, strings.TrimSpace(m.By), strings.TrimSpace(m.Text), strings.TrimSpace(m.Thread)
	switch {
	case m.By == "":
		return Message{}, errors.New("a message says who wrote it")
	case m.Text == "" && len(m.Data) == 0:
		return Message{}, errors.New("a message needs words or data")
	case len(m.Text) > maxText || len(m.Data) > maxData:
		return Message{}, fmt.Errorf("a message holds up to %d KB of words and %d KB of data", maxText>>10, maxData>>10)
	}
	if m.Shape != "" {
		shape, err := LoadShape(agentDir, m.Shape)
		if err != nil {
			return Message{}, err
		}
		if err := shape.Check(m.Data); err != nil {
			return Message{}, fmt.Errorf("the data does not fit %s: %w", m.Shape, err)
		}
	} else if len(m.Data) > 0 && !json.Valid(m.Data) {
		return Message{}, errors.New("the data is not JSON")
	}
	unlock, err := lock(agentDir)
	if err != nil {
		return Message{}, err
	}
	defer unlock()
	all, err := readAll(agentDir, room)
	if err != nil {
		return Message{}, err
	}
	last := 0
	found := m.Thread == ""
	for _, old := range all {
		if n := number(old.ID); n > last {
			last = n
		}
		if old.ID == m.Thread && old.Thread == "" {
			found = true
		}
	}
	if !found {
		return Message{}, fmt.Errorf("no thread %s in %s: a reply names the first message of its thread", m.Thread, room)
	}
	m.ID, m.At = fmt.Sprintf("m%d", last+1), now.UTC()
	line, err := json.Marshal(m)
	if err != nil {
		return Message{}, err
	}
	path := roomPath(agentDir, room)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Message{}, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return Message{}, err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		_ = f.Close()
		return Message{}, err
	}
	return m, f.Close()
}

type Query struct {
	Room   string
	After  string
	Thread string
	Label  map[string]string
	Limit  int
}

func Read(agentDir string, q Query) ([]Message, error) {
	room, err := CleanName(q.Room)
	if err != nil {
		return nil, err
	}
	all, err := readAll(agentDir, room)
	if err != nil {
		return nil, err
	}
	after := number(q.After)
	out := []Message{}
	for _, m := range all {
		if number(m.ID) <= after {
			continue
		}
		if q.Thread != "" && m.ID != q.Thread && m.Thread != q.Thread {
			continue
		}
		if !hasLabels(m, q.Label) {
			continue
		}
		out = append(out, m)
	}
	limit := q.Limit
	if limit <= 0 || limit > ReadLimit {
		limit = ReadLimit
	}
	if q.After == "" && len(out) > limit {
		out = out[len(out)-limit:]
	} else if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func hasLabels(m Message, want map[string]string) bool {
	for k, v := range want {
		if m.Labels[k] != v {
			return false
		}
	}
	return true
}

func List(agentDir string) []Info {
	root := filepath.Join(Dir(agentDir), "rooms")
	out := []Info{}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		rel, _ := filepath.Rel(root, strings.TrimSuffix(path, ".jsonl"))
		name := filepath.ToSlash(rel)
		all, _ := readAll(agentDir, name)
		info := Info{Name: name, Messages: len(all)}
		if len(all) > 0 {
			info.Last, info.LastBy = all[len(all)-1].At, all[len(all)-1].By
		}
		out = append(out, info)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func ParseLabels(pairs []string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	out := map[string]string{}
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if k = strings.TrimSpace(k); !ok || k == "" {
			return nil, fmt.Errorf("a label is key=value, not %q", p)
		}
		out[k] = strings.TrimSpace(v)
	}
	return out, nil
}
