package utils

import (
	"regexp"
	"sort"
	"strings"
)

type SurfaceChange struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	Op       string `json:"op"`
	Breaking bool   `json:"breaking,omitempty"`
}

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
	op := "changed"
	if f.New {
		op = "added"
	}
	switch {
	case migrDir.MatchString(f.Path):
		return []SurfaceChange{{Kind: "migration", Name: baseName(f.Path), Path: f.Path, Op: op, Breaking: sqlBreaks(f.Patch)}}
	case contract.MatchString(f.Path):
		return append([]SurfaceChange{{Kind: "contract", Name: baseName(f.Path), Path: f.Path, Op: op}}, contractDecls(f)...)
	case configRe.MatchString(f.Path):
		return []SurfaceChange{{Kind: "config", Name: baseName(f.Path), Path: f.Path, Op: op}}
	}
	if f.Binary || f.Patch == "" {
		return nil
	}
	added, removed := declsInPatch(f)
	out := declChanges(f.Path, added, removed)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func patchLines(patch string, each func(sign byte, body string)) {
	for _, line := range strings.Split(patch, "\n") {
		if len(line) < 2 || strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			continue
		}
		if sign := line[0]; sign == '+' || sign == '-' {
			each(sign, line[1:])
		}
	}
}

func declsInPatch(f FileDiff) (added, removed map[string]string) {
	added, removed = map[string]string{}, map[string]string{}
	patchLines(f.Patch, func(sign byte, body string) {
		kind, name := declOf(f.Path, body)
		if name == "" {
			return
		}
		if sign == '+' {
			added[kind+" "+name] = strings.TrimSpace(body)
		} else {
			removed[kind+" "+name] = strings.TrimSpace(body)
		}
	})
	return added, removed
}

func declChanges(path string, added, removed map[string]string) []SurfaceChange {
	var out []SurfaceChange
	for key, line := range added {
		kind, name := splitKey(key)
		old, ok := removed[key]
		delete(removed, key)
		switch {
		case !ok:
			out = append(out, SurfaceChange{Kind: kind, Name: name, Path: path, Op: "added"})
		case old != line:
			out = append(out, SurfaceChange{Kind: kind, Name: name, Path: path, Op: "changed", Breaking: kind == "symbol" || kind == "route"})
		}
	}
	for key := range removed {
		kind, name := splitKey(key)
		out = append(out, SurfaceChange{Kind: kind, Name: name, Path: path, Op: "removed", Breaking: true})
	}
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

type declLanguage struct {
	files  func(path string) bool
	isTest func(path string) bool
	name   func(line string) string
}

func hasSuffix(suffixes ...string) func(string) bool {
	return func(path string) bool {
		for _, s := range suffixes {
			if strings.HasSuffix(path, s) {
				return true
			}
		}
		return false
	}
}

func containsAny(parts ...string) func(string) bool {
	return func(path string) bool {
		for _, p := range parts {
			if strings.Contains(path, p) {
				return true
			}
		}
		return false
	}
}

func groupOf(re *regexp.Regexp) func(string) string {
	return func(line string) string {
		if m := re.FindStringSubmatch(line); m != nil {
			return firstGroup(m[1:])
		}
		return ""
	}
}

var declLanguages = []declLanguage{
	{files: hasSuffix(".go"), isTest: hasSuffix("_test.go"), name: groupOf(goDecl)},
	{files: hasSuffix(".ts", ".tsx", ".js", ".mjs"), isTest: containsAny(".test.", ".spec.", "__tests__/"), name: func(line string) string {
		if m := tsDecl.FindStringSubmatch(line); m != nil {
			return m[1]
		}
		return ""
	}},
	{files: hasSuffix(".py"), isTest: containsAny("test"), name: func(line string) string {
		if n := groupOf(pyDecl)(line); !strings.HasPrefix(n, "_") {
			return n
		}
		return ""
	}},
	{files: hasSuffix(".rb"), isTest: containsAny("spec/", "test/"), name: groupOf(rbDecl)},
}

func declOf(path, line string) (kind, name string) {
	if m := routeRe.FindStringSubmatch(line); m != nil {
		route := m[2]
		if route == "" {
			route = m[3]
		}
		return "route", strings.ToUpper(m[1]) + " " + route
	}
	for _, lang := range declLanguages {
		if !lang.files(path) {
			continue
		}
		if lang.isTest(path) {
			return "", ""
		}
		if n := lang.name(line); n != "" {
			return "symbol", n
		}
		return "", ""
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

func contractName(body string) string {
	if m := gqlDecl.FindStringSubmatch(body); m != nil {
		return m[1] + " " + m[2]
	}
	if m := protoRe.FindStringSubmatch(body); m != nil {
		return m[1]
	}
	if m := openAPI.FindStringSubmatch(body); m != nil {
		return m[1]
	}
	return ""
}

func contractDecls(f FileDiff) []SurfaceChange {
	var out []SurfaceChange
	seen := map[string]bool{}
	patchLines(f.Patch, func(sign byte, body string) {
		name := contractName(body)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		op := "added"
		if sign == '-' {
			op = "removed"
		}
		out = append(out, SurfaceChange{Kind: "contract", Name: name, Path: f.Path, Op: op, Breaking: op == "removed"})
	})
	return out
}

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

func SurfaceMarkdown(repos []RepoSurface) string {
	var b strings.Builder
	b.WriteString("## Changed surface\n")
	found := false
	for _, r := range repos {
		if len(r.Changes) == 0 {
			continue
		}
		found = true
		if len(repos) > 1 {
			b.WriteString("\n**" + r.Service + "**\n")
		}
		for _, c := range r.Changes {
			mark := ""
			if c.Breaking {
				mark = " ⚠ breaking"
			}
			b.WriteString("- " + c.Op + " " + c.Kind + " `" + c.Name + "` - " + c.Path + mark + "\n")
		}
	}
	if !found {
		b.WriteString("- nothing public changed: no exported symbol, route, contract, migration or config\n")
	}
	return b.String()
}
