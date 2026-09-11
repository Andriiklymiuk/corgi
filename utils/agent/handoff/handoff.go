// Package handoff is the typed record one run leaves for the next: what is
// done, what is not, what was decided, what to ask, and where the code is.
// Never a transcript. A file beside the workspace that any harness can read,
// mirrored to the ticket by whoever has a tracker token.
package handoff

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/atomicfile"
)

// State is the A2A task state the card is in when the packet is written.
const (
	StateWorking       = "working"
	StateInputRequired = "input-required"
	StateAuthRequired  = "auth-required"
	StateCompleted     = "completed"
	StateFailed        = "failed"
	StateBlocked       = "blocked"
)

var states = map[string]bool{StateWorking: true, StateInputRequired: true, StateAuthRequired: true, StateCompleted: true, StateFailed: true, StateBlocked: true}

// Packet is the whole handoff. Lists are short sentences; Next is one.
type Packet struct {
	Ref     string `json:"ref"`
	Tracker string `json:"tracker,omitempty"`
	State   string `json:"state"`
	// Blocked is the reason when State is blocked. Only a missing tool,
	// credential or secret is a reason; "hard" is not.
	Blocked      string        `json:"blocked,omitempty"`
	Where        Where         `json:"where"`
	Done         []string      `json:"done,omitempty"`
	Remaining    []string      `json:"remaining,omitempty"`
	Decisions    []string      `json:"decisions,omitempty"`
	Uncertain    []string      `json:"uncertain,omitempty"`
	Verification *Verification `json:"verification,omitempty"`
	Next         string        `json:"next,omitempty"`
	From         From          `json:"from"`
	Budget       Budget        `json:"budget"`
	// Draft marks a packet corgi assembled without the run's own words —
	// git state and the last line said — so the reader weighs it as such.
	Draft     bool      `json:"draft,omitempty"`
	WrittenAt time.Time `json:"writtenAt"`
}

// Where is the code: branch, worktree, the commits, what is uncommitted.
type Where struct {
	Branch   string   `json:"branch,omitempty"`
	Worktree string   `json:"worktree,omitempty"`
	Base     string   `json:"base,omitempty"`
	Head     string   `json:"head,omitempty"`
	Dirty    []string `json:"dirty,omitempty"`
}

// Verification is the check that was run, machine-checkable by the next run.
type Verification struct {
	Cmd   string    `json:"cmd"`
	Exit  int       `json:"exit"`
	At    string    `json:"at,omitempty"` // the head it ran on
	RanAt time.Time `json:"ranAt,omitempty"`
}

// From is who wrote it, enough for a same-harness resume.
type From struct {
	Harness    string `json:"harness,omitempty"`
	Model      string `json:"model,omitempty"`
	Account    string `json:"account,omitempty"`
	Session    string `json:"session,omitempty"`
	Transcript string `json:"transcript,omitempty"`
	Host       string `json:"host,omitempty"`
}

// Budget is how much room was left when the packet was written.
type Budget struct {
	Context  int `json:"context,omitempty"`  // percent of the window used
	FiveHour int `json:"fiveHour,omitempty"` // percent of the 5h window used
	SevenDay int `json:"sevenDay,omitempty"`
}

const (
	dirName  = "handoffs"
	maxBytes = 32 << 10
	maxItem  = 300
	// MaxAge is how old a packet may be and still be offered to a new session.
	MaxAge = 7 * 24 * time.Hour
)

// Dir is where a workspace keeps its packets: per-developer state, under
// corgi_services, ignored by git.
func Dir(composeDir string) string {
	return filepath.Join(utils.CorgiServicesIn(composeDir), dirName)
}

func Path(composeDir, ref string) string {
	return filepath.Join(Dir(composeDir), safeRef(ref)+".json")
}

// MarkdownPath is the twin a person or a harness without a JSON reader opens.
func MarkdownPath(composeDir, ref string) string {
	return filepath.Join(Dir(composeDir), safeRef(ref)+".md")
}

var unsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func safeRef(ref string) string {
	ref = unsafe.ReplaceAllString(strings.TrimSpace(ref), "_")
	if ref == "" {
		return "_"
	}
	return ref
}

var (
	secretLike  = regexp.MustCompile(`(?i)(api[_-]?key|secret|token|password|passwd)\s*[:=]\s*['"]?[A-Za-z0-9_\-./+]{8,}`)
	knownKeys   = regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{8,}|ghp_[A-Za-z0-9]{20,}|glpat-[A-Za-z0-9_-]{16,}|AKIA[0-9A-Z]{16}|xox[baprs]-[A-Za-z0-9-]{10,}|-----BEGIN [A-Z ]*PRIVATE KEY`)
	placeholder = regexp.MustCompile(`\b(TODO|TBD|FIXME|XXX)\b`)
)

// Validate refuses what would mislead the next run or leak on the ticket:
// no ref or state, a secret in any field, a placeholder where a fact
// should be, an item too long to be a sentence, a packet too big to read.
func Validate(p Packet) error {
	if strings.TrimSpace(p.Ref) == "" {
		return errors.New("a handoff needs a ref: the ticket it is about")
	}
	if !states[p.State] {
		return fmt.Errorf("state %q is not one of working, input-required, auth-required, completed, failed, blocked", p.State)
	}
	if p.State == StateBlocked && strings.TrimSpace(p.Blocked) == "" {
		return errors.New("blocked needs a reason: the missing tool, credential or secret")
	}
	for _, list := range [][]string{p.Done, p.Remaining, p.Decisions, p.Uncertain} {
		for _, item := range list {
			if len(item) > maxItem {
				return fmt.Errorf("an item is %d characters; a handoff is sentences, not a transcript", len(item))
			}
		}
	}
	all := strings.Join(append(append(append(append([]string{p.Next, p.Blocked}, p.Done...), p.Remaining...), p.Decisions...), p.Uncertain...), "\n")
	if secretLike.MatchString(all) || knownKeys.MatchString(all) {
		return errors.New("a handoff must not carry a secret; it is mirrored to the ticket")
	}
	if !p.Draft && placeholder.MatchString(all) {
		return errors.New("a handoff must not carry TODO or TBD: say what is remaining instead")
	}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if len(data) > maxBytes {
		return fmt.Errorf("a handoff is %d bytes; keep it under %d", len(data), maxBytes)
	}
	return nil
}

// Write validates, then writes the JSON and its Markdown twin.
func Write(composeDir string, p Packet) error {
	if p.WrittenAt.IsZero() {
		p.WrittenAt = time.Now()
	}
	if err := Validate(p); err != nil {
		return err
	}
	dir := Dir(composeDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	utils.EnsureCorgiServicesIgnore(utils.CorgiServicesIn(composeDir), dirName+"/")
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicfile.Write(Path(composeDir, p.Ref), data, 0o600); err != nil {
		return err
	}
	return atomicfile.Write(MarkdownPath(composeDir, p.Ref), []byte(p.Markdown()), 0o600)
}

// Read is one packet by ref; os.ErrNotExist when there is none.
func Read(composeDir, ref string) (Packet, error) {
	var p Packet
	data, err := os.ReadFile(Path(composeDir, ref))
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return p, fmt.Errorf("%s: %w", Path(composeDir, ref), err)
	}
	return p, nil
}

// List is every packet in a workspace, newest first.
func List(composeDir string) []Packet {
	entries, err := os.ReadDir(Dir(composeDir))
	if err != nil {
		return nil
	}
	var out []Packet
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(Dir(composeDir), e.Name()))
		if err != nil {
			continue
		}
		var p Packet
		if json.Unmarshal(data, &p) == nil && p.Ref != "" {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].WrittenAt.After(out[j].WrittenAt) })
	return out
}

// Remove deletes a packet and its twin; nothing to delete is not an error.
func Remove(composeDir, ref string) error {
	for _, p := range []string{Path(composeDir, ref), MarkdownPath(composeDir, ref)} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// ForBranch finds the packet whose branch matches, else the one whose ref
// the branch name carries.
func ForBranch(composeDir, branch string) (Packet, bool) {
	if branch == "" {
		return Packet{}, false
	}
	ref := RefFromBranch(branch)
	for _, p := range List(composeDir) {
		if p.Where.Branch == branch || (ref != "" && strings.EqualFold(p.Ref, ref)) {
			return p, true
		}
	}
	return Packet{}, false
}

var refInBranch = regexp.MustCompile(`\b([A-Z][A-Z0-9]{1,9}-\d{1,6})\b`)

// RefFromBranch pulls a ticket key out of a branch name — feature/ABC-123/slug,
// ABC-123-slug, fix/abc-123 — or "".
func RefFromBranch(branch string) string {
	if m := refInBranch.FindStringSubmatch(strings.ToUpper(branch)); m != nil {
		return m[1]
	}
	return ""
}

// Markdown is the packet for a person, or a harness that reads files.
func (p Packet) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Handoff · %s\n\n", p.Ref)
	fmt.Fprintf(&b, "state: **%s**", p.State)
	if p.Blocked != "" {
		fmt.Fprintf(&b, " — %s", p.Blocked)
	}
	if p.Draft {
		b.WriteString(" _(draft: assembled by corgi, not written by the run)_")
	}
	b.WriteString("\n")
	if p.Tracker != "" {
		fmt.Fprintf(&b, "ticket: %s\n", p.Tracker)
	}
	if p.Where.Branch != "" || p.Where.Head != "" {
		fmt.Fprintf(&b, "where: `%s`", p.Where.Branch)
		if p.Where.Worktree != "" {
			fmt.Fprintf(&b, " in `%s`", p.Where.Worktree)
		}
		if p.Where.Head != "" {
			fmt.Fprintf(&b, " at %s", short(p.Where.Head))
		}
		if p.Where.Base != "" {
			fmt.Fprintf(&b, " (base %s)", short(p.Where.Base))
		}
		b.WriteString("\n")
		if len(p.Where.Dirty) > 0 {
			fmt.Fprintf(&b, "uncommitted: %s\n", strings.Join(p.Where.Dirty, ", "))
		}
	}
	section := func(name string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n## %s\n", name)
		for _, it := range items {
			fmt.Fprintf(&b, "- %s\n", it)
		}
	}
	section("Done", p.Done)
	section("Remaining", p.Remaining)
	section("Decisions", p.Decisions)
	section("Uncertain — ask before assuming", p.Uncertain)
	if p.Verification != nil {
		fmt.Fprintf(&b, "\n## Verified\n`%s` → exit %d", p.Verification.Cmd, p.Verification.Exit)
		if p.Verification.At != "" {
			fmt.Fprintf(&b, " at %s", short(p.Verification.At))
		}
		b.WriteString("\n")
	}
	if p.Next != "" {
		fmt.Fprintf(&b, "\n## Next\n%s\n", p.Next)
	}
	var from []string
	for _, kv := range [][2]string{{"harness", p.From.Harness}, {"model", p.From.Model}, {"account", p.From.Account}, {"session", p.From.Session}, {"host", p.From.Host}} {
		if kv[1] != "" {
			from = append(from, kv[0]+" "+kv[1])
		}
	}
	if len(from) > 0 {
		fmt.Fprintf(&b, "\nfrom: %s\n", strings.Join(from, " · "))
	}
	if p.Budget.Context > 0 || p.Budget.FiveHour > 0 {
		fmt.Fprintf(&b, "budget when written: context %d%% · 5h %d%% · week %d%%\n", p.Budget.Context, p.Budget.FiveHour, p.Budget.SevenDay)
	}
	if !p.WrittenAt.IsZero() {
		fmt.Fprintf(&b, "written: %s\n", p.WrittenAt.Local().Format("2006-01-02 15:04"))
	}
	return b.String()
}

// Summary is one line for a board row or a ticket comment.
func (p Packet) Summary() string {
	parts := []string{p.State}
	if n := len(p.Done); n > 0 {
		parts = append(parts, fmt.Sprintf("%d done", n))
	}
	if n := len(p.Remaining); n > 0 {
		parts = append(parts, fmt.Sprintf("%d remaining", n))
	}
	if p.Next != "" {
		parts = append(parts, "next: "+p.Next)
	}
	return strings.Join(parts, " · ")
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// run is a seam for git in tests.
var run = func(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// GitWhere reads branch, head, base (merge-base with the default branch,
// best effort) and dirty files from a checkout.
func GitWhere(dir string) Where {
	w := Where{}
	if dir == "" {
		return w
	}
	w.Branch, _ = run(dir, "rev-parse", "--abbrev-ref", "HEAD")
	w.Head, _ = run(dir, "rev-parse", "HEAD")
	for _, base := range []string{"origin/main", "origin/master", "main", "master"} {
		if b, err := run(dir, "merge-base", "HEAD", base); err == nil && b != "" && b != w.Head {
			w.Base = b
			break
		}
	}
	if status, err := run(dir, "status", "--porcelain"); err == nil && status != "" {
		for _, line := range strings.Split(status, "\n") {
			if len(line) > 3 {
				w.Dirty = append(w.Dirty, strings.TrimSpace(line[3:]))
			}
		}
	}
	return w
}

// CommitsSince is how far the branch moved since the packet: commits after
// its head. The staleness that matters is measured in commits, not hours.
func CommitsSince(dir, head string) (int, error) {
	if head == "" {
		return 0, errors.New("the packet has no head")
	}
	out, err := run(dir, "rev-list", "--count", head+"..HEAD")
	if err != nil {
		return 0, err
	}
	var n int
	_, err = fmt.Sscanf(out, "%d", &n)
	return n, err
}

// Verify re-runs the packet's check at the current head. exit 0 and the same
// head means the packet can be trusted as written; anything else means the
// next run starts from the ticket and the diff, not the packet.
func Verify(dir string, p Packet, runCmd func(dir, cmd string) (int, error)) (Verification, bool) {
	if p.Verification == nil || strings.TrimSpace(p.Verification.Cmd) == "" {
		return Verification{}, false
	}
	head, _ := run(dir, "rev-parse", "HEAD")
	code, err := runCmd(dir, p.Verification.Cmd)
	if err != nil && code == 0 {
		code = 1
	}
	v := Verification{Cmd: p.Verification.Cmd, Exit: code, At: head, RanAt: time.Now()}
	return v, code == 0 && (p.Verification.At == "" || p.Verification.At == head)
}
