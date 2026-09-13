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

// The phone reads a session's diff the way it reads its conversation: only
// for a workspace the laptop allowed, the file list first, one file's patch
// on request, and never more than a phone can show.
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
	// The generated file's lines are not somebody's work: not in the total.
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
}
