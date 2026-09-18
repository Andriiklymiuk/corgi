package bots

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type Bot struct {
	Name        string    `json:"name"`
	Title       string    `json:"title,omitempty"`
	Workspace   string    `json:"workspace"`
	Soul        string    `json:"soul,omitempty"`
	Model       string    `json:"model,omitempty"`
	Profile     string    `json:"profile,omitempty"`
	Isolate     bool      `json:"isolate,omitempty"`
	Color       string    `json:"color,omitempty"`
	On          []string  `json:"on,omitempty"`
	LastSession string    `json:"lastSession,omitempty"`
	LastSeen    time.Time `json:"lastSeen,omitzero"`
	CreatedAt   time.Time `json:"createdAt"`
}

func (b Bot) Display() string {
	if strings.TrimSpace(b.Title) != "" {
		return b.Title
	}
	return b.Name
}

type Store struct {
	Version int   `json:"version"`
	Bots    []Bot `json:"bots"`
}

func Path(agentDir string) string { return filepath.Join(agentDir, "bots.json") }

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

func ValidName(name string) bool { return namePattern.MatchString(name) }

var Colors = []string{"indigo", "orange", "teal", "pink", "green", "amber", "blue", "red"}

const (
	triggerPRReview        = "pr.review"
	triggerReviewRequested = "review.requested"
	triggerCIFailed        = "ci.failed"
)

var Triggers = map[string]string{
	triggerPRReview:        "a review lands on a pull request",
	"pr.comment":           "someone comments on a pull request",
	triggerReviewRequested: "someone asks for a review",
	triggerCIFailed:        "a build goes red",
	"issue.new":            "a new issue arrives",
	"issue.comment":        "someone comments on an issue",
	"task":                 "a task of your own lands",
}

func ValidTrigger(kind string) bool { _, ok := Triggers[kind]; return ok }

func TriggerKinds() []string {
	return []string{triggerReviewRequested, triggerPRReview, "pr.comment", triggerCIFailed, "issue.new", "issue.comment", "task"}
}

func ParseTriggers(list []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, raw := range list {
		for _, k := range strings.Split(raw, ",") {
			k = strings.ToLower(strings.TrimSpace(k))
			if k == "" || k == "none" || seen[k] {
				continue
			}
			if !ValidTrigger(k) {
				return nil, fmt.Errorf("a bot cannot run on %q — one of %s", k, strings.Join(TriggerKinds(), ", "))
			}
			seen[k] = true
			out = append(out, k)
		}
	}
	return out, nil
}

func TriggerWords(on []string) string {
	var words []string
	for _, k := range on {
		if w, ok := Triggers[k]; ok {
			words = append(words, w)
		}
	}
	switch len(words) {
	case 0:
		return ""
	case 1:
		return words[0]
	}
	return strings.Join(words[:len(words)-1], ", ") + " or " + words[len(words)-1]
}

func Template(name string) (Bot, bool) {
	for _, t := range Templates {
		if t.Name == name {
			return t, true
		}
	}
	return Bot{}, false
}

func TemplateNames() []string {
	out := make([]string, 0, len(Templates))
	for _, t := range Templates {
		out = append(out, t.Name)
	}
	return out
}

func (b Bot) RunsOn(kind string) bool {
	for _, k := range b.On {
		if k == kind {
			return true
		}
	}
	return false
}

var Templates = []Bot{
	{Name: "reviewer", Title: "Code Reviewer", Color: "orange", Model: "sonnet", On: []string{triggerReviewRequested, triggerPRReview},
		Soul: "You review pull requests in this repository. Read the diff, run the tests, be brief: what is wrong, what is risky, what is fine. Post your findings as one review comment on the pull request. Never merge, never push."},
	{Name: "fixer", Title: "Build Fixer", Color: "red", Model: "sonnet", On: []string{triggerCIFailed},
		Soul: "A build went red on a branch. Read the failing job, find the cause, fix it on that branch with the smallest change, run the tests, push, and say in one line what it was."},
	{Name: "shipper", Title: "Shipper", Color: "teal", Model: "opus", Isolate: true,
		Soul: "You take a ticket from spec to pull request: read the ticket, plan in three lines, implement in a worktree of your own, run the tests, open a draft pull request, and hand off what is left."},
	{Name: "chief", Title: "Chief", Color: "pink", Model: "haiku",
		Soul: "You are the person's chief of staff for this repository: you answer what to look at first, what is blocked and why, who is on what — in a few lines, from the board. You never change code."},
	{Name: "proactive", Title: "Proactive", Color: "amber", Model: "opus",
		Soul: "You are the proactive engineer of this repository: you find the one thing worth building next — a feature the product almost does and a user would feel, or the thing a developer here trips on every day — and you say it with the evidence you can point at: a file and line, a promise the README makes that the code does not keep, a step done by hand. One idea at a time, ranked by what it changes for a user against what it costs. You never change code: you put the idea on the board as a task with its evidence, or spec it when asked."},
}

var Clocks = map[string]string{"proactive": "suggest"}

func TemplateClock(name string) string { return Clocks[name] }

var mu sync.Mutex

func Load(path string) (*Store, error) {
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
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	sort.Slice(s.Bots, func(i, j int) bool { return s.Bots[i].Name < s.Bots[j].Name })
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *Store) Find(name string) (Bot, bool) {
	for _, b := range s.Bots {
		if b.Name == name {
			return b, true
		}
	}
	return Bot{}, false
}

func (s *Store) Put(b Bot) {
	for i := range s.Bots {
		if s.Bots[i].Name == b.Name {
			if b.LastSession == "" {
				b.LastSession, b.LastSeen = s.Bots[i].LastSession, s.Bots[i].LastSeen
			}
			b.CreatedAt = s.Bots[i].CreatedAt
			s.Bots[i] = b
			return
		}
	}
	if b.CreatedAt.IsZero() {
		b.CreatedAt = time.Now().UTC()
	}
	s.Bots = append(s.Bots, b)
}

func (s *Store) Remove(name string) bool {
	for i := range s.Bots {
		if s.Bots[i].Name == name {
			s.Bots = append(s.Bots[:i], s.Bots[i+1:]...)
			return true
		}
	}
	return false
}

func RecordSession(path, name, sessionID string, at time.Time) error {
	mu.Lock()
	defer mu.Unlock()
	s, err := Load(path)
	if err != nil {
		return err
	}
	for i := range s.Bots {
		if s.Bots[i].Name == name {
			if s.Bots[i].LastSession == sessionID {
				return nil
			}
			s.Bots[i].LastSession, s.Bots[i].LastSeen = sessionID, at.UTC()
			return Save(path, s)
		}
	}
	return nil
}
