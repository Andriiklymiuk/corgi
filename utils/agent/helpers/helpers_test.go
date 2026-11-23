package helpers

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEveryHelperHasItsSource(t *testing.T) {
	for _, name := range Names() {
		source, err := Source(name)
		if err != nil || len(source) == 0 {
			t.Fatalf("%s: %v", name, err)
		}
		if Describe(name) == "" {
			t.Fatalf("%s has no description", name)
		}
	}
	if _, err := Source("../../etc/passwd"); err == nil {
		t.Fatal("an unknown name read a file")
	}
}

func TestRunPassesArgumentsAndTheExitCode(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3")
	}
	code, err := Run("mine-sessions", []string{"--help"})
	if err != nil || code != 0 {
		t.Fatalf("--help: code %d, err %v", code, err)
	}
	code, err = Run("mine-sessions", []string{"--no-such-flag"})
	if err != nil || code == 0 {
		t.Fatalf("a bad flag: code %d, err %v", code, err)
	}
	if code, err := Run("nope", nil); err == nil || code != 2 || !strings.Contains(err.Error(), "mine-sessions") {
		t.Fatalf("unknown helper: code %d, err %v", code, err)
	}
}

func mineSessions(t *testing.T, args ...string) string {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3")
	}
	source, err := Source("mine-sessions")
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "mine.py")
	if err := os.WriteFile(script, source, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("python3", append([]string{"-I", script}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return string(out)
}

func writeTranscript(t *testing.T, root, name string, rows ...string) {
	t.Helper()
	dir := filepath.Join(root, "-work-acme")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	pad := fmt.Sprintf(`{"type":"pad","text":%q}`, strings.Repeat("x", 21000))
	if err := os.WriteFile(filepath.Join(dir, name+".jsonl"), []byte(strings.Join(append(rows, pad), "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestMineSessionsShowsTheExchangeAndOnlyAPersonsSessions(t *testing.T) {
	root := t.TempDir()
	writeTranscript(t, root, "aaaa1111-x",
		`{"type":"user","entrypoint":"cli","message":{"content":"ship it"}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Pushed and tagged."}]}}`,
		`{"type":"user","timestamp":"2026-11-20T09:10:00Z","message":{"content":"have you pushed??"}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"1","name":"Bash","input":{"command":"git log"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"1","is_error":true,"content":"not found"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Yes. It is on main."}]}}`,
		`{"type":"attachment","timestamp":"2026-11-20T09:12:00Z","attachment":{"type":"queued_command","commandMode":"prompt","origin":{"kind":"human"},"prompt":"stop, why a new build"}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Stopped."}]}}`)
	writeTranscript(t, root, "bbbb2222-x",
		`{"type":"user","entrypoint":"sdk-cli","message":{"content":"why is this here, do not stop"}}`)

	out := mineSessions(t, "--root", root, "--project", "/work/acme")
	for _, want := range []string{
		"read 1 sessions a person typed in", "left out 1 headless runs",
		"aaaa1111:2  2026-11-20T09:10", "before: Pushed and tagged.", "person: have you pushed??", "after:  Yes. It is on main.",
		"person: stop, why a new build", "after:  Stopped.", "1 in 1  Bash: not found",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "why is this here") {
		t.Errorf("a headless run is not a person:\n%s", out)
	}
	shown := mineSessions(t, "--root", root, "--show", "aaaa1111:2", "--around", "3")
	for _, want := range []string{"0 PERSON: ship it", "2 PERSON: have you pushed??", "3 AGENT: [Bash] git log", "4   result: not found", "5 AGENT: Yes. It is on main."} {
		if !strings.Contains(shown, want) {
			t.Errorf("missing %q in:\n%s", want, shown)
		}
	}
	if strings.Contains(shown, "Stopped.") {
		t.Errorf("only the rows around it:\n%s", shown)
	}
	if none := mineSessions(t, "--root", root, "--project", "/work/other"); !strings.Contains(none, "read 0 sessions") {
		t.Errorf("another project: %s", none)
	}
}
