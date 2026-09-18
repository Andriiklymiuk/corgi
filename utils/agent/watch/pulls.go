package watch

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/atomicfile"
)

type PullStatus struct {
	State  string    `json:"state"`
	Checks string    `json:"checks,omitempty"`
	Review string    `json:"review,omitempty"`
	At     time.Time `json:"at"`
}

func (p PullStatus) Facts() sessions.PullFacts {
	return sessions.PullFacts{State: p.State, Checks: p.Checks, Review: p.Review}
}

func (p PullStatus) Ready() bool { return sessions.PullReady(p.Facts()) }

func (p PullStatus) Line() string { return sessions.PullLine(p.Facts()) }

type PullAsker interface {
	PullStatus(ctx context.Context, ref string) (PullStatus, bool)
}

var (
	githubPull = regexp.MustCompile(`^https?://github\.com/([^/]+/[^/]+)/pull/(\d+)`)
	gitlabMR   = regexp.MustCompile(`^https?://[^/]+/(.+?)/-/merge_requests/(\d+)`)
)

var pullLink = regexp.MustCompile(`https?://github\.com/[^/\s]+/[^/\s]+/pull/\d+|https?://[^/\s]+/\S+?/-/merge_requests/\d+`)

func PullLinks(text string) []string {
	var out []string
	for _, link := range pullLink.FindAllString(text, -1) {
		link = strings.TrimRight(link, ").,;:")
		if PullRef(link) != "" {
			out = append(out, link)
		}
	}
	return out
}

func PullRef(link string) string {
	if m := githubPull.FindStringSubmatch(link); m != nil {
		return m[1] + "#" + m[2]
	}
	if m := gitlabMR.FindStringSubmatch(link); m != nil {
		return m[1] + "!" + m[2]
	}
	return ""
}

type PullLog struct {
	mu    sync.Mutex
	path  string
	Pulls map[string]PullStatus `json:"pulls,omitempty"`
}

const pullKeep = 200

func pullsPath(agentDir string) string { return filepath.Join(agentDir, "watch", "pulls.json") }

func LoadPullLog(agentDir string) *PullLog {
	l := &PullLog{path: pullsPath(agentDir), Pulls: map[string]PullStatus{}}
	if data, err := os.ReadFile(l.path); err == nil {
		_ = json.Unmarshal(data, l)
	}
	if l.Pulls == nil {
		l.Pulls = map[string]PullStatus{}
	}
	return l
}

func (l *PullLog) Get(refOrLink string) (PullStatus, bool) {
	if strings.HasPrefix(refOrLink, "http") {
		refOrLink = PullRef(refOrLink)
	}
	if refOrLink == "" {
		return PullStatus{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	p, ok := l.Pulls[refOrLink]
	return p, ok
}

func (l *PullLog) Set(ref string, p PullStatus) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if ref == "" {
		return nil
	}
	l.Pulls[ref] = p
	for len(l.Pulls) > pullKeep {
		oldestKey, oldest := "", time.Time{}
		for k, v := range l.Pulls {
			if oldest.IsZero() || v.At.Before(oldest) {
				oldestKey, oldest = k, v.At
			}
		}
		delete(l.Pulls, oldestKey)
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return err
	}
	return atomicfile.Write(l.path, data, 0o600)
}

func checksVerdict(conclusions []string, running int) string {
	if len(conclusions) == 0 && running == 0 {
		return "none"
	}
	seen := false
	for _, c := range conclusions {
		switch strings.ToLower(c) {
		case "failure", "timed_out", "action_required", "error", "failed", "startup_failure":
			return "failing"
		case "success", "succeeded", "passed":
			seen = true
		}
	}
	if running > 0 {
		return "pending"
	}
	if !seen {
		return "none"
	}
	return "passing"
}
