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

type Kind string

const (
	KindIssueNew        Kind = "issue.new"
	KindIssueComment    Kind = "issue.comment"
	KindPRComment       Kind = "pr.comment"
	KindPRReview        Kind = "pr.review"
	KindReviewRequested Kind = "review.requested"
	KindCIFailed        Kind = "ci.failed"
	KindChatMention     Kind = "chat.mention"
	KindChatMessage     Kind = "chat.message"
)

type Event struct {
	Key       string   `json:"key"`
	Source    string   `json:"source"`
	Kind      Kind     `json:"kind"`
	Workspace string   `json:"workspace,omitempty"`
	Ref       string   `json:"ref"`
	Title     string   `json:"title"`
	Body      string   `json:"body,omitempty"`
	URL       string   `json:"url,omitempty"`
	Author    string   `json:"author,omitempty"`
	Labels    []string `json:"labels,omitempty"`
	Links     []string `json:"links,omitempty"`
	State     string   `json:"state,omitempty"`
	Assignee  string   `json:"assignee,omitempty"`
	Mine      bool     `json:"mine,omitempty"`
	Self      bool     `json:"self,omitempty"`
	Bot       bool     `json:"bot,omitempty"`
	// A subtask names its parent; a parent lists its open subtasks of mine.
	Parent      string    `json:"parent,omitempty"`
	ParentTitle string    `json:"parentTitle,omitempty"`
	Subtasks    []string  `json:"subtasks,omitempty"`
	At          time.Time `json:"at"`
	// A deferred run that must not start before this moment.
	NotBefore time.Time `json:"notBefore,omitempty"`
}

type Rules struct {
	Enabled  bool
	Labels   []string
	States   []string
	Assignee string
	Comments bool
	PRs      bool
	CI       bool
	Reviews  bool
	From     []string
	Bots     bool
	Mentions bool
	Channels []string
}

func (r Rules) Match(e Event) bool { return r.Why(e) == "" }

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
		case KindChatMention:
			return "chat mentions need --mentions"
		case KindChatMessage:
			return "a channel message needs that channel in --channel"
		}
		return "kind " + string(e.Kind) + " is not watched"
	}
	switch e.Kind {
	case KindReviewRequested:
		if len(r.States) == 0 {
			if over := finishedState(e.State); over != "" {
				return "it is " + over + " — there is nothing to review"
			}
		}
	case KindChatMention, KindChatMessage:
		if e.Self {
			return "I wrote it"
		}
		if e.Bot && !r.Bots {
			return "it is from a bot; --bots makes those count"
		}
		if e.Kind == KindChatMessage && !containsFold(r.Channels, e.State) {
			return fmt.Sprintf("%s is not a channel this workspace listens to", orNone([]string{e.State}))
		}
		if len(r.From) > 0 && !matchesPerson(e.Author, r.From) {
			return fmt.Sprintf("it is from %s, and you are waiting on %s",
				orNone([]string{e.Author}), strings.Join(r.From, ", "))
		}
	case KindCIFailed:
		if !e.Mine {
			return "not on something of mine"
		}
	case KindPRComment, KindPRReview, KindIssueComment:
		if !e.Mine {
			return "not on something of mine"
		}
		if len(r.States) == 0 {
			if over := finishedState(e.State); over != "" {
				return "it is " + over + " — the comment is not work"
			}
		}
		if e.Kind != KindPRReview && IsAcknowledgement(e.Body) {
			return "it is a thank-you or a sign-off, not a request"
		}
		if e.Bot && !r.Bots {
			return "it is from a bot; --bots makes those count"
		}
		if len(r.From) > 0 && !matchesPerson(e.Author, r.From) {
			return fmt.Sprintf("it is from %s, and you are waiting on %s",
				orNone([]string{e.Author}), strings.Join(r.From, ", "))
		}
	case KindIssueNew:
		if dead := finishedState(e.State); dead != "" {
			if len(r.States) == 0 {
				return "it is " + dead + " — nobody is going to act on it"
			}
		}
		if r.Assignee != "any" && !e.Mine {
			return "not assigned to me (--assignee any takes every issue)"
		}
		if len(e.Subtasks) > 0 {
			return "its open subtasks are the work: " + strings.Join(e.Subtasks, ", ")
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

var deadStates = map[string]string{
	"duplicate": "a duplicate", "duplicated": "a duplicate",
	"canceled": "cancelled", "cancelled": "cancelled",
	"won't do": "not being done", "wont do": "not being done",
	"not planned": "not planned", "obsolete": "obsolete",
	"rejected": "rejected",
}

var closedStates = map[string]string{
	"done": "done", "closed": "closed", "resolved": "resolved",
	"complete": "done", "completed": "done", "shipped": "shipped",
	"released": "released", "merged": "merged", "to release": "waiting on a release",
	"locked":   "locked",
	"verified": "verified", "deployed": "deployed", "in production": "in production",
	"on production": "in production", "live": "live", "qa passed": "past QA", "tested": "tested",
}

var (
	ackWords = regexp.MustCompile(`(?i)\b(thanks?|thank you|thx|ty|ok|okay|lgtm|works?|working|good|great|perfect|nice|awesome|approved?|merged|done|confirmed|verified|passed|passing|green|fixed|resolved|all good|looks good)\b|👍|✅|🙏|🎉|👌`)
	askWords = regexp.MustCompile(`(?i)\?|\b(could|can|would|please|pls|should|need|needs|must|why|how|what|when|where|fix|change|update|add|remove|revert|still|but|however|not|doesn't|does not|isn't|is not|broken|fails?|failing|error|bug|wrong|missing)\b`)
)

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

func FinishedState(state string) string { return finishedState(state) }

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

func finishedState(state string) string {
	key := strings.ToLower(strings.TrimSpace(state))
	if why, ok := deadStates[key]; ok {
		return why
	}
	return closedStates[key]
}

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
	case KindChatMention:
		return r.Mentions
	case KindChatMessage:
		return len(r.Channels) > 0
	}
	return false
}

func (r Rules) MatchesNothing() bool { return !r.Enabled }

var sourceKinds = map[string][]Kind{
	"linear": {KindIssueNew, KindIssueComment},
	"jira":   {KindIssueNew, KindIssueComment},
	"github": {KindPRComment, KindPRReview, KindReviewRequested, KindCIFailed},
	"gitlab": {KindPRComment, KindPRReview, KindReviewRequested},
	"slack":  {KindChatMention, KindChatMessage, KindReviewRequested},
}

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

type Cursor map[string]string

type Source interface {
	Name() string
	Poll(ctx context.Context, cursor Cursor) ([]Event, Cursor, error)
}

type Sink func(ctx context.Context, e Event)

type State struct {
	mu        sync.Mutex
	path      string
	agentDir  string
	Cursors   map[string]Cursor `json:"cursors"`
	Seen      []string          `json:"seen"`
	Errors    map[string]string `json:"errors,omitempty"`
	Polled    map[string]string `json:"polled,omitempty"`
	Held      []HeldNote        `json:"held,omitempty"`
	Ignored   []string          `json:"ignored,omitempty"`
	seen      map[string]struct{}
	roundRefs map[string]int
	Fixes     *FixLog `json:"-"`
}

const seenKeep = 2000

type HeldNote struct {
	Workspace string    `json:"workspace"`
	Body      string    `json:"body"`
	At        time.Time `json:"at"`
	Key       string    `json:"key,omitempty"`
}

const heldKeep = 100

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

func (s *State) Hold(workspace, body string, at time.Time) {
	s.HoldEvent(workspace, "", body, at)
}

func (s *State) HoldEvent(workspace, key, body string, at time.Time) {
	s.mu.Lock()
	s.Held = append(s.Held, HeldNote{Workspace: workspace, Key: key, Body: body, At: at})
	if len(s.Held) > heldKeep {
		s.Held = s.Held[len(s.Held)-heldKeep:]
	}
	s.mu.Unlock()
	_ = s.save()
}

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

func (s *State) Thread(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.Cursors {
		if v := c["thread:"+key]; v != "" {
			return v
		}
	}
	return ""
}

func (s *State) SourceIdentity(source, field string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, c := range s.Cursors {
		if strings.HasSuffix(key, "/"+source) && c[field] != "" {
			return c[field]
		}
	}
	return ""
}

func (s *State) SetThreadForTest(key, parent string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Cursors == nil {
		s.Cursors = map[string]Cursor{}
	}
	if s.Cursors["test/slack"] == nil {
		s.Cursors["test/slack"] = Cursor{}
	}
	s.Cursors["test/slack"]["thread:"+key] = parent
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

type Watch struct {
	Workspace string
	Rules     Rules
	Sources   []Source
	Interval  time.Duration
	State     *State
	Sink      Sink
	Log       func(string)
	wake      chan struct{}
	wakeOnce  sync.Once
	Round     func(now time.Time)
	Asleep    func(now time.Time) bool
}

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

type Secrets struct {
	Linear     string `json:"linear,omitempty"`
	JiraURL    string `json:"jiraUrl,omitempty"`
	JiraEmail  string `json:"jiraEmail,omitempty"`
	JiraToken  string `json:"jiraToken,omitempty"`
	GitHub     string `json:"github,omitempty"`
	GitLab     string `json:"gitlab,omitempty"`
	GitLabURL  string `json:"gitlabUrl,omitempty"`
	SlackUser  string `json:"slackUser,omitempty"`
	SlackBot   string `json:"slackBot,omitempty"`
	HookSecret string `json:"hookSecret,omitempty"`
	Me         string `json:"me,omitempty"`
}

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
	pick(&s.SlackUser, "SLACK_USER_TOKEN")
	pick(&s.SlackBot, "SLACK_BOT_TOKEN")
	return s
}

func LoadSecretsFor(agentDir, workspace string) Secrets {
	return overlaySecrets(LoadSecrets(agentDir), WorkspaceSecrets(agentDir, workspace))
}

func WorkspaceSecrets(agentDir, workspace string) Secrets {
	return readSecretsFile(agentDir).Workspaces[workspace]
}

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
	pick(&base.SlackUser, over.SlackUser)
	pick(&base.SlackBot, over.SlackBot)
	pick(&base.Me, over.Me)
	return base
}

func (s Secrets) IsZero() bool {
	return s == Secrets{}
}

func GitHubToken(s Secrets) (token, source string) {
	if t := strings.TrimSpace(s.GitHub); t != "" {
		return t, "saved"
	}
	if t := strings.TrimSpace(githubAuthToken()); t != "" {
		return t, "gh-auth"
	}
	return "", ""
}

func SaveSecrets(agentDir string, s Secrets) error {
	f := readSecretsFile(agentDir)
	f.Secrets = s
	return writeSecretsFile(agentDir, f)
}

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

func Fingerprint(token string) string {
	if token == "" {
		return "none"
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])[:8]
}

type Summary struct {
	Key    string
	Polled string
	Error  string
}

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

var ErrNoToken = errors.New("no token")

type FixRecord struct {
	Key          string    `json:"key"`
	Workspace    string    `json:"workspace"`
	Ref          string    `json:"ref,omitempty"`
	Kind         string    `json:"kind,omitempty"`
	Title        string    `json:"title,omitempty"`
	URL          string    `json:"url,omitempty"`
	StartedAt    time.Time `json:"startedAt"`
	FinishedAt   time.Time `json:"finishedAt,omitempty"`
	PRs          []string  `json:"prs,omitempty"`
	Note         string    `json:"note,omitempty"`
	Error        string    `json:"error,omitempty"`
	Failure      string    `json:"failure,omitempty"`
	Handover     string    `json:"handover,omitempty"`
	Branch       string    `json:"branch,omitempty"`
	Forgiven     bool      `json:"forgiven,omitempty"`
	Bot          string    `json:"bot,omitempty"`
	Parent       string    `json:"parent,omitempty"`
	CostUSD      float64   `json:"costUSD,omitempty"`
	Tokens       int64     `json:"tokens,omitempty"`
	Retry        string    `json:"retry,omitempty"`
	SpentPercent int       `json:"spentPercent,omitempty"`
}

func (r FixRecord) Done() bool { return !r.FinishedAt.IsZero() }

type FixLog struct {
	mu       sync.Mutex
	path     string
	Started  []FixRecord      `json:"started"`
	Deferred []Event          `json:"deferred,omitempty"`
	Blocks   map[string]Block `json:"blocks,omitempty"`
}

type Block struct {
	Reason string    `json:"reason"`
	By     string    `json:"by"`
	At     time.Time `json:"at"`
}

const (
	BlockedByBreaker = "breaker"
	BlockedByRun     = "run"
	BlockedByPerson  = "person"
	BreakerAfter     = 2
)

func blockKey(workspace, ref string) string { return workspace + "/" + strings.TrimSpace(ref) }

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

func (l *FixLog) Blocked(workspace, ref string) (Block, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.Blocks[blockKey(workspace, ref)]
	return b, ok
}

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

func (l *FixLog) Start(workspace, key string, at time.Time) {
	l.StartFor(Event{Key: key, Workspace: workspace}, at)
}

func (l *FixLog) StartFor(e Event, at time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Started = append(l.Started, FixRecord{
		Key: e.Key, Workspace: e.Workspace, Ref: e.Ref, Kind: string(e.Kind),
		Title: e.Title, URL: e.URL, Parent: e.Parent, StartedAt: at,
	})
	l.dropDeferred(e.Key)
	_ = l.save()
}

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

// The newest run of the workspace whose pull requests include this link.
func (l *FixLog) RunThatOpened(workspace, link string) (FixRecord, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	want := PullRef(link)
	for i := len(l.Started) - 1; i >= 0; i-- {
		r := l.Started[i]
		if workspace != "" && r.Workspace != workspace {
			continue
		}
		for _, pr := range r.PRs {
			if pr == link || (want != "" && PullRef(pr) == want) {
				return r, true
			}
		}
	}
	return FixRecord{}, false
}

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

func (l *FixLog) Defer(e Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.dropDeferred(e.Key)
	l.Deferred = append(l.Deferred, e)
	_ = l.save()
}

func (l *FixLog) DeferredEvents() []Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Event(nil), l.Deferred...)
}

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

func FindEvent(agentDir, key string) (Event, bool) {
	for _, e := range RecentEvents(agentDir, 500) {
		if e.Key == key {
			return e, true
		}
	}
	return Event{}, false
}

func FindEventByRef(agentDir, ref, workspace string) (Event, bool) {
	if ref == "" {
		return Event{}, false
	}
	for _, e := range RecentEvents(agentDir, 500) {
		if strings.EqualFold(e.Ref, ref) && (workspace == "" || e.Workspace == workspace) {
			return e, true
		}
	}
	return Event{}, false
}

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

func (s *State) SameRefThisRound(workspace string, e Event) int {
	if e.Ref == "" || e.Kind == KindIssueNew {
		return 0
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

func (s *State) NewRound() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.roundRefs = nil
}

const (
	FailureNone       = ""
	FailureNoAuth     = "no-credential"
	FailurePermission = "permission"
	FailureTimeout    = "timeout"
	FailureOther      = "other"
)

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
			return "", 0
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

const handoverMax = 700

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

func (l *FixLog) SetRetry(key, model string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.Started) - 1; i >= 0; i-- {
		if l.Started[i].Key == key {
			l.Started[i].Retry = model
			_ = l.save()
			return
		}
	}
}

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

type Cost struct {
	USD    float64 `json:"usd"`
	Tokens int64   `json:"tokens"`
	Runs   int     `json:"runs"`
}

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
