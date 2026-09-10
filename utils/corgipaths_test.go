package utils

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCorgiServicesPrefersTheNestedFolderAndFallsBackToLegacy(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(".corgi", "corgi_services")

	if got := CorgiServicesRelIn(dir); got != nested {
		t.Errorf("empty workspace = %q, want %q", got, nested)
	}

	os.MkdirAll(filepath.Join(dir, "corgi_services"), 0o755)
	if got := CorgiServicesRelIn(dir); got != "corgi_services" {
		t.Errorf("legacy present = %q, want the legacy folder", got)
	}

	os.MkdirAll(filepath.Join(dir, nested), 0o755)
	if got := CorgiServicesRelIn(dir); got != nested {
		t.Errorf("both present = %q, want %q", got, nested)
	}

	if got := CorgiServicesRelIn(""); got != nested {
		t.Errorf("no workspace = %q, want %q", got, nested)
	}
}

func TestComposeDirOfWalksBackThroughEitherLayout(t *testing.T) {
	if got := ComposeDirOf("/ws/.corgi/corgi_services"); got != "/ws" {
		t.Errorf("nested = %q", got)
	}
	if got := ComposeDirOf("/ws/corgi_services"); got != "/ws" {
		t.Errorf("legacy = %q", got)
	}
}

func TestMigrateMovesTheFolderAndRetargetsGitignore(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "corgi_services")
	os.MkdirAll(filepath.Join(legacy, "db_services", "pg"), 0o755)
	os.WriteFile(filepath.Join(legacy, "db_services", "pg", ".env"), []byte("PGHOST=x"), 0o600)
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("node_modules\ncorgi_services/*\n!corgi_services/.gitignore\n"), 0o644)

	moved, err := MigrateCorgiServices(dir)
	if err != nil || !moved {
		t.Fatalf("migrate = %v, %v", moved, err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Error("the legacy folder should be gone")
	}
	env := filepath.Join(dir, ".corgi", "corgi_services", "db_services", "pg", ".env")
	if data, err := os.ReadFile(env); err != nil || string(data) != "PGHOST=x" {
		t.Errorf("content did not come along: %v", err)
	}
	ignore, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	for _, want := range []string{"node_modules", ".corgi/corgi_services/*", "!.corgi/corgi_services/.gitignore"} {
		if !strings.Contains(string(ignore), want) {
			t.Errorf("missing %q in\n%s", want, ignore)
		}
	}

	again, err := MigrateCorgiServices(dir)
	if again || err != nil {
		t.Errorf("a second run should do nothing: %v, %v", again, err)
	}
	if noWorkspace, err := MigrateCorgiServices(""); noWorkspace || err != nil {
		t.Errorf("no workspace = %v, %v", noWorkspace, err)
	}
}

func TestMigrateRefusesWhileServicesRunOrBothFoldersExist(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "corgi_services")
	os.MkdirAll(legacy, 0o755)
	pid := livePID(t)
	state, _ := json.Marshal(RunState{Services: []RunStateEntry{{Name: "api", Status: "running", PID: pid, PGID: pid}}})
	os.WriteFile(filepath.Join(legacy, ".state.json"), state, 0o600)

	if _, err := MigrateCorgiServices(dir); err == nil || !strings.Contains(err.Error(), "corgi stop") {
		t.Fatalf("a running service should stop the move, got %v", err)
	}

	stopped, _ := json.Marshal(RunState{Services: []RunStateEntry{{Name: "api", Status: "stopped"}}})
	os.WriteFile(filepath.Join(legacy, ".state.json"), stopped, 0o600)
	os.MkdirAll(filepath.Join(dir, ".corgi", "corgi_services"), 0o755)
	if _, err := MigrateCorgiServices(dir); err == nil || !strings.Contains(err.Error(), "merge them by hand") {
		t.Fatalf("two folders should not be merged silently, got %v", err)
	}
}

// A crash or a reboot leaves rows saying "running" that no longer are. They
// must not wedge the move forever.
func TestMigrateIgnoresARunningRowWhoseProcessIsGone(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "corgi_services")
	os.MkdirAll(filepath.Join(legacy, ".logs", "api"), 0o755)
	logFile := filepath.Join(legacy, ".logs", "api", "boot.log")
	os.WriteFile(logFile, []byte("up"), 0o644)

	state, _ := json.Marshal(RunState{
		ComposePath: filepath.Join(dir, "corgi-compose.yml"),
		Services: []RunStateEntry{
			{Name: "api", Status: "running", PID: deadPID(t), PGID: deadPID(t), LogFile: logFile},
			{Name: "web", Status: "running", PID: 0, Container: "no-such-container-corgi-test"},
		},
		DBServices: []RunStateEntry{{Name: "pg", Status: "running", Container: "no-such-container-corgi-test"}},
	})
	os.WriteFile(filepath.Join(legacy, ".state.json"), state, 0o600)

	moved, err := MigrateCorgiServices(dir)
	if err != nil || !moved {
		t.Fatalf("dead rows should not block the move: %v, %v", moved, err)
	}

	target := filepath.Join(dir, ".corgi", "corgi_services")
	var after RunState
	data, err := os.ReadFile(filepath.Join(target, ".state.json"))
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(data, &after)
	want := filepath.Join(target, ".logs", "api", "boot.log")
	if after.Services[0].LogFile != want {
		t.Errorf("logFile = %q, want %q", after.Services[0].LogFile, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("the log should be where the state now points: %v", err)
	}
}

// A separator is escaped inside JSON, so rewriting the saved paths as raw
// text silently misses every Windows one. Assert on the parsed state.
func TestMigrateRewritesLogPathsThroughTheParsedState(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "corgi_services")
	os.MkdirAll(filepath.Join(legacy, ".logs", "api"), 0o755)
	logFile := filepath.Join(legacy, ".logs", "api", "boot.log")
	os.WriteFile(logFile, []byte("up"), 0o644)

	elsewhere := filepath.Join(dir, "somewhere-else.log")
	state, _ := json.Marshal(RunState{
		Services:   []RunStateEntry{{Name: "api", Status: "stopped", LogFile: logFile}},
		DBServices: []RunStateEntry{{Name: "pg", Status: "stopped", LogFile: elsewhere}},
	})
	for _, name := range []string{".state.json", ".state.last.json"} {
		os.WriteFile(filepath.Join(legacy, name), state, 0o600)
	}

	if moved, err := MigrateCorgiServices(dir); err != nil || !moved {
		t.Fatalf("migrate = %v, %v", moved, err)
	}

	target := filepath.Join(dir, ".corgi", "corgi_services")
	want := filepath.Join(target, ".logs", "api", "boot.log")
	for _, name := range []string{".state.json", ".state.last.json"} {
		after, err := ReadRunState(filepath.Join(target, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if after.Services[0].LogFile != want {
			t.Errorf("%s: logFile = %q, want %q", name, after.Services[0].LogFile, want)
		}
		if after.DBServices[0].LogFile != elsewhere {
			t.Errorf("%s: a path outside the folder must be left alone, got %q", name, after.DBServices[0].LogFile)
		}
	}
}

// deadPID is a pid nothing owns: claimed, then reaped.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Skipf("cannot spawn a process to reap: %v", err)
	}
	return cmd.Process.Pid
}

// Git records a worktree's path absolutely, in both directions. A move that
// only fixes one of them leaves the checkout prunable.
func TestMigrateRepairsMovedWorktrees(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "api")
	os.MkdirAll(repo, 0o755)
	git := func(dir string, args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Skipf("git %v: %s: %v", args, out, err)
		}
	}
	git(repo, "init", "-q", ".")
	git(repo, "config", "user.email", "t@t")
	git(repo, "config", "user.name", "t")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("hi"), 0o644)
	git(repo, "add", "-A")
	git(repo, "commit", "-qm", "init")

	worktree := filepath.Join(root, "corgi_services", ".worktrees", "api-feature-x")
	os.MkdirAll(filepath.Dir(worktree), 0o755)
	git(repo, "worktree", "add", "-q", worktree, "-b", "feature/x")

	moved, err := MigrateCorgiServices(root)
	if err != nil || !moved {
		t.Fatalf("migrate = %v, %v", moved, err)
	}

	listed := exec.Command("git", "worktree", "list")
	listed.Dir = repo
	out, err := listed.Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "prunable") {
		t.Fatalf("the moved worktree is still prunable — the next prune would drop it:\n%s", out)
	}
	want := filepath.Join(root, ".corgi", "corgi_services", ".worktrees", "api-feature-x")
	if !strings.Contains(string(out), want) {
		t.Errorf("the repo should point at the new path %q:\n%s", want, out)
	}
}
