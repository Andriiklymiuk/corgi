package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// MemoryDirName is the committed, team-shared workspace memory store, relative to
// the dir holding corgi-compose.yml. Opt-in: absent means "no memory", never an error.
const MemoryDirName = ".corgi/memory"

// typeDirs maps a fact type to its subdir (and back). Order defines index/list order.
var typeDirs = []struct{ Type, Dir string }{
	{"decision", "decisions"},
	{"incident", "incidents"},
	{"domain", "domain"},
	{"fix", "fixes"},
}

// Fact is one memory entry — frontmatter + body. One file under <type>/<name>.md.
type Fact struct {
	Name        string   `json:"name" yaml:"name"`
	Description string   `json:"description" yaml:"description"`
	Type        string   `json:"type" yaml:"type"`
	Service     string   `json:"service,omitempty" yaml:"service,omitempty"`
	Created     string   `json:"created,omitempty" yaml:"created,omitempty"`
	Pattern     string   `json:"pattern,omitempty" yaml:"pattern,omitempty"`
	Links       []string `json:"links,omitempty" yaml:"links,omitempty"`
	Body        string   `json:"-" yaml:"-"`
	Path        string   `json:"path" yaml:"-"`
}

var (
	frontmatterRe = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n?(.*)$`)
	linkRe        = regexp.MustCompile(`\[\[([^\]]+)\]\]`)
)

func typeForDir(dir string) string {
	for _, td := range typeDirs {
		if td.Dir == dir {
			return td.Type
		}
	}
	return ""
}

func typeRank(t string) int {
	for i, td := range typeDirs {
		if td.Type == t {
			return i
		}
	}
	return len(typeDirs)
}

// parseFact splits frontmatter from body and normalizes [[name]] links to bare names.
func parseFact(raw []byte, path string) (Fact, error) {
	var f Fact
	m := frontmatterRe.FindSubmatch(raw)
	if m == nil {
		return f, nil // no frontmatter — caller treats as malformed via lint, not a read error
	}
	if err := yaml.Unmarshal(m[1], &f); err != nil {
		return f, err
	}
	f.Body = strings.TrimSpace(string(m[2]))
	f.Path = path
	f.Links = normalizeLinks(f.Links)
	return f, nil
}

func normalizeLinks(in []string) []string {
	out := make([]string, 0, len(in))
	for _, l := range in {
		if m := linkRe.FindStringSubmatch(l); m != nil {
			out = append(out, m[1])
		} else if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// ReadFacts loads every fact under root, sorted by type rank then name. An absent
// root returns an empty slice and no error — memory is opt-in.
func ReadFacts(root string) ([]Fact, error) {
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil, nil
	}
	var facts []Fact
	for _, td := range typeDirs {
		dir := filepath.Join(root, td.Dir)
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			p := filepath.Join(dir, e.Name())
			raw, err := os.ReadFile(p)
			if err != nil {
				return nil, err
			}
			f, err := parseFact(raw, p)
			if err != nil {
				return nil, err
			}
			if f.Type == "" {
				f.Type = typeForDir(td.Dir) // trust the folder when frontmatter omits it
			}
			facts = append(facts, f)
		}
	}
	sort.SliceStable(facts, func(i, j int) bool {
		if ri, rj := typeRank(facts[i].Type), typeRank(facts[j].Type); ri != rj {
			return ri < rj
		}
		return facts[i].Name < facts[j].Name
	})
	return facts, nil
}

func dirForType(t string) (string, bool) {
	for _, td := range typeDirs {
		if td.Type == t {
			return td.Dir, true
		}
	}
	return "", false
}

// AddFact scaffolds <root>/<typedir>/<name>.md with valid frontmatter and returns its
// path. Creates the dir on first use (so the store is created lazily on opt-in).
func AddFact(root string, f Fact) (string, error) {
	dir, ok := dirForType(f.Type)
	if !ok {
		return "", fmt.Errorf("unknown memory type %q (want decision|incident|domain|fix)", f.Type)
	}
	if f.Name == "" {
		return "", fmt.Errorf("fact name is required")
	}
	if f.Created == "" {
		f.Created = time.Now().UTC().Format("2006-01-02")
	}
	full := filepath.Join(root, dir)
	if err := os.MkdirAll(full, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(full, f.Name+".md")
	var b strings.Builder
	b.WriteString("---\n")
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(frontmatterOf(f)); err != nil {
		return "", err
	}
	_ = enc.Close()
	b.WriteString("---\n\n")
	if f.Body != "" {
		b.WriteString(f.Body + "\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// frontmatterOf returns the ordered, omitempty frontmatter view (no Body/Path).
func frontmatterOf(f Fact) map[string]any {
	m := map[string]any{"name": f.Name, "description": f.Description, "type": f.Type}
	if f.Service != "" {
		m["service"] = f.Service
	}
	if f.Created != "" {
		m["created"] = f.Created
	}
	if f.Pattern != "" {
		m["pattern"] = f.Pattern
	}
	if len(f.Links) > 0 {
		wrapped := make([]string, len(f.Links))
		for i, l := range f.Links {
			wrapped[i] = "[[" + l + "]]"
		}
		m["links"] = wrapped
	}
	return m
}

// RenderIndex builds index.md from facts (already sorted by ReadFacts).
func RenderIndex(facts []Fact) string {
	var b strings.Builder
	b.WriteString("<!-- generated by `corgi memory index` — do not edit by hand -->\n")
	b.WriteString("# Workspace memory\n\n")
	b.WriteString(fmt.Sprintf("_%d facts · generated %s_\n", len(facts), time.Now().UTC().Format("2006-01-02")))
	lastType := ""
	for _, f := range facts {
		if f.Type != lastType {
			b.WriteString(fmt.Sprintf("\n## %s\n", pluralType(f.Type)))
			lastType = f.Type
		}
		svc := ""
		if f.Service != "" {
			svc = fmt.Sprintf(" (%s)", f.Service)
		}
		dir, _ := dirForType(f.Type)
		b.WriteString(fmt.Sprintf("- **%s**%s — %s → `%s/%s.md`\n", f.Name, svc, f.Description, dir, f.Name))
	}
	return b.String()
}

func pluralType(t string) string {
	if d, ok := dirForType(t); ok {
		return d
	}
	return t
}

// MemoryIssue is one lint finding (mirrors ValidationIssue's shape).
type MemoryIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	File    string `json:"file"`
}

// Memory lint codes (stable; documented in docs/agents.md).
const (
	ErrMemorySecret       = "E_MEMORY_SECRET"        // a secret-shaped string in committed memory
	ErrMemoryTypeMismatch = "E_MEMORY_TYPE_MISMATCH" // frontmatter type != folder
	ErrMemoryBadName      = "E_MEMORY_BAD_NAME"      // name != filename, or not kebab-case
	ErrMemoryNoFront      = "E_MEMORY_NO_FRONTMATTER"
	ErrMemoryDanglingLink = "E_MEMORY_DANGLING_LINK" // [[x]] with no matching fact (warning)
)

var (
	kebabRe   = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	awsKeyRe  = regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)
	pemRe     = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)
	assignRe  = regexp.MustCompile(`(?i)(password|secret|token|api[_-]?key|access[_-]?key)\s*[:=]\s*\S{6,}`)
	urlCredRe = regexp.MustCompile(`[a-z]+://[^/\s:@]+:[^/\s@]+@`) // user:pass@host
)

// scanSecrets returns true if the text looks like it contains a credential.
func scanSecrets(text string) bool {
	return awsKeyRe.MatchString(text) || pemRe.MatchString(text) ||
		assignRe.MatchString(text) || urlCredRe.MatchString(text)
}

// LintFacts validates a memory store: returns (errors, warnings). An absent store is
// clean (opt-in). Errors should fail the store; warnings are advisory.
func LintFacts(root string) ([]MemoryIssue, []MemoryIssue) {
	var errs, warns []MemoryIssue
	facts, err := ReadFacts(root)
	if err != nil {
		return []MemoryIssue{{Code: ErrMemoryNoFront, Message: err.Error()}}, nil
	}
	names := make(map[string]bool, len(facts))
	for _, f := range facts {
		names[f.Name] = true
	}
	for _, f := range facts {
		rel := f.Path
		dir := filepath.Base(filepath.Dir(f.Path))
		if f.Name == "" || f.Description == "" {
			errs = append(errs, MemoryIssue{ErrMemoryNoFront, "missing name/description frontmatter", rel})
			continue
		}
		stem := strings.TrimSuffix(filepath.Base(f.Path), ".md")
		if f.Name != stem || !kebabRe.MatchString(f.Name) {
			errs = append(errs, MemoryIssue{ErrMemoryBadName, "name must be kebab-case and match the filename", rel})
		}
		if want := typeForDir(dir); want != "" && f.Type != want {
			errs = append(errs, MemoryIssue{ErrMemoryTypeMismatch,
				fmt.Sprintf("type %q in %s/ (want %q)", f.Type, dir, want), rel})
		}
		for _, l := range f.Links {
			if !names[l] {
				warns = append(warns, MemoryIssue{ErrMemoryDanglingLink,
					fmt.Sprintf("[[%s]] has no matching fact", l), rel})
			}
		}
	}
	// The secret scan covers EVERY .md under the store — loose files at the root and
	// index.md included — so a hand-authored leak can't bypass the typed-subdir read.
	errs = append(errs, scanStoreForSecrets(root)...)
	return errs, warns
}

// scanStoreForSecrets walks root for .md files and reports each one whose contents
// look like a credential. An absent store yields nothing (opt-in, no-op).
func scanStoreForSecrets(root string) []MemoryIssue {
	var errs []MemoryIssue
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if scanSecrets(string(raw)) {
			errs = append(errs, MemoryIssue{ErrMemorySecret,
				"a secret-shaped string is present — committed memory must never hold secrets", path})
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		errs = append(errs, MemoryIssue{ErrMemorySecret, err.Error(), root})
	}
	return errs
}
