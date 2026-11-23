package watch

import (
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	KindCode      = "code"
	KindTests     = "tests"
	KindGenerated = "generated"
	KindLock      = "lock"
	KindDocs      = "docs"
	KindAssets    = "assets"
	KindMoved     = "moved"
	KindRemoved   = "removed"

	changeFileLineCap = 400
	changeLineCap     = 400
)

type ChangeLine struct {
	T    string `json:"t"`
	Old  int    `json:"old,omitempty"`
	New  int    `json:"new,omitempty"`
	Text string `json:"text"`
}

type ChangeHunk struct {
	Header string       `json:"header,omitempty"`
	Adds   int          `json:"adds"`
	Dels   int          `json:"dels"`
	Flags  []string     `json:"flags,omitempty"`
	Lines  []ChangeLine `json:"lines"`
}

type ChangeFile struct {
	Path   string       `json:"path"`
	Was    string       `json:"was,omitempty"`
	Status string       `json:"status"`
	Adds   int          `json:"adds"`
	Dels   int          `json:"dels"`
	Kind   string       `json:"kind"`
	Why    string       `json:"why,omitempty"`
	Eyes   bool         `json:"eyes"`
	Cut    bool         `json:"cut,omitempty"`
	Hunks  []ChangeHunk `json:"hunks,omitempty"`
}

type Change struct {
	Ref     string       `json:"ref"`
	Link    string       `json:"link"`
	Title   string       `json:"title"`
	Body    string       `json:"body,omitempty"`
	Author  string       `json:"author,omitempty"`
	Branch  string       `json:"branch,omitempty"`
	Base    string       `json:"base,omitempty"`
	Head    string       `json:"head,omitempty"`
	State   string       `json:"state,omitempty"`
	Total   int          `json:"total"`
	Eyes    int          `json:"eyes"`
	Skipped []ChangeSkip `json:"skipped,omitempty"`
	Files   []ChangeFile `json:"files"`

	Opened    time.Time      `json:"opened,omitzero"`
	Commits   int            `json:"commits,omitempty"`
	Behind    bool           `json:"behind,omitempty"`
	Conflicts bool           `json:"conflicts,omitempty"`
	Reviewers []ChangePerson `json:"reviewers,omitempty"`
	CheckRuns []ChangeCheck  `json:"checkRuns,omitempty"`
	Activity  []ChangeEvent  `json:"activity,omitempty"`

	baseSHA  string
	startSHA string
}

type ChangePerson struct {
	Name  string `json:"name"`
	State string `json:"state"`
}

type ChangeCheck struct {
	Name  string `json:"name"`
	State string `json:"state"`
}

type ChangeEvent struct {
	Kind string    `json:"kind"`
	Who  string    `json:"who,omitempty"`
	Text string    `json:"text,omitempty"`
	At   time.Time `json:"at,omitzero"`
}

const (
	PersonApproved  = "approved"
	PersonChanges   = "changes"
	PersonCommented = "commented"
	PersonWaiting   = "waiting"

	CheckPassed  = "passed"
	CheckFailed  = "failed"
	CheckRunning = "running"
	CheckSkipped = "skipped"

	changeEventsKeep = 30
)

func checkState(status, conclusion string) string {
	switch strings.ToLower(conclusion) {
	case "success", "passed", "succeeded":
		return CheckPassed
	case "failure", "failed", "timed_out", "action_required", "startup_failure", "error", "canceled", "cancelled":
		return CheckFailed
	case "skipped", "neutral", "manual":
		return CheckSkipped
	}
	if conclusion == "" && strings.EqualFold(status, "completed") {
		return CheckPassed
	}
	return CheckRunning
}

func (c *Change) settle() {
	sort.SliceStable(c.Activity, func(i, j int) bool { return c.Activity[i].At.Before(c.Activity[j].At) })
	if len(c.Activity) > changeEventsKeep {
		c.Activity = append(c.Activity[:1:1], c.Activity[len(c.Activity)-changeEventsKeep+1:]...)
	}
	sort.SliceStable(c.CheckRuns, func(i, j int) bool { return checkWeight(c.CheckRuns[i]) < checkWeight(c.CheckRuns[j]) })
}

func checkWeight(c ChangeCheck) int {
	switch c.State {
	case CheckFailed:
		return 0
	case CheckRunning:
		return 1
	case CheckPassed:
		return 2
	}
	return 3
}

type ChangeSkip struct {
	Kind  string `json:"kind"`
	Files int    `json:"files"`
	Lines int    `json:"lines"`
}

var lockFiles = map[string]bool{
	"package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true, "bun.lock": true, "bun.lockb": true,
	"go.sum": true, "cargo.lock": true, "podfile.lock": true, "gemfile.lock": true, "poetry.lock": true,
	"composer.lock": true, "uv.lock": true, "package.resolved": true, "flake.lock": true,
}

var (
	generatedPath = regexp.MustCompile(`(?i)(^|/)(generated|__generated__|dist|build|vendor|node_modules|\.next)/|\.gen\.|_generated\.|\.pb\.go$|_pb2\.py$|\.min\.(js|css)$|\.snap$|\.g\.dart$|\.map$`)
	testPath      = regexp.MustCompile(`(?i)_test\.go$|\.(test|spec)\.[a-z]+$|(^|/)(__tests__|__mocks__|tests?|e2e|fixtures|testdata|\.maestro)/|_test\.py$|(^|/)test_[^/]+\.py$`)
	docsPath      = regexp.MustCompile(`(?i)\.(md|mdx|txt|rst|adoc)$|(^|/)(docs?|LICENSE|CHANGELOG)(/|$)`)
	assetPath     = regexp.MustCompile(`(?i)\.(png|jpe?g|gif|webp|svg|ico|mp4|mov|pdf|ttf|otf|woff2?|zip)$`)
)

type sensitivePath struct {
	re  *regexp.Regexp
	why string
}

var sensitivePaths = []sensitivePath{
	{regexp.MustCompile(`(?i)(^|/)(migrations?|migrate)/|\.sql$`), "changes the database"},
	{regexp.MustCompile(`(?i)(^|/)\.env|secrets?|credentials?`), "secrets or env"},
	{regexp.MustCompile(`(?i)(^|[/_.-])(auth(n|z|entication|orization)?|oauth2?|login|permissions?|guards?|acl|rbac|passwords?|crypto)([/_.-]|$)`), "who may do what"},
	{regexp.MustCompile(`(?i)payment|billing|invoice|stripe|checkout|pricing|subscription|refund`), "money"},
	{regexp.MustCompile(`(?i)(^|/)\.github/workflows/|\.gitlab-ci|(^|/)Dockerfile|docker-compose|\.tf$|(^|/)(k8s|helm|terraform|infra|deploy)/|(^|/)Makefile$`), "build or deploy"},
	{regexp.MustCompile(`(?i)(^|/)(package\.json|go\.mod|Cargo\.toml|requirements[^/]*\.txt|pyproject\.toml|Gemfile|Podfile|build\.gradle(\.kts)?)$`), "dependencies"},
}

type lineFlag struct {
	re   *regexp.Regexp
	side string
	flag string
}

var lineFlags = []lineFlag{
	{regexp.MustCompile(`(?i)(password|passwd|secret|api[_-]?key|access[_-]?key|private[_-]?key)["']?\s*[:=]\s*["'][^"'\s$<{]{8,}["']`), "+", "looks like a secret"},
	{regexp.MustCompile(`(?i)\b(drop\s+(table|column|index)|truncate\s+table|delete\s+from)\b`), "+", "deletes data"},
	{regexp.MustCompile(`@ts-ignore|@ts-expect-error|@ts-nocheck|eslint-disable|//\s*nolint|#\s*type:\s*ignore|#\s*noqa|\bas any\b|:\s*any\b|swiftlint:disable`), "+", "a check is switched off"},
	{regexp.MustCompile(`\.(skip|only)\(|\b(xit|xdescribe|fdescribe|fit)\(|\bt\.Skip(f|Now)?\(|@pytest\.mark\.skip|@Disabled|@Ignore`), "+", "a test is skipped"},
	{regexp.MustCompile(`catch\s*(\([^)]*\))?\s*\{\s*\}|except[^:]*:\s*pass\b|\b_\s*=\s*err\b|\.catch\(\(\)\s*=>\s*(\{\s*\}|null|undefined)\)`), "+", "an error is swallowed"},
	{regexp.MustCompile(`(?i)rm\s+-rf|--no-verify|--force\b|chmod\s+777|verify\s*=\s*False|InsecureSkipVerify:\s*true|rejectUnauthorized:\s*false`), "+", "a forceful or unsafe switch"},
	{regexp.MustCompile(`\bconsole\.(log|debug)\(|\bdebugger\b|\bbinding\.pry\b|\bpdb\.set_trace\(`), "+", "a debug line is left"},
	{regexp.MustCompile(`(?i)\b(todo|fixme|hack|xxx)\b`), "+", "a TODO is left"},
	{regexp.MustCompile(`dangerouslySetInnerHTML|\beval\(|\bexec\(|innerHTML\s*=`), "+", "runs or injects text as code"},
	{regexp.MustCompile(`(?i)\b(authorize|isAdmin|requireAuth|hasPermission|canAccess|validate[A-Z_]\w*|sanitize\w*|assert\w*|verify\w*)\(`), "-", "a guard is removed"},
}

func fileKind(file ChangeFile) string {
	name := strings.ToLower(path.Base(file.Path))
	switch {
	case lockFiles[name]:
		return KindLock
	case generatedPath.MatchString(file.Path):
		return KindGenerated
	case file.Status == "removed":
		return KindRemoved
	case file.Status == "renamed" && file.Adds+file.Dels == 0:
		return KindMoved
	case assetPath.MatchString(file.Path):
		return KindAssets
	case testPath.MatchString(file.Path):
		return KindTests
	case docsPath.MatchString(file.Path):
		return KindDocs
	}
	return KindCode
}

func sensitiveWhy(p string) string {
	for _, s := range sensitivePaths {
		if s.re.MatchString(p) {
			return s.why
		}
	}
	return ""
}

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@ ?(.*)$`)

func ParsePatch(patch string) (hunks []ChangeHunk, cut bool) {
	var cur *ChangeHunk
	oldLine, newLine, kept := 0, 0, 0
	for _, raw := range strings.Split(strings.ReplaceAll(patch, "\r\n", "\n"), "\n") {
		if m := hunkHeader.FindStringSubmatch(raw); m != nil {
			oldLine, _ = strconv.Atoi(m[1])
			newLine, _ = strconv.Atoi(m[2])
			hunks = append(hunks, ChangeHunk{Header: strings.TrimSpace(m[3]), Lines: []ChangeLine{}})
			cur = &hunks[len(hunks)-1]
			continue
		}
		if cur == nil || raw == "" || strings.HasPrefix(raw, `\`) {
			continue
		}
		line := ChangeLine{T: raw[:1], Text: clipLine(raw[1:])}
		switch line.T {
		case "+":
			line.New = newLine
			newLine++
			cur.Adds++
		case "-":
			line.Old = oldLine
			oldLine++
			cur.Dels++
		default:
			line.T = " "
			line.Old, line.New = oldLine, newLine
			oldLine++
			newLine++
		}
		if kept >= changeFileLineCap {
			cut = true
			continue
		}
		kept++
		cur.Lines = append(cur.Lines, line)
	}
	for i := range hunks {
		hunks[i].Flags = hunkFlags(hunks[i])
	}
	return hunks, cut
}

func clipLine(s string) string {
	if len(s) <= changeLineCap {
		return s
	}
	return s[:changeLineCap] + "…"
}

func hunkFlags(h ChangeHunk) []string {
	var flags []string
	seen := map[string]bool{}
	added := map[string]bool{}
	for _, l := range h.Lines {
		if l.T == "+" {
			added[strings.TrimSpace(l.Text)] = true
		}
	}
	for _, l := range h.Lines {
		if l.T == " " {
			continue
		}
		for _, f := range lineFlags {
			if f.side != l.T || seen[f.flag] || !f.re.MatchString(l.Text) {
				continue
			}
			if f.side == "-" && added[strings.TrimSpace(l.Text)] {
				continue
			}
			seen[f.flag] = true
			flags = append(flags, f.flag)
		}
	}
	return flags
}

func (c *Change) Sort() {
	c.Total, c.Eyes = 0, 0
	skipped := map[string]*ChangeSkip{}
	for i := range c.Files {
		f := &c.Files[i]
		f.Kind = fileKind(*f)
		flagged := false
		for _, h := range f.Hunks {
			flagged = flagged || len(h.Flags) > 0
		}
		switch {
		case f.Kind == KindCode:
			f.Eyes, f.Why = true, sensitiveWhy(f.Path)
		case f.Kind == KindTests && f.Dels > 0 && f.Dels > 2*f.Adds:
			f.Eyes, f.Why = true, "tests are removed"
		case flagged && (f.Kind == KindTests || f.Kind == KindDocs):
			f.Eyes = true
		}
		lines := f.Adds + f.Dels
		c.Total += lines
		if f.Eyes {
			c.Eyes += lines
			continue
		}
		f.Hunks = nil
		row := skipped[f.Kind]
		if row == nil {
			row = &ChangeSkip{Kind: f.Kind}
			skipped[f.Kind] = row
		}
		row.Files++
		row.Lines += lines
	}
	sort.SliceStable(c.Files, func(i, j int) bool { return fileWeight(c.Files[i]) > fileWeight(c.Files[j]) })
	c.Skipped = c.Skipped[:0]
	for _, kind := range []string{KindTests, KindDocs, KindGenerated, KindLock, KindMoved, KindRemoved, KindAssets} {
		if row := skipped[kind]; row != nil {
			c.Skipped = append(c.Skipped, *row)
		}
	}
}

func fileWeight(f ChangeFile) int {
	if !f.Eyes {
		return 0
	}
	w := 1
	if f.Why != "" {
		w += 4
	}
	for _, h := range f.Hunks {
		w += 2 * len(h.Flags)
	}
	return w
}
