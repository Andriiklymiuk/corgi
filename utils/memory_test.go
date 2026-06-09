package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFact(t *testing.T, root, sub, name, body string) {
	t.Helper()
	dir := filepath.Join(root, sub)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadFactsAbsentStoreIsEmpty(t *testing.T) {
	facts, err := ReadFacts(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("absent store must not error, got %v", err)
	}
	if len(facts) != 0 {
		t.Fatalf("absent store must be empty, got %d", len(facts))
	}
}

func TestReadFactsParsesFrontmatter(t *testing.T) {
	root := filepath.Join(t.TempDir(), "memory")
	writeFact(t, root, "decisions", "postgres-over-mysql", `---
name: postgres-over-mysql
description: Chose Postgres over MySQL.
type: decision
service: api
links: ["[[billing-rules]]"]
---

Body here.
`)
	facts, err := ReadFacts(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 {
		t.Fatalf("want 1 fact, got %d", len(facts))
	}
	f := facts[0]
	if f.Name != "postgres-over-mysql" || f.Type != "decision" || f.Service != "api" {
		t.Fatalf("bad parse: %+v", f)
	}
	if f.Description != "Chose Postgres over MySQL." {
		t.Fatalf("bad description: %q", f.Description)
	}
	if len(f.Links) != 1 || f.Links[0] != "billing-rules" {
		t.Fatalf("links should be normalized to bare names, got %v", f.Links)
	}
}

func TestReadFactsSortedByTypeThenName(t *testing.T) {
	root := filepath.Join(t.TempDir(), "memory")
	writeFact(t, root, "incidents", "b-incident", "---\nname: b-incident\ndescription: x\ntype: incident\n---\n")
	writeFact(t, root, "decisions", "z-decision", "---\nname: z-decision\ndescription: x\ntype: decision\n---\n")
	writeFact(t, root, "decisions", "a-decision", "---\nname: a-decision\ndescription: x\ntype: decision\n---\n")
	facts, err := ReadFacts(root)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{facts[0].Name, facts[1].Name, facts[2].Name}
	want := []string{"a-decision", "z-decision", "b-incident"} // decisions first (a,z), then incidents
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order: got %v want %v", got, want)
		}
	}
}

func TestAddFactWritesValidFrontmatter(t *testing.T) {
	root := filepath.Join(t.TempDir(), "memory")
	path, err := AddFact(root, Fact{
		Name: "retry-on-429", Description: "Backoff on 429 in billing.",
		Type: "fix", Service: "billing", Pattern: "retry-on-429",
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "fixes", "retry-on-429.md"); path != want {
		t.Fatalf("path: got %q want %q", path, want)
	}
	facts, err := ReadFacts(root)
	if err != nil || len(facts) != 1 {
		t.Fatalf("round-trip read failed: %v len=%d", err, len(facts))
	}
	if facts[0].Pattern != "retry-on-429" {
		t.Fatalf("pattern not persisted: %+v", facts[0])
	}
}

func TestAddFactRejectsUnknownType(t *testing.T) {
	if _, err := AddFact(t.TempDir(), Fact{Name: "x", Type: "bogus"}); err == nil {
		t.Fatal("unknown type must error")
	}
}

func TestRenderIndexGroupsByType(t *testing.T) {
	root := filepath.Join(t.TempDir(), "memory")
	mustAdd(t, root, Fact{Name: "pg", Description: "d1", Type: "decision"})
	mustAdd(t, root, Fact{Name: "oom", Description: "d2", Type: "incident"})
	idx := RenderIndex(mustRead(t, root))
	if !strings.Contains(idx, "## decisions") || !strings.Contains(idx, "## incidents") {
		t.Fatalf("index missing type headings:\n%s", idx)
	}
	if !strings.Contains(idx, "**pg**") || !strings.Contains(idx, "do not edit by hand") {
		t.Fatalf("index missing entry or banner:\n%s", idx)
	}
}

func mustAdd(t *testing.T, root string, f Fact) {
	t.Helper()
	if _, err := AddFact(root, f); err != nil {
		t.Fatal(err)
	}
}
func mustRead(t *testing.T, root string) []Fact {
	t.Helper()
	facts, err := ReadFacts(root)
	if err != nil {
		t.Fatal(err)
	}
	return facts
}

func TestLintCatchesPlantedSecret(t *testing.T) {
	root := filepath.Join(t.TempDir(), "memory")
	mustAdd(t, root, Fact{
		Name: "leaky", Description: "has a key", Type: "decision",
		Body: "the key is AKIAIOSFODNN7EXAMPLE do not", // AWS-key shape
	})
	errs, _ := LintFacts(root)
	if !hasCode(errs, "E_MEMORY_SECRET") {
		t.Fatalf("planted secret not flagged: %+v", errs)
	}
}

func TestLintCatchesSecretInLooseRootFile(t *testing.T) {
	root := filepath.Join(t.TempDir(), "memory")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	// A loose .md dropped in the store root — not in a typed subdir, so ReadFacts skips it.
	loose := filepath.Join(root, "loose.md")
	if err := os.WriteFile(loose, []byte("notes\nAKIAIOSFODNN7EXAMPLE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	errs, _ := LintFacts(root)
	if !hasCode(errs, "E_MEMORY_SECRET") {
		t.Fatalf("secret in loose root file not flagged: %+v", errs)
	}
	if !hasFile(errs, loose) {
		t.Fatalf("E_MEMORY_SECRET should name the loose file %q: %+v", loose, errs)
	}
}

func TestLintCatchesSecretInIndexMd(t *testing.T) {
	root := filepath.Join(t.TempDir(), "memory")
	mustAdd(t, root, Fact{Name: "ok", Description: "clean fact", Type: "domain"})
	// index.md lives at the store root and is never a typed fact.
	if err := os.WriteFile(filepath.Join(root, "index.md"), []byte("token=supersecretvalue\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	errs, _ := LintFacts(root)
	if !hasCode(errs, "E_MEMORY_SECRET") {
		t.Fatalf("secret in index.md not flagged: %+v", errs)
	}
}

func TestLintDoesNotDoubleReportTypedFactSecret(t *testing.T) {
	root := filepath.Join(t.TempDir(), "memory")
	mustAdd(t, root, Fact{
		Name: "leaky", Description: "has a key", Type: "decision",
		Body: "the key is AKIAIOSFODNN7EXAMPLE do not",
	})
	errs, _ := LintFacts(root)
	n := 0
	for _, e := range errs {
		if e.Code == "E_MEMORY_SECRET" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("typed-fact secret should be reported exactly once, got %d: %+v", n, errs)
	}
}

func TestLooseRootFileIsNotAFact(t *testing.T) {
	root := filepath.Join(t.TempDir(), "memory")
	mustAdd(t, root, Fact{Name: "real", Description: "a real fact", Type: "domain"})
	// Loose .md at the root and index.md must not surface in list/index.
	if err := os.WriteFile(filepath.Join(root, "loose.md"), []byte("just notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.md"), []byte("# index\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	facts := mustRead(t, root)
	if len(facts) != 1 || facts[0].Name != "real" {
		t.Fatalf("list/index must only return typed-subdir facts, got %+v", facts)
	}
}

func TestLintFlagsTypeDirMismatch(t *testing.T) {
	root := filepath.Join(t.TempDir(), "memory")
	// fact says type: decision but lives under incidents/
	writeFact(t, root, "incidents", "wrong", "---\nname: wrong\ndescription: x\ntype: decision\n---\n")
	errs, _ := LintFacts(root)
	if !hasCode(errs, "E_MEMORY_TYPE_MISMATCH") {
		t.Fatalf("type/dir mismatch not flagged: %+v", errs)
	}
}

func TestLintFlagsBrokenLinkAsWarning(t *testing.T) {
	root := filepath.Join(t.TempDir(), "memory")
	mustAdd(t, root, Fact{Name: "a", Description: "x", Type: "decision", Links: []string{"ghost"}})
	_, warns := LintFacts(root)
	if !hasCode(warns, "E_MEMORY_DANGLING_LINK") {
		t.Fatalf("dangling link not warned: %+v", warns)
	}
}

func TestLintCleanStorePasses(t *testing.T) {
	root := filepath.Join(t.TempDir(), "memory")
	mustAdd(t, root, Fact{Name: "ok", Description: "clean fact", Type: "domain"})
	errs, warns := LintFacts(root)
	if len(errs) != 0 || len(warns) != 0 {
		t.Fatalf("clean store should pass: errs=%+v warns=%+v", errs, warns)
	}
}

func hasCode(issues []MemoryIssue, code string) bool {
	for _, i := range issues {
		if i.Code == code {
			return true
		}
	}
	return false
}

func hasFile(issues []MemoryIssue, file string) bool {
	for _, i := range issues {
		if i.File == file {
			return true
		}
	}
	return false
}
