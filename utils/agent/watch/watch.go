// Package watch turns tracker and code-review activity into events the
// daemon can act on: a new bug in Linear or Jira, a reviewer's comment on
// one of your pull requests. Sources are polled with a saved cursor so
// each round asks only for what changed; webhooks feed the same pipeline.
// Nothing here spends agent tokens — the sink decides what to do.
package watch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils/atomicfile"
)

// Kind says what happened.
type Kind string

const (
	KindIssueNew     Kind = "issue.new"
	KindIssueComment Kind = "issue.comment"
	KindPRComment    Kind = "pr.comment"
	KindPRReview     Kind = "pr.review"
)

// Event is one thing worth telling a person or an agent about.
type Event struct {
	// Key is stable across polls and webhooks for the same thing, so a
	// comment seen twice is handled once: "linear:ABC-123", "github:acme/api#12:c123".
	Key       string    `json:"key"`
	Source    string    `json:"source"` // linear, jira, github, gitlab
	Kind      Kind      `json:"kind"`
	Workspace string    `json:"workspace,omitempty"`
	Ref       string    `json:"ref"` // ABC-123, acme/api#12
	Title     string    `json:"title"`
	Body      string    `json:"body,omitempty"`
	URL       string    `json:"url,omitempty"`
	Author    string    `json:"author,omitempty"`
	Labels    []string  `json:"labels,omitempty"`
	State     string    `json:"state,omitempty"`
	Assignee  string    `json:"assignee,omitempty"`
	Mine      bool      `json:"mine,omitempty"` // assigned to me, or my PR
	At        time.Time `json:"at"`
}

// Rules is what a workspace asked to be told about. Zero value matches
// nothing; Enabled must be set.
type Rules struct {
	Enabled  bool
	Labels   []string // any of; empty means any label
	States   []string // any of; empty means any state
	Assignee string   // "me" (default) or "any"
	Comments bool     // comments on issues assigned to me
	PRs      bool     // reviews and comments on my pull requests
}

// Match says whether an event is one the rules asked for.
func (r Rules) Match(e Event) bool { return r.Why(e) == "" }

// Why is what stops an event, in the words a person can act on; "" when
// the rules take it.
func (r Rules) Why(e Event) string {
	if !r.Enabled {
		return "watch is off here"
	}
	if !r.matchesKind(e.Kind) {
		switch e.Kind {
		case KindIssueComment:
			return "issue comments need --comments"
		case KindPRComment, KindPRReview:
			return "PR reviews and comments need --prs"
		}
		return "kind " + string(e.Kind) + " is not watched"
	}
	switch e.Kind {
	case KindPRComment, KindPRReview, KindIssueComment:
		if !e.Mine {
			return "not on something of mine"
		}
	case KindIssueNew:
		if r.Assignee != "any" && !e.Mine {
			return "not assigned to me (--assignee any takes every issue)"
		}
		if len(r.Labels) > 0 && !anyFold(e.Labels, r.Labels) {
			return fmt.Sprintf("none of the labels %s is on it (it has %s)", strings.Join(r.Labels, ", "), orNone(e.Labels))
		}
		if len(r.States) > 0 && !containsFold(r.States, e.State) {
			return fmt.Sprintf("state %q is not one of %s", e.State, strings.Join(r.States, ", "))
		}
	}
	return ""
}

func orNone(list []string) string {
	if len(list) == 0 {
		return "none"
	}
	return strings.Join(list, ", ")
}

// matchesKind says whether any event of this kind could pass the rules.
func (r Rules) matchesKind(k Kind) bool {
	if !r.Enabled {
		return false
	}
	switch k {
	case KindPRComment, KindPRReview:
		return r.PRs
	case KindIssueComment:
		return r.Comments
	case KindIssueNew:
		return true
	}
	return false
}

// MatchesNothing says no event can ever pass. Only Enabled closes every
// kind — issue.new is possible whenever the rules are on — so this is the
// whole test; per-source dead ends are DeadSource's.
func (r Rules) MatchesNothing() bool { return !r.Enabled }

// sourceKinds is everything each source can emit.
var sourceKinds = map[string][]Kind{
	"linear": {KindIssueNew, KindIssueComment},
	"jira":   {KindIssueNew, KindIssueComment},
	"github": {KindPRComment, KindPRReview},
	"gitlab": {KindPRComment, KindPRReview},
}

// DeadSource says a source can emit nothing these rules take, so polling
// it would only spend requests. A source not in the table is assumed live.
func (r Rules) DeadSource(source string) bool {
	kinds, ok := sourceKinds[source]
	if !ok {
		return r.MatchesNothing()
	}
	for _, k := range kinds {
		if r.matchesKind(k) {
			return false
		}
	}
	return true
}

// DeadSources lists the known sources these rules can never use, sorted.
func (r Rules) DeadSources() []string {
	var out []string
	for name := range sourceKinds {
		if r.DeadSource(name) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func containsFold(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(strings.TrimSpace(x), strings.TrimSpace(s)) {
			return true
		}
	}
	return false
}

func anyFold(have, want []string) bool {
	for _, h := range have {
		if containsFold(want, h) {
			return true
		}
	}
	return false
}

// Cursor is a source's own bookmark: an updatedAt, an ETag, the last id.
type Cursor map[string]string

// Source is one API that can be polled for changes since a cursor.
type Source interface {
	Name() string
	// Poll returns what changed since cursor and the cursor to save. A source
	// with nothing new returns nil events and the same cursor; a 304 costs
	// one request and no parsing.
	Poll(ctx context.Context, cursor Cursor) ([]Event, Cursor, error)
}

// Sink receives matched, unseen events.
type Sink func(ctx context.Context, e Event)

// State is what survives restarts: cursors per workspace and source, and
// the keys already handled. One file under the agent dir.
type State struct {
	mu      sync.Mutex
	path    string
	Cursors map[string]Cursor `json:"cursors"` // "<workspace>/<source>"
	Seen    []string          `json:"seen"`    // newest last
	Errors  map[string]string `json:"errors,omitempty"`
	Polled  map[string]string `json:"polled,omitempty"` // "<workspace>/<source>" → RFC3339
	// seen indexes Seen so a lookup does not walk the list.
	seen map[string]struct{}
	// Fixes is the fix history beside it, its own file.
	Fixes *FixLog `json:"-"`
}

const seenKeep = 2000

// LoadState reads <agentDir>/watch/state.json; a missing file is empty state.
func LoadState(agentDir string) *State {
	s := &State{path: filepath.Join(agentDir, "watch", "state.json"), Cursors: map[string]Cursor{}}
	if data, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(data, s)
	}
	if s.Cursors == nil {
		s.Cursors = map[string]Cursor{}
	}
	s.seen = make(map[string]struct{}, len(s.Seen))
	for _, k := range s.Seen {
		s.seen[k] = struct{}{}
	}
	s.Fixes = LoadFixLog(agentDir)
	return s
}

// IsSeen says a key was handled, without recording anything.
func (s *State) IsSeen(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.seen[key]
	return ok
}

func (s *State) save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.Seen) > seenKeep {
		for _, k := range s.Seen[:len(s.Seen)-seenKeep] {
			delete(s.seen, k)
		}
		s.Seen = s.Seen[len(s.Seen)-seenKeep:]
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	return atomicfile.Write(s.path, data, 0o600)
}

// MarkSeen records a key; false when it was already there.
func (s *State) MarkSeen(key string) bool {
	s.mu.Lock()
	if _, ok := s.seen[key]; ok {
		s.mu.Unlock()
		return false
	}
	s.seen[key] = struct{}{}
	s.Seen = append(s.Seen, key)
	s.mu.Unlock()
	_ = s.save()
	return true
}

// Unsee forgets a key so the event can be handled again — a fix that was
// deferred, not done.
func (s *State) Unsee(key string) {
	s.mu.Lock()
	if _, ok := s.seen[key]; !ok {
		s.mu.Unlock()
		return
	}
	delete(s.seen, key)
	kept := s.Seen[:0]
	for _, k := range s.Seen {
		if k != key {
			kept = append(kept, k)
		}
	}
	s.Seen = kept
	s.mu.Unlock()
	_ = s.save()
}

func (s *State) cursor(ws, source string) Cursor {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.Cursors[ws+"/"+source]
	if c == nil {
		return Cursor{}
	}
	return c
}

func (s *State) setCursor(ws, source string, c Cursor, now time.Time, err error) {
	s.mu.Lock()
	key := ws + "/" + source
	if c != nil {
		s.Cursors[key] = c
	}
	if s.Polled == nil {
		s.Polled = map[string]string{}
	}
	s.Polled[key] = now.UTC().Format(time.RFC3339)
	if s.Errors == nil {
		s.Errors = map[string]string{}
	}
	if err != nil {
		s.Errors[key] = err.Error()
	} else {
		delete(s.Errors, key)
	}
	s.mu.Unlock()
	_ = s.save()
}

// Watch is one workspace's poll loop.
type Watch struct {
	Workspace string
	Rules     Rules
	Sources   []Source
	Interval  time.Duration
	State     *State
	Sink      Sink
	// Log receives one line per round that did something; nil is silent.
	Log func(string)
}

// Once polls every source one time and hands new matches to the sink.
// Returns how many events were handed over.
func (w *Watch) Once(ctx context.Context, now time.Time) int {
	handed := 0
	for _, src := range w.Sources {
		before := w.State.cursor(w.Workspace, src.Name())
		events, cursor, err := src.Poll(ctx, before)
		w.State.setCursor(w.Workspace, src.Name(), cursor, now, err)
		if err != nil {
			w.logf("watch %s/%s: %v", w.Workspace, src.Name(), err)
			continue
		}
		// The first round only sets the bookmark: what is already in the
		// tracker is not news, and with action fix it would be a burst of runs.
		if len(before) == 0 {
			if len(events) > 0 {
				w.logf("watch %s/%s: bookmark set, %d older item(s) skipped", w.Workspace, src.Name(), len(events))
			}
			continue
		}
		for _, e := range events {
			if w.Handle(ctx, e) {
				handed++
			}
		}
	}
	return handed
}

// Handle runs one event through the rules and the seen list; true when it
// reached the sink. Webhooks enter here.
func (w *Watch) Handle(ctx context.Context, e Event) bool {
	if e.Workspace == "" {
		e.Workspace = w.Workspace
	}
	if !w.Rules.Match(e) || !w.State.MarkSeen(e.Key) {
		return false
	}
	if w.Sink != nil {
		w.Sink(ctx, e)
	}
	return true
}

// Run polls until ctx ends. Errors back the interval off, doubling up to
// ten times the base, so a dead token does not hammer an API.
func (w *Watch) Run(ctx context.Context) {
	interval := w.Interval
	if interval <= 0 {
		interval = 3 * time.Minute
	}
	wait := interval
	for {
		now := time.Now()
		w.Once(ctx, now)
		if w.failing() {
			wait = min(wait*2, interval*10)
		} else {
			wait = interval
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func (w *Watch) failing() bool {
	w.State.mu.Lock()
	defer w.State.mu.Unlock()
	for key := range w.State.Errors {
		if strings.HasPrefix(key, w.Workspace+"/") {
			return true
		}
	}
	return false
}

func (w *Watch) logf(format string, a ...any) {
	if w.Log != nil {
		w.Log(strings.TrimSpace(fmt.Sprintf(format, a...)))
	}
}

// Secrets are the API tokens, read from the environment first and then
// from <agentDir>/watch/secrets.json (0600), written by `corgi agent watch auth`.
type Secrets struct {
	Linear     string `json:"linear,omitempty"`
	JiraURL    string `json:"jiraUrl,omitempty"`
	JiraEmail  string `json:"jiraEmail,omitempty"`
	JiraToken  string `json:"jiraToken,omitempty"`
	GitHub     string `json:"github,omitempty"`
	GitLab     string `json:"gitlab,omitempty"`
	GitLabURL  string `json:"gitlabUrl,omitempty"`
	HookSecret string `json:"hookSecret,omitempty"`
	Me         string `json:"me,omitempty"` // tracker login/email when the API cannot tell us
}

func secretsPath(agentDir string) string { return filepath.Join(agentDir, "watch", "secrets.json") }

// LoadSecrets merges the file with the environment; env wins.
func LoadSecrets(agentDir string) Secrets {
	var s Secrets
	if data, err := os.ReadFile(secretsPath(agentDir)); err == nil {
		_ = json.Unmarshal(data, &s)
	}
	pick := func(dst *string, env string) {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			*dst = v
		}
	}
	pick(&s.Linear, "LINEAR_API_KEY")
	pick(&s.JiraURL, "JIRA_URL")
	pick(&s.JiraEmail, "JIRA_EMAIL")
	pick(&s.JiraToken, "JIRA_API_TOKEN")
	pick(&s.GitHub, "GITHUB_TOKEN")
	pick(&s.GitLab, "GITLAB_TOKEN")
	pick(&s.GitLabURL, "GITLAB_URL")
	return s
}

// GitHubToken is the token a GitHub poll would use and where it comes from:
// "saved" for the environment or the file, "gh-auth" for the gh CLI's,
// "" for none. Saved wins, as in NewGitHub.
func GitHubToken(s Secrets) (token, source string) {
	if t := strings.TrimSpace(s.GitHub); t != "" {
		return t, "saved"
	}
	if t := strings.TrimSpace(githubAuthToken()); t != "" {
		return t, "gh-auth"
	}
	return "", ""
}

// SaveSecrets writes the file; the environment is never written.
func SaveSecrets(agentDir string, s Secrets) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(secretsPath(agentDir)), 0o700); err != nil {
		return err
	}
	return atomicfile.Write(secretsPath(agentDir), data, 0o600)
}

// Fingerprint is a short id for a token, safe to print.
func Fingerprint(token string) string {
	if token == "" {
		return "none"
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])[:8]
}

// Summary is what `corgi agent watch` prints per workspace and source.
type Summary struct {
	Key    string
	Polled string
	Error  string
}

// Summaries lists the saved poll state, sorted.
func (s *State) Summaries() []Summary {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Summary
	for key, at := range s.Polled {
		out = append(out, Summary{Key: key, Polled: at, Error: s.Errors[key]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// ErrNoToken is what a source returns when it has nothing to authenticate with.
var ErrNoToken = errors.New("no token")

// FixRecord is one fix the daemon started, kept so the caps hold across
// restarts.
type FixRecord struct {
	Key       string    `json:"key"`
	Workspace string    `json:"workspace"`
	StartedAt time.Time `json:"startedAt"`
}

// FixLog is <agentDir>/watch/fixes.json: the fixes started, newest last,
// and the events whose fix a cap, quiet hours or a limit deferred — kept
// aside for a manual `corgi agent watch run`, never retried by the daemon
// on its own.
type FixLog struct {
	mu       sync.Mutex
	path     string
	Started  []FixRecord `json:"started"`
	Deferred []Event     `json:"deferred,omitempty"`
}

const (
	fixKeep      = 200
	deferredKeep = 100
)

// LoadFixLog reads the file; a missing file is an empty log.
func LoadFixLog(agentDir string) *FixLog {
	l := &FixLog{path: filepath.Join(agentDir, "watch", "fixes.json")}
	if data, err := os.ReadFile(l.path); err == nil {
		_ = json.Unmarshal(data, l)
	}
	return l
}

func (l *FixLog) save() error {
	if len(l.Started) > fixKeep {
		l.Started = l.Started[len(l.Started)-fixKeep:]
	}
	if len(l.Deferred) > deferredKeep {
		l.Deferred = l.Deferred[len(l.Deferred)-deferredKeep:]
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

// Start records a fix starting and drops the event from the deferred list
// if it was waiting there.
func (l *FixLog) Start(workspace, key string, at time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Started = append(l.Started, FixRecord{Key: key, Workspace: workspace, StartedAt: at})
	l.dropDeferred(key)
	_ = l.save()
}

// StartedSince counts the workspace's fixes started at or after since.
func (l *FixLog) StartedSince(workspace string, since time.Time) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, r := range l.Started {
		if r.Workspace == workspace && !r.StartedAt.Before(since) {
			n++
		}
	}
	return n
}

// LastStarted is the workspace's most recent fix start.
func (l *FixLog) LastStarted(workspace string) (last time.Time, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.Started {
		if r.Workspace == workspace && r.StartedAt.After(last) {
			last, ok = r.StartedAt, true
		}
	}
	return last, ok
}

// Defer keeps an event whose fix did not start; one entry per key.
func (l *FixLog) Defer(e Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.dropDeferred(e.Key)
	l.Deferred = append(l.Deferred, e)
	_ = l.save()
}

// DeferredEvents is a copy of what waits for a fix. Only the daemon
// writes the file: an event leaves the list when its fix starts.
func (l *FixLog) DeferredEvents() []Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Event(nil), l.Deferred...)
}

// DeferredCount is how many events wait for a fix in a workspace ("" is all).
func (l *FixLog) DeferredCount(workspace string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, e := range l.Deferred {
		if workspace == "" || e.Workspace == workspace {
			n++
		}
	}
	return n
}

func (l *FixLog) dropDeferred(key string) {
	kept := l.Deferred[:0]
	for _, e := range l.Deferred {
		if e.Key != key {
			kept = append(kept, e)
		}
	}
	l.Deferred = kept
}
