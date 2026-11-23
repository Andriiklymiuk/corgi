package watch

import (
	"strings"
	"testing"
)

const samplePatch = "@@ -10,4 +10,5 @@ func handler() {\n keep\n-\tauthorize(user)\n+\t// TODO later\n+\tconsole.log(user)\n keep too\n\\ No newline at end of file"

func TestAPatchBecomesNumberedLines(t *testing.T) {
	hunks, cut := ParsePatch(samplePatch)
	if cut || len(hunks) != 1 {
		t.Fatalf("hunks = %d cut = %v", len(hunks), cut)
	}
	h := hunks[0]
	if h.Header != "func handler() {" || h.Adds != 2 || h.Dels != 1 || len(h.Lines) != 5 {
		t.Fatalf("hunk = %+v", h)
	}
	if h.Lines[1].T != "-" || h.Lines[1].Old != 11 || h.Lines[2].T != "+" || h.Lines[2].New != 11 || h.Lines[4].New != 13 {
		t.Fatalf("lines = %+v", h.Lines)
	}
	got := strings.Join(h.Flags, "|")
	for _, want := range []string{"a guard is removed", "a TODO is left", "a debug line is left"} {
		if !strings.Contains(got, want) {
			t.Errorf("flags %q miss %q", got, want)
		}
	}
}

func TestAMovedGuardIsNotARemovedGuard(t *testing.T) {
	hunks, _ := ParsePatch("@@ -1,2 +1,2 @@\n-authorize(user)\n+authorize(user)\n")
	if len(hunks[0].Flags) != 0 {
		t.Fatalf("flags = %v", hunks[0].Flags)
	}
}

func TestALongFileIsCut(t *testing.T) {
	patch := "@@ -1,0 +1,500 @@\n" + strings.Repeat("+line\n", 500)
	hunks, cut := ParsePatch(patch)
	if !cut || len(hunks[0].Lines) != changeFileLineCap || hunks[0].Adds != 500 {
		t.Fatalf("cut = %v lines = %d adds = %d", cut, len(hunks[0].Lines), hunks[0].Adds)
	}
	if got := clipLine(strings.Repeat("x", changeLineCap+5)); !strings.HasSuffix(got, "…") {
		t.Fatalf("a long line is not clipped")
	}
}

func TestFilesAreSortedIntoWhatNeedsEyes(t *testing.T) {
	file := func(path, status string, adds, dels int, patch string) ChangeFile {
		f := ChangeFile{Path: path, Status: status, Adds: adds, Dels: dels}
		f.Hunks, _ = ParsePatch(patch)
		return f
	}
	plain := "@@ -1,1 +1,2 @@\n keep\n+more\n"
	c := Change{Files: []ChangeFile{
		file("src/list.ts", "modified", 1, 0, plain),
		file("bun.lock", "modified", 900, 300, plain),
		file("src/list.test.ts", "modified", 40, 2, plain),
		file("api/handlers_test.go", "modified", 0, 30, plain),
		file("docs/guide.md", "modified", 12, 0, plain),
		file("src/__generated__/types.ts", "modified", 200, 100, plain),
		file("db/migrations/007_add.sql", "added", 9, 0, "@@ -0,0 +1,1 @@\n+DROP TABLE users;\n"),
		file("src/old.ts", "removed", 0, 50, plain),
		file("src/new-name.ts", "renamed", 0, 0, ""),
		file("assets/logo.png", "added", 0, 0, ""),
		file("src/auth/guard.ts", "modified", 3, 1, plain),
	}}
	c.Sort()
	if c.Total != 1648 || c.Eyes != 44 {
		t.Fatalf("total = %d eyes = %d", c.Total, c.Eyes)
	}
	if c.Files[0].Path != "db/migrations/007_add.sql" || c.Files[0].Why != "changes the database" {
		t.Fatalf("first = %+v", c.Files[0])
	}
	byPath := map[string]ChangeFile{}
	for _, f := range c.Files {
		byPath[f.Path] = f
	}
	for path, kind := range map[string]string{"bun.lock": KindLock, "src/list.test.ts": KindTests, "docs/guide.md": KindDocs, "src/__generated__/types.ts": KindGenerated,
		"src/old.ts": KindRemoved, "src/new-name.ts": KindMoved, "assets/logo.png": KindAssets, "src/list.ts": KindCode} {
		if byPath[path].Kind != kind {
			t.Errorf("%s is %q, want %q", path, byPath[path].Kind, kind)
		}
	}
	if !byPath["api/handlers_test.go"].Eyes || byPath["api/handlers_test.go"].Why != "tests are removed" {
		t.Errorf("removed tests are folded away: %+v", byPath["api/handlers_test.go"])
	}
	if byPath["bun.lock"].Eyes || byPath["bun.lock"].Hunks != nil {
		t.Errorf("a lock file keeps its lines")
	}
	if byPath["src/auth/guard.ts"].Why != "who may do what" || sensitiveWhy("src/author.ts") != "" {
		t.Errorf("auth paths are misread")
	}
	if len(c.Skipped) != 7 || c.Skipped[0].Kind != KindTests || c.Skipped[0].Lines != 42 {
		t.Fatalf("skipped = %+v", c.Skipped)
	}
}
