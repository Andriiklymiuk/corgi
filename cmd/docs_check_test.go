package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"andriiklymiuk/corgi/utils"
)

// A doc that names a changed symbol or route is one to re-read; a CLAUDE.md
// pointer past the end of its file, or to no file, is stale.
func TestDocsCheckFindsMentionsAndStalePointers(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("docs/limits.md", "# Limits\n\nCall `CheckLimit` before a request.\nPOST /limits/reset clears it.\n")
	write("docs/other.md", "Nothing about CheckLimits here (a longer word).\n")
	write("api/limits.go", "package api\nfunc CheckLimit() {}\n")
	write("CLAUDE.md", "See api/limits.go:2 for the check and api/limits.go:40 for the reset; also api/gone.go:1.\n")
	repos := []utils.RepoSurface{{Service: "api", Changes: []utils.SurfaceChange{
		{Kind: "symbol", Name: "CheckLimit", Op: "changed", Path: "api/limits.go", Breaking: true},
		{Kind: "route", Name: "POST /limits/reset", Op: "added", Path: "api/routes.go"},
		{Kind: "config", Name: "go.mod", Op: "changed", Path: "go.mod"},
	}}}
	r := checkDocs([]string{root}, repos)
	if r.DocsScanned != 3 {
		t.Fatalf("scanned %d docs", r.DocsScanned)
	}
	if len(r.Mentions) != 2 {
		t.Fatalf("mentions: %+v", r.Mentions)
	}
	if r.Mentions[0].Doc != filepath.Join(root, "docs", "limits.md") || r.Mentions[0].Name != "CheckLimit" || r.Mentions[1].Name != "POST /limits/reset" {
		t.Fatalf("mentions: %+v", r.Mentions)
	}
	if len(r.StalePointers) != 2 {
		t.Fatalf("stale pointers: %+v", r.StalePointers)
	}
	if r.StalePointers[0].Why != "the file has 2 lines" || r.StalePointers[1].Why != "no such file" {
		t.Fatalf("why: %+v", r.StalePointers)
	}
}
