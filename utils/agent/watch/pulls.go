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

	"andriiklymiuk/corgi/utils/atomicfile"
)

// PullStatus is what the forge says about a pull request past open or
// merged: whether its checks pass and whether it has been approved — the
// two things between "in review" and "ready to merge". Every surface reads
// the same words: checks passing / failing / pending / none, review
// approved / changes / pending / none.
type PullStatus struct {
	State  string    `json:"state"`
	Checks string    `json:"checks,omitempty"`
	Review string    `json:"review,omitempty"`
	At     time.Time `json:"at"`
}

// Ready is "nothing stands between this and Merge": open, checks green or
// absent, approved.
func (p PullStatus) Ready() bool {
	return p.State == "open" && (p.Checks == "passing" || p.Checks == "none" || p.Checks == "") && p.Review == "approved"
}

// Line is the status in a few words for a row: "checks ✓ · approved",
// "checks ✗", "changes requested".
func (p PullStatus) Line() string {
	var parts []string
	switch p.Checks {
	case "passing":
		parts = append(parts, "checks ✓")
	case "failing":
		parts = append(parts, "checks ✗")
	case "pending":
		parts = append(parts, "checks running")
	}
	switch p.Review {
	case "approved":
		parts = append(parts, "approved")
	case "changes":
		parts = append(parts, "changes requested")
	case "pending":
		parts = append(parts, "review pending")
	}
	if p.Ready() {
		return "ready to merge · " + strings.Join(parts, " · ")
	}
	return strings.Join(parts, " · ")
}

// PullAsker is a source that can say how a pull request stands: GitHub
// and GitLab do; the trackers do not.
type PullAsker interface {
	PullStatus(ctx context.Context, ref string) (PullStatus, bool)
}

var (
	githubPull = regexp.MustCompile(`^https?://github\.com/([^/]+/[^/]+)/pull/(\d+)`)
	gitlabMR   = regexp.MustCompile(`^https?://[^/]+/(.+?)/-/merge_requests/(\d+)`)
)

// PullRef is the ref a pull request link is filed under: acme/api#7 for
// GitHub, group/project!7 for GitLab. "" for anything else.
func PullRef(link string) string {
	if m := githubPull.FindStringSubmatch(link); m != nil {
		return m[1] + "#" + m[2]
	}
	if m := gitlabMR.FindStringSubmatch(link); m != nil {
		return m[1] + "!" + m[2]
	}
	return ""
}

// PullLog is <agentDir>/watch/pulls.json: the last status the daemon read
// for each pull request the inbox or the board mentions. Read by every
// surface, written by the daemon once a round.
type PullLog struct {
	mu    sync.Mutex
	path  string
	Pulls map[string]PullStatus `json:"pulls,omitempty"`
}

const pullKeep = 200

func pullsPath(agentDir string) string { return filepath.Join(agentDir, "watch", "pulls.json") }

// LoadPullLog reads the file; missing or broken is empty.
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

// Get is the status for a ref, or for a link.
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

// Set records a status; the oldest go once the file is past what any list shows.
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

// checksVerdict folds a set of check conclusions into one word: any failure
// is failing, else anything still running is pending, else passing; none
// at all is none. Neutral outcomes (skipped, cancelled, neutral) count as
// neither.
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
