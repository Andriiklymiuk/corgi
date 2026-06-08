package utils

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// prependPATH points PATH at bin for the duration of the test.
func prependPATH(t *testing.T, bin string) {
	t.Helper()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// --- dbdump.go: RunPgDump / RunPgSeed via fake binaries on PATH ---

func TestRunPgDump(t *testing.T) {
	db := DatabaseService{Host: "h", Port: 5432, User: "u", DatabaseName: "app", Password: "p"}

	t.Run("success", func(t *testing.T) {
		bin := t.TempDir()
		writeFakeBin(t, bin, "pg_dump", `exit 0`)
		prependPATH(t, bin)
		if err := RunPgDump(db, t.TempDir(), "dump.sql"); err != nil {
			t.Fatalf("RunPgDump = %v, want nil", err)
		}
	})

	t.Run("failure propagates", func(t *testing.T) {
		bin := t.TempDir()
		writeFakeBin(t, bin, "pg_dump", `echo boom >&2; exit 3`)
		prependPATH(t, bin)
		if err := RunPgDump(db, t.TempDir(), "dump.sql"); err == nil {
			t.Fatal("RunPgDump should propagate a non-zero pg_dump exit")
		}
	})
}

func TestRunPgSeed(t *testing.T) {
	db := DatabaseService{User: "u", DatabaseName: "app"}

	t.Run("success", func(t *testing.T) {
		bin := t.TempDir()
		writeFakeBin(t, bin, "docker", `cat >/dev/null; exit 0`)
		prependPATH(t, bin)
		svc := t.TempDir()
		if err := os.WriteFile(filepath.Join(svc, "seed.sql"), []byte("SELECT 1;\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := RunPgSeed(svc, "seed.sql", "container1", db); err != nil {
			t.Fatalf("RunPgSeed = %v, want nil", err)
		}
	})

	t.Run("missing dump file", func(t *testing.T) {
		if err := RunPgSeed(t.TempDir(), "nope.sql", "c", db); err == nil {
			t.Fatal("RunPgSeed should fail to open a missing dump file")
		}
	})

	t.Run("docker failure propagates", func(t *testing.T) {
		bin := t.TempDir()
		writeFakeBin(t, bin, "docker", `cat >/dev/null; exit 1`)
		prependPATH(t, bin)
		svc := t.TempDir()
		if err := os.WriteFile(filepath.Join(svc, "seed.sql"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := RunPgSeed(svc, "seed.sql", "c", db); err == nil {
			t.Fatal("RunPgSeed should propagate a non-zero docker exit")
		}
	})
}

func TestJoinIfRelative(t *testing.T) {
	if got := joinIfRelative("/base", "/abs/path"); got != "/abs/path" {
		t.Errorf("absolute path = %q, want /abs/path", got)
	}
	if got := joinIfRelative("/base", "rel.sql"); got != "/base/rel.sql" {
		t.Errorf("relative path = %q, want /base/rel.sql", got)
	}
}

func TestMergeEnv(t *testing.T) {
	out := mergeEnv([]string{"A=1"}, map[string]string{"B": "2"})
	var hasA, hasB bool
	for _, e := range out {
		hasA = hasA || e == "A=1"
		hasB = hasB || e == "B=2"
	}
	if !hasA || !hasB {
		t.Fatalf("mergeEnv = %v, want both A=1 and B=2", out)
	}
}

// --- agentwork.go: GitLab MR probe + CI rollup ---

func TestProbeAgentWork_GitlabMR(t *testing.T) {
	bin := t.TempDir()
	writeFakeBin(t, bin, "git", `
case "$*" in
  *"rev-parse --abbrev-ref HEAD"*) echo "feature/mr" ;;
  *"rev-parse --git-dir"*)         echo ".git" ;;
  *"status --porcelain"*)          echo "" ;;
  *) echo "" ;;
esac`)
	// gh present but finds no PR (exit 1) so the probe falls through to glab.
	writeFakeBin(t, bin, "gh", `exit 1`)
	writeFakeBin(t, bin, "glab", `echo '[{"iid":7,"state":"opened","draft":false,"web_url":"https://gl/mr/7"}]'`)
	prependPATH(t, bin)

	aw := ProbeAgentWork(t.TempDir())
	if aw == nil || aw.PR == nil {
		t.Fatalf("expected a gitlab MR, got %+v", aw)
	}
	if aw.PR.Provider != "gitlab" || aw.PR.Number != 7 || aw.PR.State != "opened" || aw.PR.CI != "none" {
		t.Errorf("mr = %+v", aw.PR)
	}
}

func TestProbeAgentWork_NoForgeNoPR(t *testing.T) {
	bin := t.TempDir()
	writeFakeBin(t, bin, "git", `
case "$*" in
  *"rev-parse --abbrev-ref HEAD"*) echo "feature/x" ;;
  *"rev-parse --git-dir"*)         echo ".git" ;;
  *) echo "" ;;
esac`)
	// gh and glab both present but return nothing → no PR, but branch still set.
	writeFakeBin(t, bin, "gh", `exit 1`)
	writeFakeBin(t, bin, "glab", `echo '[]'`)
	prependPATH(t, bin)

	aw := ProbeAgentWork(t.TempDir())
	if aw == nil || aw.Branch != "feature/x" {
		t.Fatalf("expected branch with no PR, got %+v", aw)
	}
	if aw.PR != nil {
		t.Errorf("expected nil PR, got %+v", aw.PR)
	}
}

func TestRollupCI(t *testing.T) {
	cases := []struct {
		name   string
		checks []ciCheck
		want   string
	}{
		{"empty", nil, "none"},
		{"all pass", []ciCheck{{Conclusion: "SUCCESS"}, {Conclusion: "SUCCESS"}}, "passing"},
		{"any failure wins", []ciCheck{{Conclusion: "SUCCESS"}, {Conclusion: "FAILURE"}}, "failing"},
		{"pending downgrades", []ciCheck{{Conclusion: "SUCCESS"}, {Conclusion: "PENDING"}}, "pending"},
	}
	for _, c := range cases {
		if got := rollupCI(c.checks); got != c.want {
			t.Errorf("%s: rollupCI = %q, want %q", c.name, got, c.want)
		}
	}
}

// --- validate.go: AbortOnValidationErrors + duplicate-key branch ---

func TestAbortOnValidationErrors_Clean(t *testing.T) {
	clean := &CorgiCompose{Services: []Service{{ServiceName: "api", Port: 3000, Start: []string{"x"}}}}
	if !AbortOnValidationErrors(clean) {
		t.Fatal("a clean compose must be safe to proceed (true)")
	}
}

func TestAbortOnValidationErrors_HumanMode(t *testing.T) {
	var buf bytes.Buffer
	SetConsoleOverride(&buf)
	t.Cleanup(ClearConsoleOverride)

	bad := &CorgiCompose{
		DatabaseServices: []DatabaseService{{ServiceName: "db", Driver: "postgres", Port: 8080}},
		Services:         []Service{{ServiceName: "api", Port: 8080, Start: []string{"x"}}},
	}
	if AbortOnValidationErrors(bad) {
		t.Fatal("a compose with errors must abort (false)")
	}
	if !strings.Contains(buf.String(), "validation error") {
		t.Fatalf("human output missing the error banner: %q", buf.String())
	}
}

func TestAbortOnValidationErrors_JSONMode(t *testing.T) {
	prevJSON := JSONOutput
	JSONOutput = true
	t.Cleanup(func() { JSONOutput = prevJSON })

	// JSONError writes to os.Stdout — swap a pipe in so the test output stays clean.
	prevStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = prevStdout })

	bad := &CorgiCompose{
		DatabaseServices: []DatabaseService{{ServiceName: "db", Driver: "postgres", Port: 8080}},
		Services:         []Service{{ServiceName: "api", Port: 8080, Start: []string{"x"}}},
	}
	got := AbortOnValidationErrors(bad)
	_ = w.Close()
	// The payload is tiny and fits the pipe buffer, so reading after the write
	// side closed won't deadlock.
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	if got {
		t.Fatal("JSON mode with errors must abort (false)")
	}
	if !strings.Contains(sb.String(), ErrPortConflict) {
		t.Fatalf("JSON output missing the error code: %q", sb.String())
	}
}

func TestCheckDuplicateNames_DuplicateKeys(t *testing.T) {
	prev := DuplicateComposeKeys
	DuplicateComposeKeys = []string{"services.api"}
	t.Cleanup(func() { DuplicateComposeKeys = prev })

	issues := checkDuplicateNames(&CorgiCompose{})
	if countCode(issues, ErrDuplicateName) != 1 {
		t.Fatalf("a duplicate decode key must surface E_DUPLICATE_NAME, got %v", codesOf(issues))
	}
}

// --- memory.go: error and edge paths ---

func TestMemoryHelperFallbacks(t *testing.T) {
	if typeForDir("unknown-dir") != "" {
		t.Error("typeForDir of an unknown dir should be empty")
	}
	if typeRank("unknown-type") != len(typeDirs) {
		t.Error("typeRank of an unknown type should be last")
	}
	if pluralType("weird") != "weird" {
		t.Error("pluralType of an unknown type should echo it back")
	}
}

func TestNormalizeLinksBareAndEmpty(t *testing.T) {
	got := normalizeLinks([]string{"[[wrapped]]", "plain", "   "})
	if len(got) != 2 || got[0] != "wrapped" || got[1] != "plain" {
		t.Fatalf("normalizeLinks = %v, want [wrapped plain]", got)
	}
}

func TestParseFactNoFrontmatterAndBadYAML(t *testing.T) {
	// No frontmatter → empty fact, no error (lint handles it later).
	f, err := parseFact([]byte("just a body\n"), "x.md")
	if err != nil || f.Name != "" {
		t.Fatalf("no-frontmatter parse = (%+v, %v), want empty/nil", f, err)
	}
	// Malformed frontmatter YAML → a real parse error.
	if _, err := parseFact([]byte("---\nname: [unterminated\n---\nbody\n"), "x.md"); err == nil {
		t.Fatal("malformed frontmatter must return a parse error")
	}
}

func TestReadFactsTypeFallbackAndError(t *testing.T) {
	// type omitted → inferred from the folder.
	root := filepath.Join(t.TempDir(), "memory")
	writeFact(t, root, "decisions", "notype", "---\nname: notype\ndescription: d\n---\n")
	facts, err := ReadFacts(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 || facts[0].Type != "decision" {
		t.Fatalf("type should fall back to the folder, got %+v", facts)
	}

	// A malformed fact makes ReadFacts (and LintFacts) error out.
	bad := filepath.Join(t.TempDir(), "memory")
	writeFact(t, bad, "decisions", "broken", "---\nname: [bad\n---\nx\n")
	if _, err := ReadFacts(bad); err == nil {
		t.Fatal("a malformed fact should make ReadFacts error")
	}
	if errs, _ := LintFacts(bad); !hasCode(errs, ErrMemoryNoFront) {
		t.Fatalf("LintFacts should report the read error, got %+v", errs)
	}
}

func TestAddFactRejectsEmptyName(t *testing.T) {
	if _, err := AddFact(t.TempDir(), Fact{Type: "fix"}); err == nil {
		t.Fatal("an empty fact name must be rejected")
	}
}

func TestLintFlagsMissingDescriptionAndBadName(t *testing.T) {
	root := filepath.Join(t.TempDir(), "memory")
	// Missing description.
	writeFact(t, root, "decisions", "nodesc", "---\nname: nodesc\ntype: decision\n---\n")
	// Bad (non-kebab) name.
	writeFact(t, root, "decisions", "Bad_Name", "---\nname: Bad_Name\ndescription: d\ntype: decision\n---\n")
	errs, _ := LintFacts(root)
	if !hasCode(errs, ErrMemoryNoFront) {
		t.Errorf("missing description not flagged: %+v", errs)
	}
	if !hasCode(errs, ErrMemoryBadName) {
		t.Errorf("non-kebab name not flagged: %+v", errs)
	}
}

func TestRenderIndexIncludesService(t *testing.T) {
	idx := RenderIndex([]Fact{{Name: "n", Description: "d", Type: "fix", Service: "billing"}})
	if !strings.Contains(idx, "(billing)") {
		t.Fatalf("index should annotate the service, got:\n%s", idx)
	}
}

// --- autopilotstate.go: write failure when the parent isn't a directory ---

func TestWriteAutopilotStateMkdirFails(t *testing.T) {
	fileAsParent := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(fileAsParent, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// dir would be <file>/corgi_services — MkdirAll can't create under a file.
	path := filepath.Join(fileAsParent, "corgi_services", ".autopilot.json")
	if err := WriteAutopilotState(path, AutopilotState{Mode: AutopilotRunning}); err == nil {
		t.Fatal("WriteAutopilotState should fail when the parent dir can't be created")
	}
}

// --- output.go: ConsoleOut honors the override ---

func TestConsoleOutUsesOverride(t *testing.T) {
	var buf bytes.Buffer
	SetConsoleOverride(&buf)
	t.Cleanup(ClearConsoleOverride)
	if _, err := ConsoleOut().Write([]byte("hi")); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "hi" {
		t.Fatalf("ConsoleOut did not route to the override: %q", buf.String())
	}
}

// --- config.go: small direct-call edges ---

func TestDetectDuplicateComposeKeysEdges(t *testing.T) {
	// A top-level sequence (not a mapping) yields no duplicates.
	if dups := detectDuplicateComposeKeys([]byte("- a\n- b\n")); len(dups) != 0 {
		t.Errorf("sequence doc should have no dups, got %v", dups)
	}
	// A section whose value is a scalar (not a map) is skipped.
	if dups := detectDuplicateComposeKeys([]byte("services: hello\n")); len(dups) != 0 {
		t.Errorf("scalar section should be skipped, got %v", dups)
	}
	// Invalid YAML parses to nothing → no dups, no panic.
	if dups := detectDuplicateComposeKeys([]byte("a: b: c\n")); len(dups) != 0 {
		t.Errorf("invalid yaml should yield no dups, got %v", dups)
	}
	// A genuine duplicate under a tracked section is reported.
	dups := detectDuplicateComposeKeys([]byte("services:\n  api: {}\n  api: {}\n"))
	if len(dups) != 1 || dups[0] != "services.api" {
		t.Errorf("expected services.api dup, got %v", dups)
	}
}

func TestUnknownFieldsFromYAMLErrorNil(t *testing.T) {
	if fields := unknownFieldsFromYAMLError(nil); fields != nil {
		t.Errorf("nil error should yield nil fields, got %v", fields)
	}
}

func TestServiceRepoDir(t *testing.T) {
	if got, want := ServiceRepoDir("./api"), computeAbsolutePath("./api"); got != want {
		t.Errorf("ServiceRepoDir = %q, want %q", got, want)
	}
}

// --- suggesthistory.go: parse errors, version default, nil guards ---

func TestLoadSuggestHistory_MalformedJSON(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "corgi_services"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(SuggestHistoryPath(root), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSuggestHistory(root); err == nil {
		t.Fatal("malformed history JSON should error")
	}
}

func TestLoadSuggestHistory_ReadErrorIsNotMissing(t *testing.T) {
	root := t.TempDir()
	// Make the history path a directory so ReadFile fails with a non-NotExist error.
	if err := os.MkdirAll(SuggestHistoryPath(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSuggestHistory(root); err == nil {
		t.Fatal("a non-missing read error should propagate")
	}
}

func TestLoadSuggestHistory_DefaultsVersion(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "corgi_services"), 0o755); err != nil {
		t.Fatal(err)
	}
	// No "version" key → defaults to 1.
	if err := os.WriteFile(SuggestHistoryPath(root), []byte(`{"entries":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := LoadSuggestHistory(root)
	if err != nil {
		t.Fatal(err)
	}
	if h.Version != 1 {
		t.Errorf("Version = %d, want defaulted 1", h.Version)
	}
}

func TestSuggestHistoryNilGuards(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	if skip, _ := ShouldSkip(nil, "x", now, 0); skip {
		t.Error("ShouldSkip(nil) must not skip")
	}
	if RateLimited(nil, now, 1) {
		t.Error("RateLimited(nil) must be false")
	}
}
