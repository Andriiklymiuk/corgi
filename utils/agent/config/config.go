package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type RepoConfig struct {
	Version   int           `yaml:"version"`
	Workspace RepoWorkspace `yaml:"workspace"`
}

type RepoWorkspace struct {
	ID        string   `yaml:"id"`
	Aliases   []string `yaml:"aliases"`
	Sensitive bool     `yaml:"sensitive"`
}

type UserConfig struct {
	Version      int                        `yaml:"version"`
	Workspaces   map[string]WorkspaceConfig `yaml:"workspaces"`
	Defaults     WorkspaceConfig            `yaml:"defaults"`
	NotifyUrl    string                     `yaml:"notifyUrl"`
	DigestAt     string                     `yaml:"digestAt"`
	StayAwake    bool                       `yaml:"stayAwake"`
	PulseUrl     string                     `yaml:"pulseUrl,omitempty"`
	KeepDisplay  bool                       `yaml:"keepDisplay,omitempty"`
	AutoContinue bool                       `yaml:"autoContinue"`
	Profiles     map[string]WorkspaceConfig `yaml:"profiles"`
	TrackSlots   int                        `yaml:"trackSlots,omitempty"`
	SessionCap   int64                      `yaml:"sessionCap,omitempty"`
	Stream       []string                   `yaml:"stream,omitempty"`
}

func (u *UserConfig) StreamAllowed(workspace string) bool {
	if u == nil {
		return false
	}
	for _, w := range u.Stream {
		if w == "*" || (w != "" && w == workspace) {
			return true
		}
	}
	return false
}

type WorkspaceConfig struct {
	Autostart        *bool  `yaml:"autostart"`
	AutostartSession *bool  `yaml:"autostartSession"`
	Kind             string `yaml:"kind"`
	Bin              string `yaml:"bin"`
	// Trusted config only: argv picks what code runs, a committed repo file must never reach it.
	Args                       []string     `yaml:"args"`
	ConfigDirEnv               string       `yaml:"configDirEnv"`
	CredentialEnv              []string     `yaml:"credentialEnv"`
	Spawn                      string       `yaml:"spawn"`
	Capacity                   int          `yaml:"capacity"`
	PermissionMode             string       `yaml:"permissionMode"`
	ConfigDir                  string       `yaml:"configDir"`
	Accounts                   []string     `yaml:"accounts"`
	WakeLock                   string       `yaml:"wakeLock"`
	InheritAPIKey              bool         `yaml:"inheritApiKey"`
	InheritOAuthToken          bool         `yaml:"inheritOauthToken"`
	Watch                      *WatchConfig `yaml:"watch"`
	Models                     *ModelPolicy `yaml:"models"`
	Routines                   []Routine    `yaml:"routines,omitempty"`
	DangerouslySkipPermissions bool         `yaml:"dangerouslySkipPermissions"`
}

func LoadRepo(dir string) (*RepoConfig, error) {
	path := filepath.Join(dir, ".corgi", "agent.yml")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c RepoConfig
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &c, nil
}

func LoadUser(path string) (*UserConfig, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return &UserConfig{Workspaces: map[string]WorkspaceConfig{}}, nil
	}
	if err != nil {
		return nil, err
	}
	if runtime.GOOS != "windows" {
		if mode := info.Mode().Perm(); mode&0o077 != 0 {
			return nil, fmt.Errorf(
				"%s is readable by other users (mode %04o) — run: chmod 600 %s",
				path, mode, path)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c UserConfig
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if c.Defaults.DangerouslySkipPermissions {
		return nil, fmt.Errorf(
			"%s: dangerouslySkipPermissions cannot be set under defaults: — set it per-workspace or per-profile, so each bypass is a deliberate opt-in with an opt-out",
			path)
	}
	if c.Workspaces == nil {
		c.Workspaces = map[string]WorkspaceConfig{}
	}
	return &c, nil
}

type Resolved struct {
	ID             string
	RepoDeclaredID string
	Aliases        []string
	Sensitive      bool
	WorkspaceConfig
}

func Resolve(id string, repo *RepoConfig, user *UserConfig) Resolved {
	out := Resolved{ID: id}

	if repo != nil {
		out.RepoDeclaredID = repo.Workspace.ID
		out.Aliases = repo.Workspace.Aliases
		out.Sensitive = repo.Workspace.Sensitive
	}

	if user == nil {
		return out
	}
	out.WorkspaceConfig = user.Defaults
	if specific, ok := user.Workspaces[out.ID]; ok {
		out.WorkspaceConfig = overlay(out.WorkspaceConfig, specific)
	}
	return out
}

type ChatConfig struct {
	Slack *SlackWatch `yaml:"slack,omitempty"`
}

type SlackWatch struct {
	Mentions       bool     `yaml:"mentions"`
	Channels       []string `yaml:"channels,omitempty"`
	ReviewChannels []string `yaml:"reviewChannels,omitempty"`
	Trust          []string `yaml:"trust,omitempty"`
	PostTo         string   `yaml:"postTo,omitempty"`
	ReplyAs        string   `yaml:"replyAs,omitempty"`
}

func (s *SlackWatch) MayRun(author string) bool {
	if s == nil || len(s.Trust) == 0 {
		return false
	}
	who := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(author), "@"))
	if who == "" {
		return false
	}
	for _, w := range s.Trust {
		if strings.ToLower(strings.TrimPrefix(strings.TrimSpace(w), "@")) == who {
			return true
		}
	}
	return false
}

type WatchConfig struct {
	Enabled         bool        `yaml:"enabled"`
	Interval        string      `yaml:"interval"`
	Tracker         string      `yaml:"tracker"`
	Project         string      `yaml:"project"`
	Labels          []string    `yaml:"labels"`
	States          []string    `yaml:"states"`
	Assignee        string      `yaml:"assignee"`
	Comments        bool        `yaml:"comments"`
	PRs             bool        `yaml:"prs"`
	Repos           []string    `yaml:"repos"`
	Action          string      `yaml:"action"`
	MaxFixesPerHour int         `yaml:"maxFixesPerHour,omitempty"`
	MaxFixesPerDay  int         `yaml:"maxFixesPerDay,omitempty"`
	MaxFixesTotal   int         `yaml:"maxFixesTotal,omitempty"`
	CapSince        time.Time   `yaml:"capSince,omitempty"`
	Quiet           string      `yaml:"quiet,omitempty"`
	DaysOff         []string    `yaml:"daysOff,omitempty"`
	Lease           bool        `yaml:"lease,omitempty"`
	NoRetry         bool        `yaml:"noRetry,omitempty"`
	Isolate         bool        `yaml:"isolate,omitempty"`
	PruneAfter      string      `yaml:"pruneAfter,omitempty"`
	DayCap          int64       `yaml:"dayCap,omitempty"`
	Headless        bool        `yaml:"headless,omitempty"`
	RerunCI         bool        `yaml:"rerunCI,omitempty"`
	Chat            *ChatConfig `yaml:"chat,omitempty"`
	Silent          bool        `yaml:"silent,omitempty"`
	AutoCarry       bool        `yaml:"autoCarry,omitempty"`
	Slots           int         `yaml:"slots,omitempty"`
	Reviews         bool        `yaml:"reviews,omitempty"`
	CI              bool        `yaml:"ci,omitempty"`
	From            []string    `yaml:"from,omitempty"`
	Bots            bool        `yaml:"bots,omitempty"`
	FixKinds        []string    `yaml:"fixKinds,omitempty"`
	ReviewStatus    string      `yaml:"reviewStatus,omitempty"`
	PickupStatus    string      `yaml:"pickupStatus,omitempty"`
	AutoMerge       bool        `yaml:"autoMerge,omitempty"`
	Approve         bool        `yaml:"approve,omitempty"`
	HandOver        bool        `yaml:"handOver,omitempty"`
	AutoAllow       string      `yaml:"autoAllow,omitempty"`
	DoneWhen        []string    `yaml:"doneWhen,omitempty"`
	CompactAt       int         `yaml:"compactAt,omitempty"`
	Rebase          bool        `yaml:"rebase,omitempty"`
	Lessons         bool        `yaml:"lessons,omitempty"`
	PlanReview      string      `yaml:"planReview,omitempty"`
}

const AutoAllowReads = "reads"

const PlanReviewAlways = "always"

func ParsePlanReview(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "", "off", "none", "no":
		return "", nil
	case PlanReviewAlways:
		return PlanReviewAlways, nil
	}
	if n, ok := planReviewThreshold(s); ok {
		return fmt.Sprintf("risk>=%d", n), nil
	}
	return "", fmt.Errorf("plan-review is off, always or risk>=N (1 to 10), not %q", s)
}

func PlanReviewRequired(policy string, risk int) bool {
	switch policy {
	case "":
		return false
	case PlanReviewAlways:
		return true
	}
	n, ok := planReviewThreshold(policy)
	return ok && risk >= n
}

func planReviewThreshold(s string) (int, bool) {
	rest, ok := strings.CutPrefix(strings.ReplaceAll(strings.ToLower(s), " ", ""), "risk>=")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n < 1 || n > 10 {
		return 0, false
	}
	return n, true
}

func ParseAutoAllow(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "off", "none", "no":
		return "", nil
	case AutoAllowReads:
		return AutoAllowReads, nil
	}
	return "", fmt.Errorf("auto-allow is reads or off, not %q", s)
}

type Routine struct {
	Name     string `yaml:"name"`
	Kind     string `yaml:"kind,omitempty"`
	Prompt   string `yaml:"prompt,omitempty"`
	Schedule string `yaml:"schedule"`
	Model    string `yaml:"model,omitempty"`
	Bot      string `yaml:"bot,omitempty"`
	Off      bool   `yaml:"off,omitempty"`
}

type ModelPolicy struct {
	Auto     string            `yaml:"auto,omitempty"`
	Plan     string            `yaml:"plan,omitempty"`
	Execute  string            `yaml:"execute,omitempty"`
	Review   string            `yaml:"review,omitempty"`
	Triage   string            `yaml:"triage,omitempty"`
	Escalate string            `yaml:"escalate,omitempty"`
	Kinds    map[string]string `yaml:"kinds,omitempty"`
}

const (
	ModelAutoDefault     = "opusplan"
	ModelPlanDefault     = "opus"
	ModelExecuteDefault  = "sonnet"
	ModelReviewDefault   = "opus"
	ModelTriageDefault   = "haiku"
	ModelEscalateDefault = "opus"
)

func (m *ModelPolicy) ForAuto() string {
	return pick(m, func(p ModelPolicy) string { return p.Auto }, ModelAutoDefault)
}

func (m *ModelPolicy) ForKind(kind string) string {
	if m != nil && m.Kinds != nil {
		if v := strings.TrimSpace(m.Kinds[kind]); v != "" {
			return v
		}
	}
	switch kind {
	case "issue.new":
		return pick(m, func(p ModelPolicy) string { return p.Plan }, ModelPlanDefault)
	case "review.requested":
		return pick(m, func(p ModelPolicy) string { return p.Review }, ModelReviewDefault)
	default:
		return pick(m, func(p ModelPolicy) string { return p.Execute }, ModelExecuteDefault)
	}
}

func (m *ModelPolicy) ForEscalation() string {
	return pick(m, func(p ModelPolicy) string { return p.Escalate }, ModelEscalateDefault)
}

func pick(m *ModelPolicy, get func(ModelPolicy) string, def string) string {
	if m != nil {
		if v := strings.TrimSpace(get(*m)); v != "" {
			return v
		}
	}
	return def
}

func overlayModels(base, over *ModelPolicy) *ModelPolicy {
	if over == nil {
		return base
	}
	if base == nil {
		return over
	}
	merged := *base
	for _, f := range []struct {
		dst *string
		src string
	}{
		{&merged.Auto, over.Auto}, {&merged.Plan, over.Plan}, {&merged.Execute, over.Execute},
		{&merged.Review, over.Review}, {&merged.Triage, over.Triage}, {&merged.Escalate, over.Escalate},
	} {
		if strings.TrimSpace(f.src) != "" {
			*f.dst = f.src
		}
	}
	if len(over.Kinds) > 0 {
		merged.Kinds = map[string]string{}
		for k, v := range base.Kinds {
			merged.Kinds[k] = v
		}
		for k, v := range over.Kinds {
			merged.Kinds[k] = v
		}
	}
	return &merged
}

func overlayWatch(base, over *WatchConfig) *WatchConfig {
	if over == nil {
		return base
	}
	if base == nil {
		return over
	}
	merged := *over
	if merged.MaxFixesPerHour == 0 {
		merged.MaxFixesPerHour = base.MaxFixesPerHour
	}
	if merged.MaxFixesPerDay == 0 {
		merged.MaxFixesPerDay = base.MaxFixesPerDay
	}
	if merged.Quiet == "" {
		merged.Quiet = base.Quiet
	}
	if len(merged.DaysOff) == 0 {
		merged.DaysOff = base.DaysOff
	}
	if merged.PickupStatus == "" {
		merged.PickupStatus = base.PickupStatus
	}
	if merged.ReviewStatus == "" {
		merged.ReviewStatus = base.ReviewStatus
	}
	if merged.PlanReview == "" {
		merged.PlanReview = base.PlanReview
	}
	if len(merged.FixKinds) == 0 {
		merged.FixKinds = base.FixKinds
	}
	if len(merged.From) == 0 {
		merged.From = base.From
	}
	if merged.Chat == nil {
		merged.Chat = base.Chat
	}
	return &merged
}

func overlay(base, over WorkspaceConfig) WorkspaceConfig {
	base = overlayLaunch(base, over)
	base = overlayAccount(base, over)
	base.Watch = overlayWatch(base.Watch, over.Watch)
	base.Models = overlayModels(base.Models, over.Models)
	if len(over.Routines) > 0 {
		base.Routines = over.Routines
	}
	base.InheritAPIKey = base.InheritAPIKey || over.InheritAPIKey
	base.InheritOAuthToken = base.InheritOAuthToken || over.InheritOAuthToken
	base.DangerouslySkipPermissions = base.DangerouslySkipPermissions || over.DangerouslySkipPermissions
	return base
}

func overlayLaunch(base, over WorkspaceConfig) WorkspaceConfig {
	if over.Autostart != nil {
		base.Autostart = over.Autostart
	}
	if over.AutostartSession != nil {
		base.AutostartSession = over.AutostartSession
	}
	if over.Kind != "" {
		base.Kind = over.Kind
	}
	if over.Bin != "" {
		base.Bin = over.Bin
	}
	if len(over.Args) > 0 {
		base.Args = over.Args
	}
	if over.Spawn != "" {
		base.Spawn = over.Spawn
	}
	if over.Capacity != 0 {
		base.Capacity = over.Capacity
	}
	if over.PermissionMode != "" {
		base.PermissionMode = over.PermissionMode
	}
	return base
}

func overlayAccount(base, over WorkspaceConfig) WorkspaceConfig {
	if over.ConfigDirEnv != "" {
		base.ConfigDirEnv = over.ConfigDirEnv
	}
	if len(over.CredentialEnv) > 0 {
		base.CredentialEnv = over.CredentialEnv
	}
	if over.ConfigDir != "" {
		base.ConfigDir = over.ConfigDir
	}
	if len(over.Accounts) > 0 {
		base.Accounts = over.Accounts
	}
	if over.WakeLock != "" {
		base.WakeLock = over.WakeLock
	}
	return base
}

func ApplyProfile(r Resolved, user *UserConfig, name string) (Resolved, error) {
	if name == "" {
		return r, nil
	}
	if user == nil || len(user.Profiles) == 0 {
		return r, fmt.Errorf("no profiles defined — add a profiles: section to the agent config")
	}
	p, ok := user.Profiles[name]
	if !ok {
		names := make([]string, 0, len(user.Profiles))
		for n := range user.Profiles {
			names = append(names, n)
		}
		sort.Strings(names)
		return r, fmt.Errorf("unknown profile %q (defined: %s)", name, strings.Join(names, ", "))
	}
	r.WorkspaceConfig = overlay(r.WorkspaceConfig, p)
	return r, nil
}

func (r Resolved) AutostartSessionEnabled() bool {
	return r.AutostartSession != nil && *r.AutostartSession
}

func (r Resolved) AutostartEnabled() bool {
	return r.Autostart != nil && *r.Autostart
}
