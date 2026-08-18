package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newSessionLogRoot(t *testing.T) string {
	t.Helper()
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })
	return CorgiServicesDir()
}

func readSessionLog(t *testing.T, base string) string {
	t.Helper()
	runs, err := ListServiceRuns(base, SessionLogDir)
	if err != nil {
		t.Fatalf("list session runs: %v", err)
	}
	if len(runs) == 0 {
		t.Fatal("no session log file was created")
	}
	b, err := os.ReadFile(runs[0])
	if err != nil {
		t.Fatalf("read session log: %v", err)
	}
	return string(b)
}

func TestSessionLogCapturesConsoleOutput(t *testing.T) {
	base := newSessionLogRoot(t)
	StartSessionLog()
	t.Cleanup(CloseSessionLog)

	Infof("%s\n", "✗ [E_DANGLING_DEP] service \"web\" depends on unknown service \"api\"")
	Info("port 3000 busy (services.web)")
	CloseSessionLog()

	got := readSessionLog(t, base)
	if !strings.Contains(got, "E_DANGLING_DEP") {
		t.Fatalf("validation error missing from session log:\n%s", got)
	}
	if !strings.Contains(got, "port 3000 busy") {
		t.Fatalf("port conflict missing from session log:\n%s", got)
	}
}

func TestSessionLogRedactsCredentials(t *testing.T) {
	base := newSessionLogRoot(t)
	StartSessionLog()
	t.Cleanup(CloseSessionLog)

	Infof("DATABASE_URL=postgres://corgi:hunter2pass@localhost:5432/api\n")
	Infof("POSTGRES_PASSWORD=sup3rs3cretvalue\n")
	CloseSessionLog()

	got := readSessionLog(t, base)
	for _, leaked := range []string{"hunter2pass", "sup3rs3cretvalue"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("credential %q written to disk:\n%s", leaked, got)
		}
	}
	if !strings.Contains(got, "****") {
		t.Fatalf("expected a mask in the session log:\n%s", got)
	}
}

func TestSessionLogDoesNotChangeConsoleRouting(t *testing.T) {
	base := newSessionLogRoot(t)
	var sink strings.Builder
	SetConsoleOverride(&sink)
	t.Cleanup(ClearConsoleOverride)

	StartSessionLog()
	t.Cleanup(CloseSessionLog)
	Info("hello console")
	CloseSessionLog()

	if !strings.Contains(sink.String(), "hello console") {
		t.Fatalf("console lost the line, mirror must tee not divert: %q", sink.String())
	}
	if !strings.Contains(readSessionLog(t, base), "hello console") {
		t.Fatal("session log did not receive the line")
	}
}

func TestSessionLogIsNotOfferedAsAService(t *testing.T) {
	base := newSessionLogRoot(t)
	StartSessionLog()
	CloseSessionLog()
	if _, err := OpenLogWriter(base, "api"); err != nil {
		t.Fatalf("open api log: %v", err)
	}

	services, err := ListLoggedServices(base)
	if err != nil {
		t.Fatalf("list services: %v", err)
	}
	for _, s := range services {
		if s == SessionLogDir {
			t.Fatalf("%s must not appear in the service picker: %v", SessionLogDir, services)
		}
	}

	streams, err := ListLoggedStreams(base)
	if err != nil {
		t.Fatalf("list streams: %v", err)
	}
	var found bool
	for _, s := range streams {
		if s == SessionLogDir {
			found = true
		}
	}
	if !found {
		t.Fatalf("%s must be included in --all/--dump streams: %v", SessionLogDir, streams)
	}
}

func TestNoMirrorLeavesWritersUntouched(t *testing.T) {
	ClearConsoleMirror()
	if MirrorWriter() != nil {
		t.Fatal("mirror should be unset")
	}
	if got := withMirror(os.Stdout); got != os.Stdout {
		t.Fatalf("withMirror must return the base writer unchanged when unset")
	}
}

func TestStartSessionLogIsIdempotent(t *testing.T) {
	base := newSessionLogRoot(t)
	StartSessionLog()
	StartSessionLog()
	t.Cleanup(CloseSessionLog)
	Info("one line")
	CloseSessionLog()

	runs, err := ListServiceRuns(base, SessionLogDir)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 session log, got %d: %v", len(runs), runs)
	}
}

func TestSessionLogDirIsUnderLogsDir(t *testing.T) {
	base := newSessionLogRoot(t)
	StartSessionLog()
	CloseSessionLog()
	want := filepath.Join(base, ".logs", SessionLogDir)
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected session log dir at %s: %v", want, err)
	}
	stray := filepath.Join(CorgiComposePathDir, ".logs")
	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Fatalf("session log leaked a .logs dir into the project root at %s", stray)
	}
}

func TestCloseSessionLogDropsEmptyFile(t *testing.T) {
	base := newSessionLogRoot(t)
	StartSessionLog()
	CloseSessionLog()

	runs, err := ListServiceRuns(base, SessionLogDir)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("a run that logged nothing must not leave a file: %v", runs)
	}
}

func TestWithMirrorTeesToSessionLog(t *testing.T) {
	base := newSessionLogRoot(t)
	StartSessionLog()
	t.Cleanup(CloseSessionLog)

	var terminal strings.Builder
	fmt.Fprintln(WithMirror(&terminal), "E_PORT_CONFLICT: port 3000 busy")
	CloseSessionLog()

	if !strings.Contains(terminal.String(), "E_PORT_CONFLICT") {
		t.Fatalf("terminal lost the line: %q", terminal.String())
	}
	if !strings.Contains(readSessionLog(t, base), "E_PORT_CONFLICT") {
		t.Fatal("exit-path error missing from session log")
	}
}
