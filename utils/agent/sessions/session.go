package sessions

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils/agent/usage"
)

type Status string

const (
	StatusWorking    Status = "working"
	StatusNeedsInput Status = "needs_input"
	StatusDone       Status = "done"
	StatusStale      Status = "stale"
	StatusGone       Status = "gone"
	StatusUnknown    Status = "unknown"
	StatusLimited    Status = "limited"
)

var (
	limitText    = regexp.MustCompile(`(?i)\b(session|usage|rate|weekly|daily) limit\b|\brate.?limited?\b`)
	limitReset   = regexp.MustCompile(`(?i)\bresets?\s+(?:at\s+)?([^\n"}\\]+?)\s*(?:$|["}\\])`)
	overloadText = regexp.MustCompile(`(?i)\boverloaded?\b|\b529\b|\bcapacity\b|\btry again (later|in)\b|\btemporarily unavailable\b`)
	quotaText    = regexp.MustCompile(`(?i)\b(session|usage|weekly|daily) limit\b|\bresets?\s`)
)

type LimitKind string

const (
	LimitQuota    LimitKind = "quota"
	LimitOverload LimitKind = "overload"
)

func ClassifyLimit(errorType, message string) (LimitKind, string, bool) {
	if errorType != "rate_limit" && !limitText.MatchString(message) && !overloadText.MatchString(message) {
		return "", "", false
	}
	reset := ""
	if m := limitReset.FindStringSubmatch(message); m != nil {
		reset = strings.TrimRight(strings.TrimSpace(m[1]), ".")
	}
	if reset == "" && !quotaText.MatchString(message) && overloadText.MatchString(message) {
		return LimitOverload, "", true
	}
	return LimitQuota, reset, true
}

func LimitReset(errorType, message string) (bool, string) {
	_, reset, ok := ClassifyLimit(errorType, message)
	return ok, reset
}

func ModelLabel(id string) string {
	switch {
	case strings.Contains(id, "fable"):
		return "Fable"
	case strings.Contains(id, "opus"):
		return "Opus"
	case strings.Contains(id, "sonnet"):
		return "Sonnet"
	case strings.Contains(id, "haiku"):
		return "Haiku"
	}
	return id
}

const StaleAfter = 30 * time.Minute

type HostKind string

const (
	HostVSCodeTerminal HostKind = "vscode-terminal"
	HostVSCodePanel    HostKind = "vscode-panel"
	HostITerm          HostKind = "iterm"
	HostTerminalApp    HostKind = "terminal"
	HostTmux           HostKind = "tmux"
	HostUnknown        HostKind = "unknown"
)

type Event struct {
	Name         string         `json:"name"`
	Model        string         `json:"model,omitempty"`
	SessionID    string         `json:"sessionId"`
	Cwd          string         `json:"cwd,omitempty"`
	ConfigDir    string         `json:"configDir,omitempty"`
	Source       string         `json:"source,omitempty"`
	Reason       string         `json:"reason,omitempty"`
	Tool         string         `json:"tool,omitempty"`
	Notification string         `json:"notification,omitempty"`
	Message      string         `json:"message,omitempty"`
	Error        string         `json:"error,omitempty"`
	Subject      string         `json:"subject,omitempty"`
	Risk         string         `json:"risk,omitempty"`
	Context      *usage.Context `json:"context,omitempty"`
	Title        string         `json:"title,omitempty"`
	Branch       string         `json:"branch,omitempty"`
	Summary      string         `json:"summary,omitempty"`
	PR           string         `json:"pr,omitempty"`
	Window       string         `json:"window,omitempty"`
	Ticket       string         `json:"ticket,omitempty"`
	TicketKey    string         `json:"ticketKey,omitempty"`
	Attempt      string         `json:"attempt,omitempty"`
	Bot          string         `json:"bot,omitempty"`
	Agent        string         `json:"agent,omitempty"`
	TermProgram  string         `json:"termProgram,omitempty"`
	TermSession  string         `json:"termSession,omitempty"`
	TmuxPane     string         `json:"tmuxPane,omitempty"`
	ClaudePID    int            `json:"claudePid,omitempty"`
	Ancestors    []int          `json:"ancestors,omitempty"`
	Names        []string       `json:"names,omitempty"`
	TTY          uint64         `json:"tty,omitempty"`
	At           time.Time      `json:"at"`
}

type Host struct {
	Kind        HostKind `json:"kind"`
	WindowID    string   `json:"windowId,omitempty"`
	App         string   `json:"app,omitempty"`
	Folder      string   `json:"folder,omitempty"`
	ShellPID    int      `json:"shellPid,omitempty"`
	Terminal    string   `json:"terminal,omitempty"`
	TermProgram string   `json:"termProgram,omitempty"`
	Pane        string   `json:"pane,omitempty"`
	Connected   bool     `json:"connected,omitempty"`
}

type Session struct {
	ID            string         `json:"id"`
	Label         string         `json:"label"`
	Display       string         `json:"display,omitempty"`
	Cwd           string         `json:"cwd,omitempty"`
	Folder        string         `json:"folder,omitempty"`
	Profile       string         `json:"profile,omitempty"`
	ConfigDir     string         `json:"configDir,omitempty"`
	ClaudePID     int            `json:"claudePid,omitempty"`
	Ancestors     []int          `json:"ancestors,omitempty"`
	Names         []string       `json:"names,omitempty"`
	TTY           uint64         `json:"tty,omitempty"`
	Window        string         `json:"window,omitempty"`
	TermProgram   string         `json:"termProgram,omitempty"`
	TermSession   string         `json:"termSession,omitempty"`
	TmuxPane      string         `json:"tmuxPane,omitempty"`
	Host          Host           `json:"host"`
	Status        Status         `json:"status"`
	StatusSince   time.Time      `json:"statusSince"`
	Tool          string         `json:"tool,omitempty"`
	Detail        string         `json:"detail,omitempty"`
	StartedAt     time.Time      `json:"startedAt"`
	LastActivity  time.Time      `json:"lastActivity"`
	FocusError    string         `json:"focusError,omitempty"`
	FocusAt       time.Time      `json:"focusAt,omitempty"`
	Context       *usage.Context `json:"context,omitempty"`
	Pending       *Pending       `json:"pending,omitempty"`
	Title         string         `json:"title,omitempty"`
	Model         string         `json:"model,omitempty"`
	ResumedBy     string         `json:"resumedBy,omitempty"`
	Note          string         `json:"note,omitempty"`
	Stuck         bool           `json:"stuck,omitempty"`
	Ticket        string         `json:"ticket,omitempty"`
	TicketKey     string         `json:"ticketKey,omitempty"`
	Attempt       string         `json:"attempt,omitempty"`
	Home          string         `json:"home,omitempty"`
	ReadAt        time.Time      `json:"readAt,omitzero"`
	Unread        *Unread        `json:"unread,omitempty"`
	Bot           string         `json:"bot,omitempty"`
	Agent         string         `json:"agent,omitempty"`
	Branch        string         `json:"branch,omitempty"`
	Summary       string         `json:"summary,omitempty"`
	PR            string         `json:"pr,omitempty"`
	TurnStartedAt time.Time      `json:"turnStartedAt,omitempty"`
	Limit         LimitKind      `json:"limit,omitempty"`
	ResumeAt      time.Time      `json:"resumeAt,omitzero"`
	Resumes       int            `json:"resumes,omitempty"`
	AutoAllowed   int            `json:"autoAllowed,omitempty"`
	FailStreak    int            `json:"failStreak,omitempty"`
	failSubject   string
	Drift         []string  `json:"drift,omitempty"`
	Changes       *Changes  `json:"changes,omitempty"`
	Overlap       []Overlap `json:"overlap,omitempty"`
	Tests         *TestRun  `json:"tests,omitempty"`
	Behind        *Behind   `json:"behind,omitempty"`
	Gate          *GateRun  `json:"gate,omitempty"`
	Compacted     int       `json:"compacted,omitempty"`
	CompactedAt   time.Time `json:"compactedAt,omitzero"`
	Spend         *Spend    `json:"spend,omitempty"`
	Cap           int64     `json:"cap,omitempty"`
	OverCap       bool      `json:"overCap,omitempty"`
	Headless      *Headless `json:"headless,omitempty"`
	Standing      *Standing `json:"standing,omitempty"`
}

type Headless struct {
	Turns   int       `json:"turns"`
	At      time.Time `json:"at"`
	Running bool      `json:"running,omitempty"`
	Error   string    `json:"error,omitempty"`
}

type Spend struct {
	Tokens int64     `json:"tokens"`
	Turns  int       `json:"turns,omitempty"`
	At     time.Time `json:"at"`
}

type Changes struct {
	Files   int       `json:"files"`
	Lines   int       `json:"lines"`
	Touched []string  `json:"touched,omitempty"`
	At      time.Time `json:"at"`
}

type Overlap struct {
	ID           string   `json:"id"`
	Session      string   `json:"session"`
	Files        []string `json:"files,omitempty"`
	SameCheckout bool     `json:"sameCheckout,omitempty"`
}

type TestRun struct {
	OK  bool      `json:"ok"`
	At  time.Time `json:"at"`
	Cmd string    `json:"cmd"`
}

type Behind struct {
	Commits   int       `json:"commits"`
	Conflicts []string  `json:"conflicts,omitempty"`
	Upstream  string    `json:"upstream,omitempty"`
	At        time.Time `json:"at"`
	Told      bool      `json:"told,omitempty"`
	Rebased   int       `json:"rebased,omitempty"`
}

func JoinFiles(files []string, n int) string {
	names := []string{}
	for i, f := range files {
		if i == n {
			names = append(names, "+"+strconv.Itoa(len(files)-n)+" more")
			break
		}
		if i := strings.LastIndex(f, "/"); i >= 0 {
			f = f[i+1:]
		}
		names = append(names, f)
	}
	return strings.Join(names, ", ")
}

type GateRun struct {
	OK    bool      `json:"ok"`
	At    time.Time `json:"at"`
	Cmd   string    `json:"cmd,omitempty"`
	Fails int       `json:"fails,omitempty"`
}

const TouchedMax = 8

func IsTestCommand(subject string) bool {
	f := strings.Fields(strings.ToLower(subject))
	if len(f) == 0 {
		return false
	}
	switch f[0] {
	case "jest", "vitest", "pytest", "mocha", "ava", "rspec", "phpunit", "gotestsum", "playwright", "cypress", "maestro":
		return true
	}
	if len(f) < 2 {
		return false
	}
	switch f[0] + " " + f[1] {
	case "go test", "npm test", "npm t", "yarn test", "pnpm test", "bun test", "cargo test", "make test", "make check", "mix test", "dotnet test", "swift test", "gradle test", "flutter test", "deno test":
		return true
	case "bunx jest", "npx jest", "bunx vitest", "npx vitest", "bunx playwright", "npx playwright", "bunx maestro", "npx maestro":
		return true
	case "bun run", "npm run", "pnpm run", "yarn run":
		return len(f) >= 3 && (f[2] == "test" || f[2] == "check" || strings.HasPrefix(f[2], "test:"))
	}
	return false
}

type Unread struct {
	Lines int       `json:"lines"`
	Since time.Time `json:"since"`
	First string    `json:"first,omitempty"`
}

type Pending struct {
	Tool    string    `json:"tool"`
	Subject string    `json:"subject,omitempty"`
	Risk    string    `json:"risk,omitempty"`
	At      time.Time `json:"at"`
}

const StuckAfter = 12 * time.Minute

type Account struct {
	Profile   string          `json:"profile"`
	ConfigDir string          `json:"configDir,omitempty"`
	Limits    *usage.Limits   `json:"limits,omitempty"`
	Forecast  *usage.Forecast `json:"forecast,omitempty"`
	Sessions  int             `json:"sessions"`
}

type Terminal struct {
	Name     string `json:"name"`
	ShellPID int    `json:"shellPid"`
}

type Window struct {
	ID             string     `json:"id"`
	App            string     `json:"app,omitempty"`
	ExtHostPID     int        `json:"extHostPid"`
	Folders        []string   `json:"folders,omitempty"`
	Terminals      []Terminal `json:"terminals,omitempty"`
	FocusedAt      time.Time  `json:"focusedAt,omitempty"`
	ActiveShellPID int        `json:"activeShellPid,omitempty"`
	PanelActive    bool       `json:"panelActive,omitempty"`
	ClaudeTabs     *int       `json:"claudeTabs,omitempty"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

func EditorFromChain(names []string) string {
	for _, raw := range names {
		name := strings.ToLower(filepath.Base(raw))
		switch {
		case strings.HasPrefix(name, "cursor"):
			return "Cursor"
		case strings.HasPrefix(name, "windsurf"):
			return "Windsurf"
		case strings.HasPrefix(name, "codium"), strings.HasPrefix(name, "vscodium"):
			return "VSCodium"
		case strings.HasPrefix(name, "code - insiders"), strings.HasPrefix(name, "code-insiders"):
			return "Visual Studio Code - Insiders"
		case name == "code", strings.HasPrefix(name, "code helper"):
			return "Visual Studio Code"
		}
	}
	return ""
}

func Within(dir, root string) bool {
	dir, root = filepath.Clean(dir), filepath.Clean(root)
	return dir == root || strings.HasPrefix(dir, root+string(filepath.Separator))
}

func PlaceholderID(pid int) string { return "pid:" + strconv.Itoa(pid) }

func Placeholder(id string) bool { return strings.HasPrefix(id, "pid:") }

func DefaultResolve(cwd string) (label, folder string) {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return "?", ""
	}
	return filepath.Base(cwd), ""
}

func DefaultProfile(configDir string) string {
	base := strings.TrimPrefix(filepath.Base(strings.TrimSpace(configDir)), ".")
	switch base {
	case "", ".", "claude":
		return "default"
	}
	if rest, ok := strings.CutPrefix(base, "claude-"); ok && rest != "" {
		return rest
	}
	return base
}
