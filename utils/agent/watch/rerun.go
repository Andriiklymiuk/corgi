package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils/atomicfile"
)

// A red build is not always a red change: a runner that died, a flaky
// test, a registry that timed out. With rerunCI on, the first red on a
// repository of mine reruns the failed jobs once; only a second red, on
// the same run, is handed to the session or worked on. GitHub today —
// GitLab sends no CI notification the watch reads.

// GitHubAPI is the REST base; a test points it at its own server.
var GitHubAPI = "https://api.github.com"

// Rerun is one failed run rerun: its id, where it is, and when.
type Rerun struct {
	RunID int64     `json:"runId"`
	URL   string    `json:"url,omitempty"`
	At    time.Time `json:"at"`
}

// RerunLog remembers which runs were already rerun, so a second red on
// one run is not rerun again.
type RerunLog struct {
	mu     sync.Mutex
	path   string
	Reruns map[string]Rerun `json:"reruns,omitempty"`
}

const rerunKeep = 100

func rerunsPath(agentDir string) string { return filepath.Join(agentDir, "watch", "reruns.json") }

func LoadReruns(agentDir string) *RerunLog {
	l := &RerunLog{path: rerunsPath(agentDir), Reruns: map[string]Rerun{}}
	if data, err := os.ReadFile(l.path); err == nil {
		_ = json.Unmarshal(data, l)
	}
	if l.Reruns == nil {
		l.Reruns = map[string]Rerun{}
	}
	return l
}

// Seen says whether a run was rerun already.
func (l *RerunLog) Seen(runID int64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.Reruns[strconv.FormatInt(runID, 10)]
	return ok
}

// Set records a rerun; the oldest go once the file is full.
func (l *RerunLog) Set(r Rerun) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Reruns[strconv.FormatInt(r.RunID, 10)] = r
	for len(l.Reruns) > rerunKeep {
		oldest, at := "", time.Time{}
		for k, v := range l.Reruns {
			if oldest == "" || v.At.Before(at) {
				oldest, at = k, v.At
			}
		}
		delete(l.Reruns, oldest)
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	return atomicfile.Write(l.path, data, 0o600)
}

// FailedRun is a workflow run that went red, as GitHub lists it.
type FailedRun struct {
	ID        int64
	URL       string
	Name      string
	UpdatedAt time.Time
}

// NewestFailedRun is the repository's latest failed workflow run updated
// since a moment — the one a "workflow run failed" notification is about.
func NewestFailedRun(ctx context.Context, s Secrets, repo string, since time.Time) (FailedRun, error) {
	if s.GitHub == "" {
		return FailedRun{}, ErrNoToken
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, GitHubAPI+"/repos/"+repo+"/actions/runs?status=failure&per_page=10", nil)
	if err != nil {
		return FailedRun{}, err
	}
	req.Header.Set("Authorization", "Bearer "+s.GitHub)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return FailedRun{}, fmt.Errorf("listing runs of %s: %w", repo, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return FailedRun{}, fmt.Errorf("listing runs of %s: %s", repo, resp.Status)
	}
	var list struct {
		Runs []struct {
			ID        int64  `json:"id"`
			Name      string `json:"name"`
			HTMLURL   string `json:"html_url"`
			UpdatedAt string `json:"updated_at"`
		} `json:"workflow_runs"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&list); err != nil {
		return FailedRun{}, fmt.Errorf("listing runs of %s: %w", repo, err)
	}
	var best FailedRun
	for _, r := range list.Runs {
		at, _ := time.Parse(time.RFC3339, r.UpdatedAt)
		if at.Before(since) {
			continue
		}
		if best.ID == 0 || at.After(best.UpdatedAt) {
			best = FailedRun{ID: r.ID, URL: r.HTMLURL, Name: r.Name, UpdatedAt: at}
		}
	}
	if best.ID == 0 {
		return FailedRun{}, fmt.Errorf("no failed run of %s since %s", repo, since.Format(time.Kitchen))
	}
	return best, nil
}

// RerunFailedJobs asks GitHub to run the failed jobs of a run again.
func RerunFailedJobs(ctx context.Context, s Secrets, repo string, runID int64) error {
	if s.GitHub == "" {
		return ErrNoToken
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, GitHubAPI+"/repos/"+repo+"/actions/runs/"+strconv.FormatInt(runID, 10)+"/rerun-failed-jobs", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.GitHub)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("rerunning %s run %d: %w", repo, runID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("rerunning %s run %d: %s", repo, runID, resp.Status)
	}
	return nil
}
