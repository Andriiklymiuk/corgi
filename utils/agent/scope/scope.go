package scope

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/atomicfile"
)

type Scope struct {
	Ref       string     `json:"ref"`
	Paths     []string   `json:"paths,omitempty"`
	Lines     int        `json:"lines,omitempty"`
	Tests     int        `json:"tests,omitempty"`
	Done      []string   `json:"done,omitempty"`
	SetAt     time.Time  `json:"setAt"`
	Widenings []Widening `json:"widenings,omitempty"`
}

type Widening struct {
	Path string    `json:"path"`
	By   string    `json:"by,omitempty"`
	At   time.Time `json:"at"`
}

const dirName = "scope"

func Dir(composeDir string) string {
	return filepath.Join(utils.CorgiServicesIn(composeDir), dirName)
}

var unsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func Path(composeDir, ref string) string {
	return filepath.Join(Dir(composeDir), unsafe.ReplaceAllString(strings.TrimSpace(ref), "_")+".json")
}

func Write(composeDir string, s Scope) error {
	if strings.TrimSpace(s.Ref) == "" {
		return errors.New("a scope needs a ref")
	}
	if s.SetAt.IsZero() {
		s.SetAt = time.Now()
	}
	if err := os.MkdirAll(Dir(composeDir), 0o755); err != nil {
		return err
	}
	utils.EnsureCorgiServicesIgnore(utils.CorgiServicesIn(composeDir), dirName+"/")
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(Path(composeDir, s.Ref), data, 0o600)
}

func Read(composeDir, ref string) (Scope, error) {
	var s Scope
	data, err := os.ReadFile(Path(composeDir, ref))
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(data, &s)
}

func Remove(composeDir, ref string) error {
	err := os.Remove(Path(composeDir, ref))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func List(composeDir string) []Scope {
	entries, err := os.ReadDir(Dir(composeDir))
	if err != nil {
		return nil
	}
	var out []Scope
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(Dir(composeDir), e.Name()))
		if err != nil {
			continue
		}
		var s Scope
		if json.Unmarshal(data, &s) == nil && s.Ref != "" {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SetAt.After(out[j].SetAt) })
	return out
}

var refInBranch = regexp.MustCompile(`\b([A-Z][A-Z0-9]{1,9}-\d{1,6})\b`)

func ForBranch(composeDir, branch string) (Scope, bool) {
	m := refInBranch.FindStringSubmatch(strings.ToUpper(branch))
	if m == nil {
		return Scope{}, false
	}
	s, err := Read(composeDir, m[1])
	return s, err == nil
}

func Widen(composeDir, ref, glob, by string) (Scope, error) {
	s, err := Read(composeDir, ref)
	if err != nil {
		return s, err
	}
	glob = strings.TrimSpace(glob)
	if glob == "" {
		return s, errors.New("a path is required")
	}
	s.Paths = append(s.Paths, glob)
	s.Widenings = append(s.Widenings, Widening{Path: glob, By: by, At: time.Now()})
	return s, Write(composeDir, s)
}

func (s Scope) Allows(rel string) bool {
	if len(s.Paths) == 0 {
		return true
	}
	for _, candidate := range Candidates(rel) {
		for _, p := range s.Paths {
			if Match(p, candidate) {
				return true
			}
		}
	}
	return false
}

var worktreeDir = regexp.MustCompile(`^(?:\.corgi/corgi_services/\.worktrees|\.corgi/\.worktrees)/([^/@]+)-[0-9a-f]{6}@[^/]+/`)

func Candidates(rel string) []string {
	rel = filepath.ToSlash(strings.TrimPrefix(rel, "./"))
	out := []string{rel}
	if m := worktreeDir.FindStringSubmatch(rel); m != nil {
		rewritten := m[1] + "/" + rel[len(m[0]):]
		out = append(out, rewritten)
		rel = rewritten
	}
	if i := strings.Index(rel, "/"); i > 0 {
		out = append(out, rel[i+1:])
	}
	return out
}

func InRepo(workspaceRoot, repoRoot, abs string) string {
	rel, err := filepath.Rel(repoRoot, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	name := filepath.Base(repoRoot)
	if m := regexp.MustCompile(`^([^@]+)-[0-9a-f]{6}@`).FindStringSubmatch(name); m != nil {
		name = m[1]
	}
	if workspaceRoot != "" && filepath.Clean(repoRoot) == filepath.Clean(workspaceRoot) {
		return filepath.ToSlash(rel)
	}
	return name + "/" + filepath.ToSlash(rel)
}

func Match(pattern, rel string) bool {
	pattern = filepath.ToSlash(strings.TrimPrefix(strings.TrimSpace(pattern), "./"))
	if pattern == "" {
		return false
	}
	if !strings.ContainsAny(pattern, "*?[") {
		return rel == pattern || strings.HasPrefix(rel, strings.TrimSuffix(pattern, "/")+"/")
	}
	return globRegexp(pattern).MatchString(rel)
}

func globRegexp(pattern string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch {
		case c == '*' && i+1 < len(pattern) && pattern[i+1] == '*':
			if i+2 < len(pattern) && pattern[i+2] == '/' {
				b.WriteString("(?:.*/)?")
				i += 2
			} else {
				b.WriteString(".*")
				i++
			}
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String())
}

func IsTestFile(rel string) bool {
	base := strings.ToLower(filepath.Base(rel))
	dir := strings.ToLower(filepath.ToSlash(filepath.Dir(rel)))
	switch {
	case strings.HasSuffix(base, "_test.go"), strings.HasSuffix(base, "_test.py"), strings.HasPrefix(base, "test_"):
		return true
	case strings.Contains(base, ".test."), strings.Contains(base, ".spec."):
		return true
	case strings.Contains(dir, "__tests__"), strings.HasSuffix(dir, "/tests"), strings.HasSuffix(dir, "/test"), strings.HasSuffix(dir, "/spec"), dir == "tests", dir == "test", dir == "spec":
		return true
	}
	return false
}
