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
	"andriiklymiuk/corgi/utils/gitbase"
)

const (
	StateWorking       = "working"
	StateInputRequired = "input-required"
	StateAuthRequired  = "auth-required"
	StateCompleted     = "completed"
	StateFailed        = "failed"
	StateBlocked       = "blocked"
)

var states = map[string]bool{StateWorking: true, StateInputRequired: true, StateAuthRequired: true, StateCompleted: true, StateFailed: true, StateBlocked: true}

type Packet struct {
	Ref          string        `json:"ref"`
	Tracker      string        `json:"tracker,omitempty"`
	State        string        `json:"state"`
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
	Draft        bool          `json:"draft,omitempty"`
	WrittenAt    time.Time     `json:"writtenAt"`
}

type Where struct {
	Branch   string   `json:"branch,omitempty"`
	Worktree string   `json:"worktree,omitempty"`
	Base     string   `json:"base,omitempty"`
	Head     string   `json:"head,omitempty"`
	Dirty    []string `json:"dirty,omitempty"`
}

type Verification struct {
	Cmd   string    `json:"cmd"`
	Exit  int       `json:"exit"`
	At    string    `json:"at,omitempty"`
	RanAt time.Time `json:"ranAt,omitempty"`
}

type From struct {
	Harness    string `json:"harness,omitempty"`
	Model      string `json:"model,omitempty"`
	Account    string `json:"account,omitempty"`
	Session    string `json:"session,omitempty"`
	Transcript string `json:"transcript,omitempty"`
	Host       string `json:"host,omitempty"`
}

type Budget struct {
	Context  int `json:"context,omitempty"`
	FiveHour int `json:"fiveHour,omitempty"`
	SevenDay int `json:"sevenDay,omitempty"`
}

const (
	dirName  = "handoffs"
	maxBytes = 32 << 10
	maxItem  = 300
	MaxAge   = 7 * 24 * time.Hour
)

func Dir(composeDir string) string {
	return filepath.Join(utils.CorgiServicesIn(composeDir), dirName)
}

func Path(composeDir, ref string) string {
	return filepath.Join(Dir(composeDir), safeRef(ref)+".json")
}

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

func Remove(composeDir, ref string) error {
	for _, p := range []string{Path(composeDir, ref), MarkdownPath(composeDir, ref)} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

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

func RefFromBranch(branch string) string {
	if m := refInBranch.FindStringSubmatch(strings.ToUpper(branch)); m != nil {
		return m[1]
	}
	return ""
}

func (p Packet) Markdown() string {
	var b strings.Builder
	p.writeHeading(&b)
	p.Where.writeMarkdown(&b)
	writeSection(&b, "Done", p.Done)
	writeSection(&b, "Remaining", p.Remaining)
	writeSection(&b, "Decisions", p.Decisions)
	writeSection(&b, "Uncertain - ask before assuming", p.Uncertain)
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
	p.writeProvenance(&b)
	return b.String()
}

func (p Packet) writeHeading(b *strings.Builder) {
	fmt.Fprintf(b, "# Handoff · %s\n\n", p.Ref)
	fmt.Fprintf(b, "state: **%s**", p.State)
	if p.Blocked != "" {
		fmt.Fprintf(b, " - %s", p.Blocked)
	}
	if p.Draft {
		b.WriteString(" _(draft: assembled by corgi, not written by the run)_")
	}
	b.WriteString("\n")
	if p.Tracker != "" {
		fmt.Fprintf(b, "ticket: %s\n", p.Tracker)
	}
}

func (w Where) writeMarkdown(b *strings.Builder) {
	if w.Branch == "" && w.Head == "" {
		return
	}
	fmt.Fprintf(b, "where: `%s`", w.Branch)
	if w.Worktree != "" {
		fmt.Fprintf(b, " in `%s`", w.Worktree)
	}
	if w.Head != "" {
		fmt.Fprintf(b, " at %s", short(w.Head))
	}
	if w.Base != "" {
		fmt.Fprintf(b, " (base %s)", short(w.Base))
	}
	b.WriteString("\n")
	if len(w.Dirty) > 0 {
		fmt.Fprintf(b, "uncommitted: %s\n", strings.Join(w.Dirty, ", "))
	}
}

func writeSection(b *strings.Builder, name string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "\n## %s\n", name)
	for _, it := range items {
		fmt.Fprintf(b, "- %s\n", it)
	}
}

func (p Packet) writeProvenance(b *strings.Builder) {
	if from := p.From.parts(); len(from) > 0 {
		fmt.Fprintf(b, "\nfrom: %s\n", strings.Join(from, " · "))
	}
	if p.Budget.Context > 0 || p.Budget.FiveHour > 0 {
		fmt.Fprintf(b, "budget when written: context %d%% · 5h %d%% · week %d%%\n", p.Budget.Context, p.Budget.FiveHour, p.Budget.SevenDay)
	}
	if !p.WrittenAt.IsZero() {
		fmt.Fprintf(b, "written: %s\n", p.WrittenAt.Local().Format("2006-01-02 15:04"))
	}
}

func (f From) parts() []string {
	var out []string
	for _, kv := range [][2]string{{"harness", f.Harness}, {"model", f.Model}, {"account", f.Account}, {"session", f.Session}, {"host", f.Host}} {
		if kv[1] != "" {
			out = append(out, kv[0]+" "+kv[1])
		}
	}
	return out
}

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

const gitRevParse = "rev-parse"

var run = func(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func GitWhere(dir string) Where {
	w := Where{}
	if dir == "" {
		return w
	}
	w.Branch, _ = run(dir, gitRevParse, "--abbrev-ref", "HEAD")
	w.Head, _ = run(dir, gitRevParse, "HEAD")
	for _, base := range gitbase.Refs(dir) {
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

func WorktreeDir(dir string, p Packet) string {
	if p.Where.Worktree == "" {
		return dir
	}
	full := filepath.Join(dir, p.Where.Worktree)
	rel, err := filepath.Rel(dir, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return dir
	}
	return full
}

func TrustedCommand(cmd string, trusted []string) bool {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return false
	}
	for _, t := range trusted {
		if strings.TrimSpace(t) == cmd {
			return true
		}
	}
	return false
}

func Verify(dir string, p Packet, runCmd func(dir, cmd string) (int, error)) (Verification, bool) {
	if p.Verification == nil || strings.TrimSpace(p.Verification.Cmd) == "" {
		return Verification{}, false
	}
	head, _ := run(dir, gitRevParse, "HEAD")
	code, err := runCmd(dir, p.Verification.Cmd)
	if err != nil && code == 0 {
		code = 1
	}
	v := Verification{Cmd: p.Verification.Cmd, Exit: code, At: head, RanAt: time.Now()}
	return v, code == 0 && (p.Verification.At == "" || p.Verification.At == head)
}
