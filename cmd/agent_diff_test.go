package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils/agent/sessions"
)

func TestDiffListsFilesThenOnePatch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(repo, "auth.go"), []byte("package auth\n\nfunc Refresh() {}\n"), 0o644)
	os.WriteFile(filepath.Join(repo, "go.sum"), []byte("x\n"), 0o644)
	run("add", ".")
	run("commit", "-q", "-m", "base")
	run("checkout", "-q", "-b", "corgi/APP-1")
	os.WriteFile(filepath.Join(repo, "auth.go"), []byte("package auth\n\n// a grace window\nfunc Refresh() {}\n"), 0o644)
	os.WriteFile(filepath.Join(repo, "go.sum"), []byte("x\ny\n"), 0o644)
	run("commit", "-qam", "grace")

	phoneBoard(t, true,
		sessions.Session{ID: "s1", Label: "api", Display: "api·auth", Status: sessions.StatusWorking, Cwd: repo, Branch: "corgi/APP-1"},
		sessions.Session{ID: "s2", Label: "web", Display: "web·cart", Status: sessions.StatusWorking, Cwd: repo},
	)
	orig := streamAllowedFor
	defer func() { streamAllowedFor = orig }()
	streamAllowedFor = func(ws string) bool { return ws == "api" }

	get := func(q string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		launchDiffHandler(rec, httptest.NewRequest(http.MethodGet, "/launch/diff?"+q, nil))
		return rec
	}
	if rec := get("session=s2"); rec.Code != http.StatusForbidden {
		t.Fatalf("web is not readable: %d %s", rec.Code, rec.Body)
	}
	rec := get("session=s1")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	var list struct {
		Files   []DiffFile `json:"files"`
		Added   int        `json:"added"`
		Deleted int        `json:"deleted"`
		Branch  string     `json:"branch"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list.Files) != 2 || list.Branch != "corgi/APP-1" {
		t.Fatalf("files: %+v", list)
	}
	byPath := map[string]DiffFile{}
	for _, f := range list.Files {
		byPath[f.Path] = f
	}
	if byPath["auth.go"].Added != 1 || !byPath["go.sum"].Generated {
		t.Fatalf("counts: %+v", byPath)
	}
	if list.Added != 1 || list.Deleted != 0 {
		t.Fatalf("totals: +%d -%d", list.Added, list.Deleted)
	}

	rec = get("session=s1&file=auth.go")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "+// a grace window") {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body)
	}
	if rec := get("session=s1&file=../etc/passwd"); rec.Code != http.StatusBadRequest {
		t.Fatalf("a path outside the checkout: %d", rec.Code)
	}
	rec = get("session=s1&all=1")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "+// a grace window") || strings.Contains(rec.Body.String(), "go.sum") {
		t.Fatalf("all: %d %s", rec.Code, rec.Body)
	}
}

func TestValidDiffPath(t *testing.T) {
	good := []string{"cmd/agent.go", "README.md", "a b/c.txt", "docs/über.md"}
	for _, p := range good {
		if !validDiffPath(p) {
			t.Errorf("%q should be a valid path", p)
		}
	}
	bad := []string{"", "-", "--output=/tmp/x", "/etc/passwd", "../secret", "a/../../b", "a//b", "a\nb", "x\x00y", strings.Repeat("a", 5000)}
	for _, p := range bad {
		if validDiffPath(p) {
			t.Errorf("%q should be refused", p)
		}
	}
}

func TestListedDiffPathOnlyReturnsGitsOwnString(t *testing.T) {
	files := []DiffFile{{Path: "cmd/agent.go"}, {Path: "docs/a b.md"}}
	if got := listedDiffPath(files, "cmd/agent.go"); got != "cmd/agent.go" {
		t.Errorf("listed = %q", got)
	}
	for _, p := range []string{"cmd/other.go", "--output=x", "../cmd/agent.go", ""} {
		if got := listedDiffPath(files, p); got != "" {
			t.Errorf("%q should not resolve, got %q", p, got)
		}
	}
}
