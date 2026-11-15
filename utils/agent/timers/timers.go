package timers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Timer struct {
	ID        string    `json:"id"`
	At        time.Time `json:"at"`
	Text      string    `json:"text"`
	Session   string    `json:"session,omitempty"`
	Workspace string    `json:"workspace,omitempty"`
	By        string    `json:"by,omitempty"`
	Created   time.Time `json:"created"`
}

const (
	MaxTimers = 200
	MaxText   = 2000
	MaxAhead  = 90 * 24 * time.Hour
)

func Path(agentDir string) string { return filepath.Join(agentDir, "timers.json") }

var mu sync.Mutex

func load(path string) ([]Timer, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var list []Timer
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return list, nil
}

func save(path string, list []Timer) error {
	sort.Slice(list, func(i, j int) bool { return list[i].At.Before(list[j].At) })
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func newID() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func Add(agentDir string, t Timer, now time.Time) (Timer, error) {
	t.Text = strings.TrimSpace(t.Text)
	if t.Text == "" {
		return t, errors.New("say what the timer is for")
	}
	if len(t.Text) > MaxText {
		return t, fmt.Errorf("keep it under %d characters", MaxText)
	}
	if !t.At.After(now) {
		return t, errors.New("that time has already passed")
	}
	if t.At.Sub(now) > MaxAhead {
		return t, errors.New("timers go up to 90 days ahead")
	}
	mu.Lock()
	defer mu.Unlock()
	list, err := load(Path(agentDir))
	if err != nil {
		return t, err
	}
	if len(list) >= MaxTimers {
		return t, fmt.Errorf("%d timers are waiting already: corgi agent later cancel <id>", len(list))
	}
	t.ID = newID()
	t.Created = now
	list = append(list, t)
	return t, save(Path(agentDir), list)
}

func List(agentDir string) ([]Timer, error) {
	mu.Lock()
	defer mu.Unlock()
	list, err := load(Path(agentDir))
	sort.Slice(list, func(i, j int) bool { return list[i].At.Before(list[j].At) })
	return list, err
}

func Cancel(agentDir, id string) (Timer, error) {
	mu.Lock()
	defer mu.Unlock()
	list, err := load(Path(agentDir))
	if err != nil {
		return Timer{}, err
	}
	for i, t := range list {
		if t.ID == id {
			list = append(list[:i], list[i+1:]...)
			return t, save(Path(agentDir), list)
		}
	}
	return Timer{}, fmt.Errorf("no timer %q (corgi agent later)", id)
}

func TakeDue(agentDir string, now time.Time) ([]Timer, error) {
	mu.Lock()
	defer mu.Unlock()
	list, err := load(Path(agentDir))
	if err != nil || len(list) == 0 {
		return nil, err
	}
	var due, keep []Timer
	for _, t := range list {
		if !t.At.After(now) {
			due = append(due, t)
		} else {
			keep = append(keep, t)
		}
	}
	if len(due) == 0 {
		return nil, nil
	}
	return due, save(Path(agentDir), keep)
}

var (
	clockRe = regexp.MustCompile(`^(\d{1,2}):(\d{2})$`)
	dayRe   = regexp.MustCompile(`^(\d+)d$`)
)

func ParseWhen(words []string, now time.Time) (time.Time, int, error) {
	if len(words) == 0 {
		return time.Time{}, 0, errors.New("say when: 2h, 30m, 1d, 17:30 or tomorrow 9:00")
	}
	w := strings.ToLower(strings.TrimSpace(words[0]))
	w = strings.TrimPrefix(w, "in ")
	if w == "tomorrow" {
		if len(words) > 1 {
			if t, ok := clock(words[1], now.AddDate(0, 0, 1)); ok {
				return t, 2, nil
			}
		}
		return clockAt(now.AddDate(0, 0, 1), 9, 0), 1, nil
	}
	if m := dayRe.FindStringSubmatch(w); m != nil {
		n, _ := strconv.Atoi(m[1])
		return now.Add(time.Duration(n) * 24 * time.Hour), 1, nil
	}
	if d, err := time.ParseDuration(w); err == nil && d > 0 {
		return now.Add(d), 1, nil
	}
	if t, ok := clock(w, now); ok {
		if !t.After(now) {
			t = t.AddDate(0, 0, 1)
		}
		return t, 1, nil
	}
	if t, err := time.Parse(time.RFC3339, words[0]); err == nil {
		return t, 1, nil
	}
	return time.Time{}, 0, fmt.Errorf("%q is not a time: 2h, 30m, 1d, 17:30, tomorrow 9:00", words[0])
}

func clock(s string, day time.Time) (time.Time, bool) {
	m := clockRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return time.Time{}, false
	}
	h, _ := strconv.Atoi(m[1])
	mins, _ := strconv.Atoi(m[2])
	if h > 23 || mins > 59 {
		return time.Time{}, false
	}
	return clockAt(day, h, mins), true
}

func clockAt(day time.Time, h, m int) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, day.Location())
}

func Words(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "in under a minute"
	case d < time.Hour:
		return fmt.Sprintf("in %d min", int(d.Minutes()+0.5))
	case d < 48*time.Hour:
		h := int(d.Hours())
		m := int(d.Minutes()) - h*60
		if m == 0 {
			return fmt.Sprintf("in %dh", h)
		}
		return fmt.Sprintf("in %dh %dm", h, m)
	}
	return fmt.Sprintf("in %d days", int(d.Hours()/24+0.5))
}
