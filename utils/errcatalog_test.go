package utils

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

func declaredCodes(t *testing.T, files ...string) []string {
	t.Helper()
	var codes []string
	for _, file := range files {
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			vs, ok := n.(*ast.ValueSpec)
			if !ok || len(vs.Values) != 1 {
				return true
			}
			lit, ok := vs.Values[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			v, _ := strconv.Unquote(lit.Value)
			if strings.HasPrefix(v, "E_") || strings.HasPrefix(v, "W_") {
				codes = append(codes, v)
			}
			return true
		})
	}
	return codes
}

func TestEveryDeclaredCodeIsInTheCatalog(t *testing.T) {
	codes := declaredCodes(t, "errcodes.go", "validate.go")
	if len(codes) < 20 {
		t.Fatalf("found only %d codes; did the declarations move?", len(codes))
	}
	for _, c := range codes {
		info, ok := LookupCode(c)
		if !ok {
			t.Errorf("%s has no ErrorCatalog entry: add its meaning and fix", c)
			continue
		}
		if info.Meaning == "" || info.Fix == "" {
			t.Errorf("%s needs both a meaning and a fix", c)
		}
		if strings.HasPrefix(c, "W_") != info.Warning {
			t.Errorf("%s: Warning must be set exactly for W_ codes", c)
		}
	}
}

func TestTheCatalogHasNoDuplicates(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range ErrorCatalog {
		if seen[c.Code] {
			t.Errorf("%s is listed twice", c.Code)
		}
		seen[c.Code] = true
	}
}

func TestTheAgentsGuideNamesEveryCode(t *testing.T) {
	doc, err := os.ReadFile("../docs/agents.md")
	if err != nil {
		t.Skip("docs/agents.md not found")
	}
	for _, c := range ErrorCatalog {
		if !strings.Contains(string(doc), "`"+c.Code+"`") {
			t.Errorf("docs/agents.md does not mention %s", c.Code)
		}
	}
}

func TestLookupCodeIgnoresCase(t *testing.T) {
	if c, ok := LookupCode(" e_port_conflict "); !ok || c.Code != ErrPortConflict {
		t.Fatalf("got %+v %v", c, ok)
	}
	if _, ok := LookupCode("E_NOPE"); ok {
		t.Fatal("an unknown code must not resolve")
	}
}

func TestAJSONErrorCarriesItsFixAndLookup(t *testing.T) {
	var b strings.Builder
	WriteJSONError(&b, ErrDockerDown, "docker is not running")
	out := b.String()
	for _, want := range []string{`"fix": "start Docker`, `"lookup": "corgi vocab E_DOCKER_DOWN"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in %s", want, out)
		}
	}
	b.Reset()
	WriteJSONErrorFix(&b, ErrUsage, "unknown flag", "did you mean --watch?")
	if !strings.Contains(b.String(), `"fix": "did you mean --watch?"`) {
		t.Errorf("a specific fix must win over the catalog's: %s", b.String())
	}
	b.Reset()
	WriteJSONError(&b, "E_SOMETHING_ELSE", "x")
	if strings.Contains(b.String(), "fix") || strings.Contains(b.String(), "lookup") {
		t.Errorf("an uncatalogued code carries no fix or lookup: %s", b.String())
	}
}
