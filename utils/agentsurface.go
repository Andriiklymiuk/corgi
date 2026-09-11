package utils

import (
	"regexp"
	"sort"
	"strings"
)

// The changed surface is the part of a diff a reviewer reads first: what
// other code can call, send or query — exported functions and types,
// routes, schemas, migrations, contracts — with what was added, removed or
// changed. Read from the patch itself, so it needs no toolchain and works
// the same across every repository of a stack.

// SurfaceChange is one declaration or contract file that changed.
type SurfaceChange struct {
	Kind string `json:"kind"` // symbol, route, contract, migration, config
	Name string `json:"name"`
	Path string `json:"path"`
	// Op is added, removed or changed.
	Op string `json:"op"`
	// Breaking marks a removal or a signature change of something public.
	Breaking bool `json:"breaking,omitempty"`
}

// RepoSurface is one repository's changed surface.
type RepoSurface struct {
	Service string          `json:"service"`
	Repo    string          `json:"repo,omitempty"`
	Changes []SurfaceChange `json:"changes"`
}

var (
	goDecl   = regexp.MustCompile(`^func (?:\([^)]*\) )?([A-Z]\w*)\(|^type ([A-Z]\w*)\b|^var ([A-Z]\w*)\b|^const ([A-Z]\w*)\b`)
	tsDecl   = regexp.MustCompile(`^export (?:default )?(?:async )?(?:function|class|interface|type|const|let|enum|abstract class)\s+([A-Za-z_$][\w$]*)`)
	pyDecl   = regexp.MustCompile(`^(?:async )?def ([a-z_]\w*)\(|^class ([A-Z]\w*)\b`)
	rbDecl   = regexp.MustCompile(`^\s*def (?:self\.)?([a-z_]\w*[?!]?)|^class ([A-Z]\w*)\b|^module ([A-Z]\w*)\b`)
	routeRe  = regexp.MustCompile(`(?i)\b(?:router|app|r|e|g|group|mux|api)\.(get|post|put|patch|delete|options|head|handle(?:func)?)\(\s*["'` + "`" + `]([^"'` + "`" + `]+)["'` + "`" + `]|@(?:Get|Post|Put|Patch|Delete)\(\s*["'` + "`" + `]([^"'` + "`" + `]*)["'` + "`" + `]`)
	gqlDecl  = regexp.MustCompile(`^(?:extend )?(type|input|enum|interface|union|scalar) ([A-Za-z_]\w*)`)
	protoRe  = regexp.MustCompile(`^(?:message|service|enum|rpc) ([A-Za-z_]\w*)`)
	sqlDDL   = regexp.MustCompile(`(?i)^\s*(create|alter|drop) (table|index|type|view|function|schema)\s+(?:if (?:not )?exists\s+)?([A-Za-z_."]+)`)
	openAPI  = regexp.MustCompile(`^\s{2}(/[^:]+):\s*$`)
	migrDir  = regexp.MustCompile(`(?i)(^|/)(migrations?|db/migrate|prisma/migrations|alembic/versions)/`)
	contract = regexp.MustCompile(`(?i)(^|/)(openapi|swagger)[^/]*\.(ya?ml|json)$|\.graphqls?$|\.proto$|(^|/)schema\.(prisma|graphql|json|sql)$`)
	configRe = regexp.MustCompile(`(?i)(^|/)(corgi-compose\.ya?ml|docker-compose[^/]*\.ya?ml|\.env\.example|package\.json|go\.mod|pyproject\.toml|Cargo\.toml|Gemfile|serverless\.ya?ml|terraform/[^/]+\.tf)$`)
)

// SurfaceOf reads the changed surface out of a repository's file diffs.
func SurfaceOf(rd RepoDiff) RepoSurface {
	out := RepoSurface{Service: rd.Service, Repo: rd.Repo}
	for _, f := range rd.Files {
		out.Changes = append(out.Changes, surfaceOfFile(f)...)
	}
	sort.SliceStable(out.Changes, func(i, j int) bool {
		if out.Changes[i].Breaking != out.Changes[j].Breaking {
			return out.Changes[i].Breaking
		}
		return kindRank(out.Changes[i].Kind) < kindRank(out.Changes[j].Kind)
	})
	return out
}

func kindRank(kind string) int {
	switch kind {
	case "migration":
		return 0
	case "contract":
		return 1
	case "route":
		return 2
	case "symbol":
		return 3
	}
	return 4
}

func surfaceOfFile(f FileDiff) []SurfaceChange {
	var out []SurfaceChange
	op := "changed"
	if f.New {
		op = "added"
	}
	switch {
	case migrDir.MatchString(f.Path):
		out = append(out, SurfaceChange{Kind: "migration", Name: baseName(f.Path), Path: f.Path, Op: op, Breaking: sqlBreaks(f.Patch)})
		return out
	case contract.MatchString(f.Path):
		out = append(out, SurfaceChange{Kind: "contract", Name: baseName(f.Path), Path: f.Path, Op: op})
		out = append(out, contractDecls(f)...)
		return out
	case configRe.MatchString(f.Path):
		out = append(out, SurfaceChange{Kind: "config", Name: baseName(f.Path), Path: f.Path, Op: op})
		return out
	}
	if f.Binary || f.Patch == "" {
		return nil
	}
	added, removed := map[string]string{}, map[string]string{}
	for _, line := range strings.Split(f.Patch, "\n") {
		if len(line) < 2 || strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			continue
		}
		sign, body := line[0], line[1:]
		if sign != '+' && sign != '-' {
			continue
		}
		kind, name := declOf(f.Path, body)
		if name == "" {
			continue
		}
		key := kind + " " + name
		if sign == '+' {
			added[key] = strings.TrimSpace(body)
		} else {
			removed[key] = strings.TrimSpace(body)
		}
	}
	for key, line := range added {
		kind, name := splitKey(key)
		if old, ok := removed[key]; ok {
			if old != line {
				out = append(out, SurfaceChange{Kind: kind, Name: name, Path: f.Path, Op: "changed", Breaking: kind == "symbol" || kind == "route"})
			}
			delete(removed, key)
			continue
		}
		out = append(out, SurfaceChange{Kind: kind, Name: name, Path: f.Path, Op: "added"})
	}
	for key := range removed {
		kind, name := splitKey(key)
		out = append(out, SurfaceChange{Kind: kind, Name: name, Path: f.Path, Op: "removed", Breaking: true})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func splitKey(key string) (kind, name string) {
	i := strings.Index(key, " ")
	return key[:i], key[i+1:]
}

func baseName(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

// declOf names the public declaration a line makes, by the file's language.
func declOf(path, line string) (kind, name string) {
	if m := routeRe.FindStringSubmatch(line); m != nil {
		route := m[2]
		if route == "" {
			route = m[3]
		}
		return "route", strings.ToUpper(m[1]) + " " + route
	}
	switch {
	case strings.HasSuffix(path, ".go"):
		if strings.HasSuffix(path, "_test.go") {
			return "", ""
		}
		if m := goDecl.FindStringSubmatch(line); m != nil {
			return "symbol", firstGroup(m[1:])
		}
	case strings.HasSuffix(path, ".ts"), strings.HasSuffix(path, ".tsx"), strings.HasSuffix(path, ".js"), strings.HasSuffix(path, ".mjs"):
		if strings.Contains(path, ".test.") || strings.Contains(path, ".spec.") || strings.Contains(path, "__tests__/") {
			return "", ""
		}
		if m := tsDecl.FindStringSubmatch(line); m != nil {
			return "symbol", m[1]
		}
	case strings.HasSuffix(path, ".py"):
		if strings.Contains(path, "test") {
			return "", ""
		}
		if m := pyDecl.FindStringSubmatch(line); m != nil && !strings.HasPrefix(firstGroup(m[1:]), "_") {
			return "symbol", firstGroup(m[1:])
		}
	case strings.HasSuffix(path, ".rb"):
		if strings.Contains(path, "spec/") || strings.Contains(path, "test/") {
			return "", ""
		}
		if m := rbDecl.FindStringSubmatch(line); m != nil {
			return "symbol", firstGroup(m[1:])
		}
	}
	return "", ""
}

func firstGroup(groups []string) string {
	for _, g := range groups {
		if g != "" {
			return g
		}
	}
	return ""
}

// contractDecls lists the types, paths, messages a contract file gained or
// lost, so "openapi.yaml changed" says what changed in it.
func contractDecls(f FileDiff) []SurfaceChange {
	var out []SurfaceChange
	seen := map[string]bool{}
	for _, line := range strings.Split(f.Patch, "\n") {
		if len(line) < 2 || strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			continue
		}
		sign, body := line[0], line[1:]
		if sign != '+' && sign != '-' {
			continue
		}
		name := ""
		if m := gqlDecl.FindStringSubmatch(body); m != nil {
			name = m[1] + " " + m[2]
		} else if m := protoRe.FindStringSubmatch(body); m != nil {
			name = m[1]
		} else if m := openAPI.FindStringSubmatch(body); m != nil {
			name = m[1]
		}
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		op := "added"
		if sign == '-' {
			op = "removed"
		}
		out = append(out, SurfaceChange{Kind: "contract", Name: name, Path: f.Path, Op: op, Breaking: op == "removed"})
	}
	return out
}

// sqlBreaks says whether a migration drops or alters something.
func sqlBreaks(patch string) bool {
	for _, line := range strings.Split(patch, "\n") {
		if !strings.HasPrefix(line, "+") {
			continue
		}
		if m := sqlDDL.FindStringSubmatch(line[1:]); m != nil && (strings.EqualFold(m[1], "drop") || strings.EqualFold(m[1], "alter")) {
			return true
		}
	}
	return false
}

// SurfaceMarkdown renders the changed surface for a pull request body.
func SurfaceMarkdown(repos []RepoSurface) string {
	var b strings.Builder
	b.WriteString("## Changed surface\n")
	any := false
	for _, r := range repos {
		if len(r.Changes) == 0 {
			continue
		}
		any = true
		if len(repos) > 1 {
			b.WriteString("\n**" + r.Service + "**\n")
		}
		for _, c := range r.Changes {
			mark := ""
			if c.Breaking {
				mark = " ⚠ breaking"
			}
			b.WriteString("- " + c.Op + " " + c.Kind + " `" + c.Name + "` — " + c.Path + mark + "\n")
		}
	}
	if !any {
		b.WriteString("- nothing public changed: no exported symbol, route, contract, migration or config\n")
	}
	return b.String()
}
