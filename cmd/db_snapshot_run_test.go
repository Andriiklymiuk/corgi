package cmd

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"andriiklymiuk/corgi/utils"
)

const fakeDockerCmdScript = `#!/bin/sh
case "$1" in
  exec)    echo "17" ;;
  version) echo "arm64" ;;
  stop)    exit 0 ;;
  inspect) echo "0" ;;
  ps)      exit 0 ;;
  compose)
    shift
    if [ "$1" = "config" ]; then echo "postgres:17-alpine"; fi
    exit 0 ;;
  cp)
    if [ "$2" = "-" ]; then cat >/dev/null; exit 0; else cat "$FAKE_TAR"; fi ;;
  *) exit 0 ;;
esac
`

func fakeTarBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	body := []byte("17\n")
	if err := tw.WriteHeader(&tar.Header{Name: "data/PG_VERSION", Mode: 0o644, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func installFakeDockerCmd(t *testing.T) {
	t.Helper()
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "docker"), []byte(fakeDockerCmdScript), 0o755); err != nil {
		t.Fatal(err)
	}
	tarPath := filepath.Join(binDir, "payload.tar")
	if err := os.WriteFile(tarPath, fakeTarBytes(t), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_TAR", tarPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// chdirStack writes a corgi-compose.yml with one postgres db, chdir's into it,
// and scaffolds the db service dir so composeImage's `docker compose` can run.
func chdirStack(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "corgi-compose.yml"),
		[]byte("name: test\ndb_services:\n  main:\n    driver: postgres\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(cwd) })

	svcDir := filepath.Join(dir, "corgi_services", "db_services", "main")
	if err := os.MkdirAll(svcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(svcDir, "docker-compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// resetSnapshotFlags saves the package-level command flags and restores them,
// defaulting to the create path with the restore prompt suppressed.
func resetSnapshotFlags(t *testing.T) {
	t.Helper()
	pl, prm, pf, ry, rf := snapList, snapRM, snapForce, restoreYes, restoreForce
	t.Cleanup(func() { snapList, snapRM, snapForce, restoreYes, restoreForce = pl, prm, pf, ry, rf })
	snapList, snapRM, snapForce, restoreYes, restoreForce = false, "", false, true, false
}

// Drives the full create → list → restore → rm path through the cobra entry
// points with a fake docker. None of these hit an os.Exit branch.
func TestRunDbSnapshotAndRestore(t *testing.T) {
	chdirStack(t)
	installFakeDockerCmd(t)
	resetSnapshotFlags(t)
	_, c := newTestComposeCommand()

	runDbSnapshot(c, []string{"snap1"})

	arc, _, err := utils.SnapshotPaths("main", "snap1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(arc); err != nil {
		t.Fatalf("snapshot archive not created: %v", err)
	}

	// --list path
	snapList = true
	runDbSnapshot(c, nil)
	snapList = false

	// restore the named snapshot (restoreYes suppresses the prompt)
	runDbRestore(c, []string{"snap1"})

	// --rm path removes the pair
	snapRM = "snap1"
	runDbSnapshot(c, nil)
	snapRM = ""
	if _, err := os.Stat(arc); !os.IsNotExist(err) {
		t.Errorf("snapshot should be removed, stat err = %v", err)
	}
}

func TestRunDbSnapshotDefaultName(t *testing.T) {
	chdirStack(t)
	installFakeDockerCmd(t)
	resetSnapshotFlags(t)
	_, c := newTestComposeCommand()

	// no name arg → a timestamp default name is generated and the snapshot saved
	runDbSnapshot(c, nil)

	items, err := utils.ListSnapshots("main")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one auto-named snapshot, got %d", len(items))
	}
}

// feedStdin replaces os.Stdin with a pipe carrying s for the duration of the test.
func feedStdin(t *testing.T, s string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old; r.Close() })
	if _, err := w.WriteString(s); err != nil {
		t.Fatal(err)
	}
	w.Close()
}

func createSnap(t *testing.T, name string) {
	t.Helper()
	resetSnapshotFlags(t)
	_, c := newTestComposeCommand()
	runDbSnapshot(c, []string{name})
}

func TestRunDbRestorePromptAbort(t *testing.T) {
	chdirStack(t)
	installFakeDockerCmd(t)
	createSnap(t, "snap1")

	restoreYes = false
	feedStdin(t, "n\n") // decline → aborts before any docker work
	_, c := newTestComposeCommand()
	runDbRestore(c, []string{"snap1"})

	if _, err := os.Stat(snapArchive(t, "snap1")); err != nil {
		t.Errorf("declining must leave the snapshot intact: %v", err)
	}
}

func TestRunDbRestorePromptAccept(t *testing.T) {
	chdirStack(t)
	installFakeDockerCmd(t)
	createSnap(t, "snap1")

	restoreYes = false
	feedStdin(t, "y\n") // accept → proceeds through RunRestore
	_, c := newTestComposeCommand()
	runDbRestore(c, []string{"snap1"})
}

func TestRunDbRestoreFromPath(t *testing.T) {
	chdirStack(t)
	installFakeDockerCmd(t)
	createSnap(t, "snap1")

	// passing the archive path (not a bare name) takes the FromPath branch, which
	// also verifies the recorded sha256 — written correctly by runDbSnapshot.
	_, c := newTestComposeCommand()
	runDbRestore(c, []string{snapArchive(t, "snap1")})
}

func snapArchive(t *testing.T, name string) string {
	t.Helper()
	arc, _, err := utils.SnapshotPaths("main", name)
	if err != nil {
		t.Fatal(err)
	}
	return arc
}

// resolution must succeed end-to-end; this also proves the os.Exit branches in
// runDbSnapshot/runDbRestore (bad config) won't fire in the run tests below.
func TestDbSnapshotResolvesViaCobra(t *testing.T) {
	chdirStack(t)
	_, c := newTestComposeCommand()
	corgi, err := utils.GetCorgiServices(c)
	if err != nil {
		t.Fatalf("GetCorgiServices: %v", err)
	}
	svc, err := resolvePostgresService("", corgi.DatabaseServices)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if svc.ServiceName != "main" || svc.Driver != "postgres" {
		t.Fatalf("resolved %+v, want main/postgres", svc)
	}
}
