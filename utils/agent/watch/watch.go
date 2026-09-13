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
	"regexp"
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
	// KindReviewRequested is someone asking me to review THEIR pull request.
	// The opposite of KindPRReview, which is feedback on mine, and the two
	// were one kind until an unattended run was told to "apply the valid
	// comments" on a colleague's branch.
	KindReviewRequested Kind = "review.requested"
	// KindCIFailed is a build that went red on something of mine. It is the
	// one kind that arrives with its own test for "done".
	KindCIFailed Kind = "ci.failed"
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
	CI       bool     // builds that went red on something of mine
	Reviews  bool     // pull requests someone asked me to review
	// From narrows comments and reviews to these people, matched against the
	// author's name or login. Half of what blocks a day is shaped like a
	// person — the one review you are waiting on — not like a board.
	From []string
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
		case KindCIFailed:
			return "red builds need --ci"
		case KindReviewRequested:
			return "review requests need --reviews"
		}
		return "kind " + string(e.Kind) + " is not watched"
	}
	switch e.Kind {
	case KindReviewRequested:
		// Someone else's pull request, addressed to me by construction: the
		// tracker only sends a review request to its reviewer. One already
		// merged needs no review.
		if len(r.States) == 0 {
			if over := finishedState(e.State); over != "" {
				return "it is " + over + " — there is nothing to review"
			}
		}
	case KindCIFailed:
		if !e.Mine {
			return "not on something of mine"
		}
	case KindPRComment, KindPRReview, KindIssueComment:
		if !e.Mine {
			return "not on something of mine"
		}
		// A comment on work that is already finished, duplicated or cancelled
		// is chatter, not a thing to do — on a ticket or on a pull request
		// that has already been merged. Explicit --states wins, as ever.
		if len(r.States) == 0 {
			if over := finishedState(e.State); over != "" {
				return "it is " + over + " — the comment is not work"
			}
		}
		// "Thanks, test is ok" asks for nothing. A thank-you, a sign-off, a
		// thumbs-up is the end of the work, not more of it.
		if e.Kind != KindPRReview && IsAcknowledgement(e.Body) {
			return "it is a thank-you or a sign-off, not a request"
		}
		if len(r.From) > 0 && !matchesPerson(e.Author, r.From) {
			return fmt.Sprintf("it is from %s, and you are waiting on %s",
				orNone([]string{e.Author}), strings.Join(r.From, ", "))
		}
	case KindIssueNew:
		if dead := finishedState(e.State); dead != "" {
			// A ticket someone has already closed as a duplicate is the one
			// piece of work guaranteed to be wasted. Explicit --states wins:
			// asking for a column means you meant it.
			if len(r.States) == 0 {
				return "it is " + dead + " — nobody is going to act on it"
			}
		}
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

// deadStates are the columns every tracker uses for work that will not
// happen: Linear's Duplicate and Canceled, Jira's Cancelled, GitHub's
// "closed as duplicate". Named rather than inferred, so a board with a
// column called "Duplicate detection" is not swept up with them.
var deadStates = map[string]string{
	"duplicate": "a duplicate", "duplicated": "a duplicate",
	"canceled": "cancelled", "cancelled": "cancelled",
	"won't do": "not being done", "wont do": "not being done",
	"not planned": "not planned", "obsolete": "obsolete",
	"rejected": "rejected",
}

// closedStates are the columns where the work is over. A comment arriving on
// one of these is someone tidying up, not something to act on.
var closedStates = map[string]string{
	"done": "done", "closed": "closed", "resolved": "resolved",
	"complete": "done", "completed": "done", "shipped": "shipped",
	"released": "released", "merged": "merged", "to release": "waiting on a release",
	"locked": "locked",
	// Past QA and out the door: a comment here is a sign-off, not work.
	"verified": "verified", "deployed": "deployed", "in production": "in production",
	"on production": "in production", "live": "live", "qa passed": "past QA", "tested": "tested",
}

var (
	ackWords = regexp.MustCompile(`(?i)\b(thanks?|thank you|thx|ty|ok|okay|lgtm|works?|working|good|great|perfect|nice|awesome|approved?|merged|done|confirmed|verified|passed|passing|green|fixed|resolved|all good|looks good)\b|👍|✅|🙏|🎉|👌`)
	askWords = regexp.MustCompile(`(?i)\?|\b(could|can|would|please|pls|should|need|needs|must|why|how|what|when|where|fix|change|update|add|remove|revert|still|but|however|not|doesn't|does not|isn't|is not|broken|fails?|failing|error|bug|wrong|missing)\b`)
)

// IsAcknowledgement says a comment asks for nothing: short, made of thanks
// or sign-off words, with no question and no request in it.
func IsAcknowledgement(body string) bool {
	text := strings.TrimSpace(body)
	if text == "" || len(text) > 160 {
		return false
	}
	if askWords.MatchString(text) {
		return false
	}
	return ackWords.MatchString(text)
}

// FinishedState is finishedState for callers outside this package.
func FinishedState(state string) string { return finishedState(state) }

// Settled names why an event needs nobody any more, given the column its
// ticket is in now, or "" while it is still waiting. A finished column ends
// every kind. A new issue is also over once it has left the column it was
// found in: someone picked it up, and the inbox was only ever announcing
// that it was there for the taking.
func Settled(e Event, current string) string {
	if over := finishedState(current); over != "" {
		return over
	}
	was, now := strings.TrimSpace(e.State), strings.TrimSpace(current)
	if e.Kind == KindIssueNew && was != "" && now != "" && !strings.EqualFold(was, now) {
		return "picked up, it is in " + now + " now"
	}
	return ""
}

// finishedState names why a ticket in this state is not worth anyone's time,
// or "" when there is still work in it.
func finishedState(state string) string {
	key := strings.ToLower(strings.TrimSpace(state))
	if why, ok := deadStates[key]; ok {
		return why
	}
	return closedStates[key]
}

// matchesPerson says an author is one of the people being waited on. A
// tracker spells the same human three ways — "Max Mustermann", "max", an
// email — so a wanted name matching any part of the author counts.
func matchesPerson(author string, wanted []string) bool {
	who := strings.ToLower(strings.TrimSpace(author))
	if who == "" {
		return false
	}
	for _, w := range wanted {
		w = strings.ToLower(strings.TrimSpace(w))
		if w != "" && strings.Contains(who, w) {
			return true
		}
	}
	return false
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
	case KindReviewRequested:
		return r.Reviews
	case KindCIFailed:
		return r.CI
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
	"github": {KindPRComment, KindPRReview, KindReviewRequested, KindCIFailed},
	"gitlab": {KindPRComment, KindPRReview, KindReviewRequested},
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
	mu       sync.Mutex
	path     string
	agentDir string
	Cursors  map[string]Cursor `json:"cursors"` // "<workspace>/<source>"
	Seen     []string          `json:"seen"`    // newest last
	Errors   map[string]string `json:"errors,omitempty"`
	Polled   map[string]string `json:"polled,omitempty"` // "<workspace>/<source>" → RFC3339
	// Held is what quiet hours swallowed, waiting for the window to open.
	Held []HeldNote `json:"held,omitempty"`
	// Ignored is only read, from a state.json an older corgi wrote; the list
	// now lives in ignored.json (see ignored.go) and this is cleared on load.
	Ignored []string `json:"ignored,omitempty"`
	// seen indexes Seen so a lookup does not walk the list.
	seen map[string]struct{}
	// roundRefs counts events per ref within one poll, so several comments
	// on one pull request collapse to one. Never persisted: it is about
	// this round only.
	roundRefs map[string]int
	// Fixes is the fix history beside it, its own file.
	Fixes *FixLog `json:"-"`
}

const seenKeep = 2000

// HeldNote is a notification quiet hours swallowed, kept so the morning can
// say what arrived rather than the night saying nothing and losing it.
type HeldNote struct {
	Workspace string    `json:"workspace"`
	Body      string    `json:"body"`
	At        time.Time `json:"at"`
	// Key names the event, so the morning can check it is still worth
	// saying before it says it.
	Key string `json:"key,omitempty"`
}

const heldKeep = 100

// LoadState reads <agentDir>/watch/state.json; a missing file is empty state.
func LoadState(agentDir string) *State {
	s := &State{path: filepath.Join(agentDir, "watch", "state.json"), agentDir: agentDir, Cursors: map[string]Cursor{}}
	if data, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(data, s)
	}
	s.moveIgnoredOut()
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

// Hold keeps a notification quiet hours must not deliver yet.
func (s *State) Hold(workspace, body string, at time.Time) {
	s.HoldEvent(workspace, "", body, at)
}

// HoldEvent is Hold with the event's key, for the re-check at release.
func (s *State) HoldEvent(workspace, key, body string, at time.Time) {
	s.mu.Lock()
	s.Held = append(s.Held, HeldNote{Workspace: workspace, Key: key, Body: body, At: at})
	if len(s.Held) > heldKeep {
		s.Held = s.Held[len(s.Held)-heldKeep:]
	}
	s.mu.Unlock()
	_ = s.save()
}

// TakeHeld returns and clears one workspace's held notes.
func (s *State) TakeHeld(workspace string) []HeldNote {
	s.mu.Lock()
	var mine, rest []HeldNote
	for _, n := range s.Held {
		if n.Workspace == workspace {
			mine = append(mine, n)
		} else {
			rest = append(rest, n)
		}
	}
	s.Held = rest
	s.mu.Unlock()
	if len(mine) > 0 {
		_ = s.save()
	}
	return mine
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
	// wake is the channel Nudge pokes: a poll now, not at the next tick.
	wake     chan struct{}
	wakeOnce sync.Once
	// Round runs before each poll, for work the clock decides — releasing
	// what quiet hours held back once the window opens.
	Round func(now time.Time)
	// Asleep says the watch is off for now — a day off — so no poll is made
	// until it wakes; a nudge (the reload button) still polls once.
	Asleep func(now time.Time) bool
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
	nudged := false
	for {
		now := time.Now()
		if w.Round != nil {
			w.Round(now)
		}
		if nudged || w.Asleep == nil || !w.Asleep(now) {
			w.Once(ctx, now)
		}
		nudged = false
		if w.failing() {
			wait = min(wait*2, interval*10)
		} else {
			wait = interval
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		case <-w.wakeChan():
			nudged = true
		}
	}
}

// Nudge asks for a poll now; a nudge while one is pending is the same nudge.
func (w *Watch) Nudge() {
	select {
	case w.wakeChan() <- struct{}{}:
	default:
	}
}

func (w *Watch) wakeChan() chan struct{} {
	w.wakeOnce.Do(func() { w.wake = make(chan struct{}, 1) })
	return w.wake
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

// secretsFile is the machine-wide tokens plus a per-workspace override.
type secretsFile struct {
	Secrets
	Workspaces map[string]Secrets `json:"workspaces,omitempty"`
}

func secretsPath(agentDir string) string { return filepath.Join(agentDir, "watch", "secrets.json") }

func readSecretsFile(agentDir string) secretsFile {
	var f secretsFile
	if data, err := os.ReadFile(secretsPath(agentDir)); err == nil {
		_ = json.Unmarshal(data, &f)
	}
	return f
}

// LoadSecrets is the machine-wide set; env wins over the file.
func LoadSecrets(agentDir string) Secrets {
	s := readSecretsFile(agentDir).Secrets
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

// LoadSecretsFor is what a workspace polls with: machine-wide, then its own
// tokens on top, so the most specific wins over the file and the env. The
// webhook secret is never overridden; one endpoint verifies every payload.
func LoadSecretsFor(agentDir, workspace string) Secrets {
	return overlaySecrets(LoadSecrets(agentDir), WorkspaceSecrets(agentDir, workspace))
}

// WorkspaceSecrets is only what this workspace stored, no fallback, no env.
func WorkspaceSecrets(agentDir, workspace string) Secrets {
	return readSecretsFile(agentDir).Workspaces[workspace]
}

// WorkspacesWithSecrets lists the workspaces holding an override.
func WorkspacesWithSecrets(agentDir string) []string {
	var out []string
	for id := range readSecretsFile(agentDir).Workspaces {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func overlaySecrets(base, over Secrets) Secrets {
	pick := func(dst *string, v string) {
		if v = strings.TrimSpace(v); v != "" {
			*dst = v
		}
	}
	pick(&base.Linear, over.Linear)
	pick(&base.JiraURL, over.JiraURL)
	pick(&base.JiraEmail, over.JiraEmail)
	pick(&base.JiraToken, over.JiraToken)
	pick(&base.GitHub, over.GitHub)
	pick(&base.GitLab, over.GitLab)
	pick(&base.GitLabURL, over.GitLabURL)
	pick(&base.Me, over.Me)
	return base
}

func (s Secrets) IsZero() bool {
	return s == Secrets{}
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

// SaveSecrets writes the machine-wide tokens, keeping every override.
func SaveSecrets(agentDir string, s Secrets) error {
	f := readSecretsFile(agentDir)
	f.Secrets = s
	return writeSecretsFile(agentDir, f)
}

// SaveWorkspaceSecrets writes one override; a zero value drops it.
func SaveWorkspaceSecrets(agentDir, workspace string, s Secrets) error {
	f := readSecretsFile(agentDir)
	if s.IsZero() {
		delete(f.Workspaces, workspace)
	} else {
		if f.Workspaces == nil {
			f.Workspaces = map[string]Secrets{}
		}
		f.Workspaces[workspace] = s
	}
	return writeSecretsFile(agentDir, f)
}

func writeSecretsFile(agentDir string, f secretsFile) error {
	data, err := json.MarshalIndent(f, "", "  ")
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
	Ref       string    `json:"ref,omitempty"`
	Kind      string    `json:"kind,omitempty"`
	Title     string    `json:"title,omitempty"`
	URL       string    `json:"url,omitempty"`
	StartedAt time.Time `json:"startedAt"`
	// The outcome, filled in when the run ends: what it opened, or why not.
	FinishedAt time.Time `json:"finishedAt,omitempty"`
	PRs        []string  `json:"prs,omitempty"`
	Note       string    `json:"note,omitempty"`
	Error      string    `json:"error,omitempty"`
	// Failure is the shape of the error, so the next run can tell a wall it
	// has already hit from a one-off.
	Failure string `json:"failure,omitempty"`
	// Handover is what the run left for whoever continues the work.
	Handover string `json:"handover,omitempty"`
	// Branch is the worktree branch an isolated run worked on, so undo can
	// release it and a row can say where the code is.
	Branch string `json:"branch,omitempty"`
	// Forgiven marks a failed run an unblock has put behind it, so it no
	// longer counts toward the breaker.
	Forgiven bool `json:"forgiven,omitempty"`
	// Bot is the bot this run ran as, when a bot ran on the event rather
	// than the workspace's fix.
	Bot string `json:"bot,omitempty"`
	// CostUSD and Tokens are what the run said it cost, from claude's own
	// receipt; zero when the run did not say.
	CostUSD float64 `json:"costUSD,omitempty"`
	Tokens  int64   `json:"tokens,omitempty"`
	// SpentPercent is how much of the account's five-hour window this run
	// used, measured across it. Ten comment fixes and ten whole tickets are
	// the same number of runs and nowhere near the same spend.
	SpentPercent int `json:"spentPercent,omitempty"`
}

// Done says the run ended, either way.
func (r FixRecord) Done() bool { return !r.FinishedAt.IsZero() }

// FixLog is <agentDir>/watch/fixes.json: the fixes started, newest last,
// and the events whose fix a cap, quiet hours or a limit deferred — kept
// aside for a manual `corgi agent watch run`, never retried by the daemon
// on its own.
type FixLog struct {
	mu       sync.Mutex
	path     string
	Started  []FixRecord `json:"started"`
	Deferred []Event     `json:"deferred,omitempty"`
	// Blocks are refs taken out of unattended runs: by the breaker after
	// two failures in a row, by a run that said it was blocked, or by a
	// person. Keyed "<workspace>/<ref>". A person unblocks.
	Blocks map[string]Block `json:"blocks,omitempty"`
}

// Block is why a ref is not being worked on, and who said so.
type Block struct {
	Reason string    `json:"reason"`
	By     string    `json:"by"` // breaker, run, person
	At     time.Time `json:"at"`
}

const (
	BlockedByBreaker = "breaker"
	BlockedByRun     = "run"
	BlockedByPerson  = "person"
	// BreakerAfter is how many failed runs in a row on one ref trip it.
	BreakerAfter = 2
)

func blockKey(workspace, ref string) string { return workspace + "/" + strings.TrimSpace(ref) }

// Block takes a ref out of unattended runs until someone unblocks it.
func (l *FixLog) Block(workspace, ref, reason, by string, now time.Time) {
	if strings.TrimSpace(ref) == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.Blocks == nil {
		l.Blocks = map[string]Block{}
	}
	l.Blocks[blockKey(workspace, ref)] = Block{Reason: strings.TrimSpace(reason), By: by, At: now}
	_ = l.save()
}

// Unblock lets runs on the ref start again. The failures that tripped the
// breaker are forgotten by time: only runs after now count toward the next.
func (l *FixLog) Unblock(workspace, ref string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := blockKey(workspace, ref)
	if _, ok := l.Blocks[key]; !ok {
		return false
	}
	delete(l.Blocks, key)
	for i := range l.Started {
		if l.Started[i].Workspace == workspace && l.Started[i].Ref == ref && l.Started[i].Done() {
			l.Started[i].Forgiven = true
		}
	}
	_ = l.save()
	return true
}

// Blocked says whether a ref is out, and why.
func (l *FixLog) Blocked(workspace, ref string) (Block, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.Blocks[blockKey(workspace, ref)]
	return b, ok
}

// FailedInARow is how many of the newest finished runs on a ref ended in
// an error, stopping at the first that did not. A run that was not started
// (deferred, refused) does not count: it did not try.
func (l *FixLog) FailedInARow(workspace, ref string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for i := len(l.Started) - 1; i >= 0; i-- {
		r := l.Started[i]
		if r.Workspace != workspace || r.Ref != ref || !r.Done() {
			continue
		}
		if strings.HasPrefix(r.Error, "not started") {
			continue
		}
		if r.Error == "" || r.Forgiven {
			break
		}
		n++
	}
	return n
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
	l.StartFor(Event{Key: key, Workspace: workspace}, at)
}

// StartFor records a fix with enough of the event to report it later.
func (l *FixLog) StartFor(e Event, at time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Started = append(l.Started, FixRecord{
		Key: e.Key, Workspace: e.Workspace, Ref: e.Ref, Kind: string(e.Kind),
		Title: e.Title, URL: e.URL, StartedAt: at,
	})
	l.dropDeferred(e.Key)
	_ = l.save()
}

// Finish writes the outcome onto the newest unfinished run for the key, so
// what a fix opened outlives the notification that announced it.
// Finish writes the outcome; failure is the error text, classified on the
// way in so the next run can recognise a wall it has already hit.
func (l *FixLog) Finish(key string, prs []string, note, failure string, at time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.Started) - 1; i >= 0; i-- {
		if l.Started[i].Key != key || l.Started[i].Done() {
			continue
		}
		l.Started[i].FinishedAt = at
		l.Started[i].PRs = prs
		l.Started[i].Note = note
		l.Started[i].Error = failure
		l.Started[i].Failure = ClassifyFailure(failure, note)
		_ = l.save()
		return
	}
}

// Interrupted closes every run that never reported an outcome and returns
// their events' keys. A daemon killed mid-fix — a crash, a reboot, a laptop
// closed — otherwise leaves the record "running" for good, and the event is
// already in the seen list, so nobody would ever hear about that issue again.
func (l *FixLog) Interrupted(reason string, at time.Time) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var keys []string
	for i := range l.Started {
		if l.Started[i].Done() {
			continue
		}
		l.Started[i].FinishedAt = at
		l.Started[i].Error = reason
		keys = append(keys, l.Started[i].Key)
	}
	if len(keys) > 0 {
		_ = l.save()
	}
	return keys
}

// RecentFixes is the newest runs first, at most limit, optionally one
// workspace's.
func (l *FixLog) RecentFixes(workspace string, limit int) []FixRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := []FixRecord{}
	for i := len(l.Started) - 1; i >= 0 && len(out) < limit; i-- {
		if workspace != "" && l.Started[i].Workspace != workspace {
			continue
		}
		out = append(out, l.Started[i])
	}
	return out
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

// DropDeferred forgets a waiting event: its fix is no longer worth starting.
func (l *FixLog) DropDeferred(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.dropDeferred(key)
	_ = l.save()
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

// RecentEvents is the tail of the events log, newest first, at most limit,
// with your own tasks ahead of it: they are on the board until finished,
// however much else has arrived since. A line that no longer parses is
// skipped rather than failing the read.
func RecentEvents(agentDir string, limit int) []Event {
	out := TaskEvents(agentDir, time.Now())
	if len(out) > limit {
		out = out[:limit]
	}
	data, err := os.ReadFile(filepath.Join(agentDir, "watch", "events.jsonl"))
	if err != nil {
		return out
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	seen := map[string]struct{}{}
	for _, e := range out {
		seen[e.Key] = struct{}{}
	}
	for i := len(lines) - 1; i >= 0 && len(out) < limit; i-- {
		var e Event
		if json.Unmarshal([]byte(lines[i]), &e) != nil || e.Key == "" {
			continue
		}
		if _, dup := seen[e.Key]; dup {
			continue
		}
		seen[e.Key] = struct{}{}
		out = append(out, e)
	}
	return out
}

// FindEvent is one logged event by key, for acting on it later.
func FindEvent(agentDir, key string) (Event, bool) {
	for _, e := range RecentEvents(agentDir, 500) {
		if e.Key == key {
			return e, true
		}
	}
	return Event{}, false
}

// Outcome is one phrase for what a run ended up doing, so every reader of the
// log says the same thing about the same run instead of inventing wording.
func (r FixRecord) Outcome() string {
	switch {
	case !r.Done():
		return "running"
	case r.Error != "":
		return r.Error
	case len(r.PRs) == 1:
		return "opened 1 PR"
	case len(r.PRs) > 1:
		return fmt.Sprintf("opened %d PRs", len(r.PRs))
	case r.Note != "":
		return r.Note
	default:
		return "nothing opened"
	}
}

// FixesSince is every run started at or after since, newest first, across
// workspaces.
func (l *FixLog) FixesSince(since time.Time) []FixRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := []FixRecord{}
	for i := len(l.Started) - 1; i >= 0; i-- {
		if l.Started[i].StartedAt.Before(since) {
			continue
		}
		out = append(out, l.Started[i])
	}
	return out
}

// EventsSince is what the watch saw at or after since, newest first. The log
// is capped, so scan is bounded whatever the window asks for.
func EventsSince(agentDir string, since time.Time) []Event {
	out := []Event{}
	for _, e := range RecentEvents(agentDir, 2000) {
		if e.At.Before(since) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// SameRefThisRound counts how many events about the same ref this workspace
// has already taken in the current round, and records this one. A reviewer
// leaving four comments on one pull request is one thing to look at, not
// four notifications and certainly not four unattended runs.
//
// Reset by NewRound at the top of every poll, so a comment tomorrow is news
// again even though the ref is the same.
func (s *State) SameRefThisRound(workspace string, e Event) int {
	if e.Ref == "" || e.Kind == KindIssueNew {
		return 0 // a new issue is one event by construction
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.roundRefs == nil {
		s.roundRefs = map[string]int{}
	}
	key := workspace + "\x00" + e.Ref
	n := s.roundRefs[key]
	s.roundRefs[key] = n + 1
	return n
}

// NewRound forgets what the last poll saw, so the collapsing above is
// per-round rather than for ever.
func (s *State) NewRound() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.roundRefs = nil
}

// FailureKinds classify why a run ended badly, because the text of an error
// is not something to match on twice. Only the blocking ones matter: a run
// that failed on a missing credential will fail the same way in an hour.
const (
	FailureNone       = ""
	FailureNoAuth     = "no-credential"
	FailurePermission = "permission"
	FailureTimeout    = "timeout"
	FailureOther      = "other"
)

// ClassifyFailure names the shape of a failure from what the run said.
func ClassifyFailure(reason, output string) string {
	if strings.TrimSpace(reason) == "" {
		return FailureNone
	}
	text := strings.ToLower(reason + " " + output)
	switch {
	case containsAny(text, "no credential", "not authenticated", "unauthorized", "401", "invalid token", "authentication failed", "please run /login"):
		return FailureNoAuth
	case containsAny(text, "permission denied", "403", "forbidden", "not permitted", "requires approval"):
		return FailurePermission
	case containsAny(text, "context deadline exceeded", "timed out", "timeout", "signal: killed"):
		return FailureTimeout
	}
	return FailureOther
}

// Blocking says a failure of this shape will happen again until a person
// changes something, so trying again only spends the budget.
func Blocking(kind string) bool {
	return kind == FailureNoAuth || kind == FailurePermission
}

func containsAny(text string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(text, n) {
			return true
		}
	}
	return false
}

// RecentBlocker is the blocking failure this workspace keeps hitting, with
// how many runs in a row hit it, or "" when the last run was fine. Only a
// run in the window counts: a credential fixed yesterday is not news.
func (l *FixLog) RecentBlocker(workspace string, within time.Duration, now time.Time) (kind string, runs int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.Started) - 1; i >= 0; i-- {
		r := l.Started[i]
		if r.Workspace != workspace || !r.Done() {
			continue
		}
		if now.Sub(r.FinishedAt) > within {
			break
		}
		if !Blocking(r.Failure) {
			return "", 0 // a run that got somewhere clears the record
		}
		if kind == "" {
			kind = r.Failure
		}
		if r.Failure != kind {
			break
		}
		runs++
	}
	return kind, runs
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Handover is what a run leaves for whoever picks the work up next: the last
// thing it said before it stopped. A run that ends — finished, failed, or
// killed with the laptop lid — otherwise takes twenty minutes of context with
// it, and the next one starts from the ticket again.
const handoverMax = 700

// SetBranch records the worktree branch an isolated run works on.
func (l *FixLog) SetBranch(key, branch string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.Started) - 1; i >= 0; i-- {
		if l.Started[i].Key == key {
			l.Started[i].Branch = branch
			_ = l.save()
			return
		}
	}
}

// SetHandover records what a run left behind, on the newest run for the key.
func (l *FixLog) SetHandover(key, text string, at time.Time) {
	text = strings.TrimSpace(text)
	if key == "" || text == "" {
		return
	}
	if len(text) > handoverMax {
		text = "…" + text[len(text)-handoverMax:]
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.Started) - 1; i >= 0; i-- {
		if l.Started[i].Key == key {
			l.Started[i].Handover = text
			_ = l.save()
			return
		}
	}
}

// LastHandover is what the newest earlier run on this ref left behind, so a
// second attempt starts where the first stopped rather than at the ticket.
func (l *FixLog) LastHandover(workspace, ref string) string {
	if ref == "" {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.Started) - 1; i >= 0; i-- {
		r := l.Started[i]
		if r.Workspace == workspace && r.Ref == ref && r.Done() && r.Handover != "" {
			return r.Handover
		}
	}
	return ""
}

// TailLines is the last n non-empty lines of a run's output, which is where
// a claude session says what it did and what it could not do.
func TailLines(out string, n int) string {
	var kept []string
	lines := strings.Split(out, "\n")
	for i := len(lines) - 1; i >= 0 && len(kept) < n; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			kept = append([]string{line}, kept...)
		}
	}
	return strings.Join(kept, "\n")
}

// SetBot names the bot a run ran as.
func (l *FixLog) SetBot(key, bot string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.Started) - 1; i >= 0; i-- {
		if l.Started[i].Key == key {
			l.Started[i].Bot = bot
			_ = l.save()
			return
		}
	}
}

// SetCost records claude's receipt on the newest run for the key.
func (l *FixLog) SetCost(key string, usd float64, tokens int64) {
	if key == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.Started) - 1; i >= 0; i-- {
		if l.Started[i].Key == key {
			l.Started[i].CostUSD, l.Started[i].Tokens = usd, tokens
			_ = l.save()
			return
		}
	}
}

// Cost is what every run on a ticket cost, added up.
type Cost struct {
	USD    float64 `json:"usd"`
	Tokens int64   `json:"tokens"`
	Runs   int     `json:"runs"`
}

// CostFor adds up the runs on a ref.
func (l *FixLog) CostFor(workspace, ref string) Cost {
	l.mu.Lock()
	defer l.mu.Unlock()
	var c Cost
	for _, r := range l.Started {
		if r.Workspace != workspace || r.Ref != ref || strings.HasPrefix(r.Error, "not started") {
			continue
		}
		c.Runs++
		c.USD += r.CostUSD
		c.Tokens += r.Tokens
	}
	return c
}

// SetSpent records what a run cost, as a share of the account's five-hour
// window. Only a positive, believable figure is kept: the window resetting
// mid-run reads as a negative, and a run that spanned a reset cannot be
// measured this way at all.
func (l *FixLog) SetSpent(key string, percent int) {
	if key == "" || percent <= 0 || percent > 100 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.Started) - 1; i >= 0; i-- {
		if l.Started[i].Key == key {
			l.Started[i].SpentPercent = percent
			_ = l.save()
			return
		}
	}
}

// TypicalSpend is what a run in this workspace usually costs, as a share of
// the five-hour window: the median of what has been measured, or 0 when
// nothing has. The median rather than the mean, because one runaway run
// should not make every later one look unaffordable.
func (l *FixLog) TypicalSpend(workspace string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	var seen []int
	for _, r := range l.Started {
		if r.Workspace == workspace && r.SpentPercent > 0 {
			seen = append(seen, r.SpentPercent)
		}
	}
	if len(seen) == 0 {
		return 0
	}
	sort.Ints(seen)
	return seen[len(seen)/2]
}
