package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/events"
	"andriiklymiuk/corgi/utils/agent/watch"
)

// WatchSpec is one workspace's watch, resolved by cmd from config and tokens.
type WatchSpec struct {
	Workspace string
	Dir       string
	ConfigDir string
	Project   string   // tracker key prefix: ABC-123 belongs to ABC
	Repos     []string // owner/repo
	Rules     watch.Rules
	Sources   []watch.Source
	Interval  time.Duration
	// Action is notify or fix.
	Action string
	// SkipPermissions lets the fix run unattended; without it claude stops
	// at the first permission prompt and the run times out.
	SkipPermissions bool
}

// owns says an event is this workspace's by project key or repo.
func (s WatchSpec) owns(e watch.Event) bool {
	switch e.Kind {
	case watch.KindIssueNew, watch.KindIssueComment:
		return s.Project != "" && strings.HasPrefix(strings.ToUpper(e.Ref), strings.ToUpper(s.Project)+"-")
	default:
		repo := e.Ref
		if i := strings.IndexAny(repo, "#!"); i > 0 {
			repo = repo[:i]
		}
		for _, r := range s.Repos {
			if strings.EqualFold(r, repo) {
				return true
			}
		}
	}
	return false
}

// fixTimeout bounds one unattended claude run.
const fixTimeout = 30 * time.Minute

// claudeCommand builds the headless run; a seam for tests.
var claudeCommand = func(ctx context.Context, dir string, env []string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	// A timeout must take the tools claude spawned with it, not just claude.
	killProcessGroup(cmd)
	cmd.WaitDelay = 10 * time.Second
	return cmd
}

func (d *Daemon) startWatches(ctx context.Context) {
	if len(d.Watches) == 0 {
		return
	}
	d.watchState = watch.LoadState(d.Dir)
	d.watchers = map[string]*watch.Watch{}
	d.fixBusy = map[string]*sync.Mutex{}
	for _, spec := range d.Watches {
		spec := spec
		w := &watch.Watch{Workspace: spec.Workspace, Rules: spec.Rules, Sources: spec.Sources, Interval: spec.Interval,
			State: d.watchState, Sink: d.watchSink(spec), Log: func(line string) { utils.Info("agent:", line) }}
		d.watchers[spec.Workspace] = w
		d.fixBusy[spec.Workspace] = &sync.Mutex{}
		if len(spec.Sources) > 0 && spec.Interval > 0 {
			go w.Run(ctx)
		}
	}
	utils.Infof("agent: watching %d workspace(s) — tracker and review events\n", len(d.Watches))
}

// handleWatchEvent routes a webhook's event to the workspace it belongs to,
// else to the first one whose rules take it; the seen list keeps it single.
func (d *Daemon) handleWatchEvent(ctx context.Context, e watch.Event) {
	if d.watchers == nil {
		return
	}
	for _, spec := range d.Watches {
		if spec.owns(e) {
			d.watchers[spec.Workspace].Handle(ctx, e)
			return
		}
	}
	for _, spec := range d.Watches {
		if d.watchers[spec.Workspace].Handle(ctx, e) {
			return
		}
	}
}

// WatchIdentity is who "me" is for a source, as its poll learned it.
func (d *Daemon) WatchIdentity(source string) string {
	if d.watchState == nil {
		return ""
	}
	for key, c := range d.watchState.Cursors {
		if strings.HasSuffix(key, "/"+source) && c["me"] != "" {
			return c["me"]
		}
	}
	return ""
}

func (d *Daemon) watchSink(spec WatchSpec) watch.Sink {
	return func(ctx context.Context, e watch.Event) {
		d.appendWatchEvent(e)
		if d.Events != nil {
			d.Events.Append(spec.Workspace, events.Event{At: time.Now().UTC(), Kind: "watch", Reason: string(e.Kind) + " " + e.Ref, URL: e.URL})
		}
		body := watchBody(e)
		if spec.Action == "fix" {
			if d.claimFix(spec.Workspace, e.Ref) {
				go d.runFix(ctx, spec, e)
			} else {
				body += " (a fix for it is already running)"
			}
		}
		go d.notifyAttention("corgi agent · "+spec.Workspace, body, spec.Workspace)
	}
}

// claimFix keeps one fix per issue or PR in flight: a second event on the
// same ref while claude is still working is reported, not run again.
func (d *Daemon) claimFix(workspace, ref string) bool {
	d.attentionMu.Lock()
	defer d.attentionMu.Unlock()
	if d.fixActive == nil {
		d.fixActive = map[string]bool{}
	}
	key := workspace + "/" + ref
	if d.fixActive[key] {
		return false
	}
	d.fixActive[key] = true
	return true
}

func (d *Daemon) releaseFix(workspace, ref string) {
	d.attentionMu.Lock()
	delete(d.fixActive, workspace+"/"+ref)
	d.attentionMu.Unlock()
}

func watchBody(e watch.Event) string {
	switch e.Kind {
	case watch.KindIssueNew:
		return fmt.Sprintf("new issue %s — %s", e.Ref, e.Title)
	case watch.KindIssueComment:
		return fmt.Sprintf("%s commented on %s: %s", firstNonEmpty(e.Author, "someone"), e.Ref, e.Body)
	case watch.KindPRReview:
		return fmt.Sprintf("%s reviewed %s: %s", firstNonEmpty(e.Author, "someone"), e.Ref, firstNonEmpty(e.Body, e.State))
	default:
		return fmt.Sprintf("%s commented on %s: %s", firstNonEmpty(e.Author, "someone"), e.Ref, e.Body)
	}
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// fixPrompt is what the headless claude gets. The skills carry the rules:
// one spec gate collapsed by the approval, draft PRs only, never merge.
func fixPrompt(e watch.Event) string {
	switch e.Kind {
	case watch.KindIssueNew:
		return "I approve all changes; ship it and open draft PRs, then watch CI to green. /corgi:stories " + e.Ref
	case watch.KindIssueComment:
		return fmt.Sprintf("A new comment on %s from %s says: %q. Act on it: I approve all changes; ship it and open draft PRs. /corgi:stories %s", e.Ref, firstNonEmpty(e.Author, "someone"), e.Body, e.Ref)
	default:
		return "Address the review feedback on my PR " + e.URL + ": apply the valid comments, push back on the wrong ones, reply and resolve the threads, push the fixes. /corgi:review " + e.URL
	}
}

var prLink = regexp.MustCompile(`https://(?:github\.com/[^\s)]+/pull/\d+|[^\s)]+/-/merge_requests/\d+)`)

// runFix runs one event's fix, one at a time per workspace, and reports
// how it ended. A key runs once: the seen list already holds it.
func (d *Daemon) runFix(ctx context.Context, spec WatchSpec, e watch.Event) {
	defer d.releaseFix(spec.Workspace, e.Ref)
	mu := d.fixBusy[spec.Workspace]
	mu.Lock()
	defer mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, fixTimeout)
	defer cancel()

	logDir := filepath.Join(d.Dir, "watch", "runs")
	_ = os.MkdirAll(logDir, 0o700)
	logPath := filepath.Join(logDir, safeName(e.Key)+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		utils.Infof("agent: watch fix %s: %v\n", e.Ref, err)
		return
	}
	defer logFile.Close()

	args := []string{"-p", fixPrompt(e), "--output-format", "text"}
	if spec.SkipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	} else {
		args = append(args, "--permission-mode", "acceptEdits")
	}
	var env []string
	if spec.ConfigDir != "" {
		env = append(env, "CLAUDE_CONFIG_DIR="+spec.ConfigDir)
	}
	fmt.Fprintf(logFile, "=== %s %s %s\n", time.Now().Format(time.RFC3339), e.Kind, e.Ref)
	cmd := claudeCommand(ctx, spec.Dir, env, args...)
	cmd.Stdin = nil
	out, runErr := cmd.Output()
	logFile.Write(out)
	if runErr != nil {
		fmt.Fprintf(logFile, "\n=== failed: %v\n", runErr)
		go d.notifyAttention("corgi agent · "+spec.Workspace, fmt.Sprintf("fix for %s failed: %v — log: %s", e.Ref, runErr, logPath), spec.Workspace)
		return
	}
	body := "fixed " + e.Ref
	if links := prLink.FindAllString(string(out), -1); len(links) > 0 {
		body += " — " + strings.Join(uniqueStrings(links), " ")
	} else if last := lastLine(string(out)); last != "" {
		body += " — " + clipText(last, 160)
	}
	go d.notifyAttention("corgi agent · "+spec.Workspace, body, spec.Workspace)
}

func (d *Daemon) appendWatchEvent(e watch.Event) {
	path := filepath.Join(d.Dir, "watch", "events.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	if data, err := json.Marshal(e); err == nil {
		f.Write(append(data, '\n'))
	}
}

func safeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			return t
		}
	}
	return ""
}

func clipText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
