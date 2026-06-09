package utils

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The snapshot/restore pipeline shells out to `docker`. The real exercise lives
// in snapshot_e2e_test.go behind the `e2e` tag and needs a daemon, so it never
// runs in CI coverage. These tests stub `docker` with a tiny shell script on
// PATH instead — every docker call returns canned output (tunable per test via
// FAKE_* env vars), so RunSnapshot, RunRestore and all their helpers run for
// real with no daemon.
//
// Knobs (set after installFakeDocker):
//
//	FAKE_FAIL          space-separated: exec|version|stop|cp|cpimport — those calls exit 1
//	FAKE_FAIL_COMPOSE  space-separated compose subcommands to fail (down|up|start)
//	FAKE_INSPECT_CODE  container exit code reported by `docker inspect` (default 0)
//	FAKE_ARCH          arch reported by `docker version` (default arm64)
//	FAKE_IMAGE         image reported by `docker compose config` (default postgres:17-alpine)
const fakeDockerScript = `#!/bin/sh
fail_has() { for x in $FAKE_FAIL; do [ "$x" = "$1" ] && return 0; done; return 1; }
case "$1" in
  exec)    fail_has exec && { echo "no PG_VERSION" >&2; exit 1; }; echo "17" ;;
  version) fail_has version && exit 1; echo "${FAKE_ARCH:-arm64}" ;;
  stop)    fail_has stop && { echo "stop failed" >&2; exit 1; }; exit 0 ;;
  inspect) echo "${FAKE_INSPECT_CODE:-0}" ;;
  compose)
    shift
    if [ "$1" = "config" ]; then [ -n "$FAKE_NO_IMAGE" ] || echo "${FAKE_IMAGE:-postgres:17-alpine}"; exit 0; fi
    for x in $FAKE_FAIL_COMPOSE; do [ "$x" = "$1" ] && { echo "compose $1 failed" >&2; exit 1; }; done
    exit 0 ;;
  cp)
    if [ "$2" = "-" ]; then
      cat >/dev/null; fail_has cpimport && { echo "inject failed" >&2; exit 1; }; exit 0
    else
      fail_has cp && { echo "export failed" >&2; exit 1; }; cat "$FAKE_TAR"
    fi ;;
  *) exit 0 ;;
esac
`

// installFakeDocker writes the stub, prepends it to PATH, and points FAKE_TAR at
// a real (uncompressed) tar the export branch streams out.
func installFakeDocker(t *testing.T) {
	t.Helper()
	binDir := t.TempDir()
	script := filepath.Join(binDir, "docker")
	if err := os.WriteFile(script, []byte(fakeDockerScript), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}

	tarPath := filepath.Join(binDir, "payload.tar")
	writePlainTar(t, tarPath)

	t.Setenv("FAKE_TAR", tarPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func writePlainTar(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	tw := tar.NewWriter(f)
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
}

func scaffoldDbServiceDir(t *testing.T, service string) {
	t.Helper()
	dir := filepath.Join(CorgiComposePathDir, "corgi_services", "db_services", service)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func useTempStack(t *testing.T) {
	t.Helper()
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })
	scaffoldDbServiceDir(t, "main")
}

// craftSnapshot writes a valid zstd(tar) archive plus a meta sidecar for "main",
// applying meta defaults (matching the fake docker) before any caller overrides.
func craftSnapshot(t *testing.T, name string, meta SnapshotMeta) (archive, metaPath string) {
	t.Helper()
	archive, metaPath, err := SnapshotPaths("main", name)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(archive), 0o755); err != nil {
		t.Fatal(err)
	}
	writeZstdTar(t, archive)
	if meta.Image == "" {
		meta.Image = "postgres:17-alpine"
	}
	if meta.Arch == "" {
		meta.Arch = "arm64"
	}
	// Record the archive's real SHA by default so the now-always-on integrity
	// check passes for callers that craft a good snapshot.
	if meta.SHA256 == "" {
		meta.SHA256 = fileSHA(t, archive)
	}
	if err := WriteSnapshotMeta(metaPath, meta); err != nil {
		t.Fatal(err)
	}
	return archive, metaPath
}

func fileSHA(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestRunSnapshotAndRestoreFakeDocker(t *testing.T) {
	useTempStack(t)
	installFakeDocker(t)

	const name = "build1"
	meta, err := RunSnapshot(SnapshotRequest{
		Service: "main", Driver: "postgres", Stack: "demo",
		Name: name, WasRunning: true,
	}, time.Now())
	if err != nil {
		t.Fatalf("RunSnapshot: %v", err)
	}
	if meta.Image != "postgres:17-alpine" || meta.Arch != "arm64" || meta.PgVersionMajor != "17" {
		t.Errorf("meta = %+v, want image/arch/pg postgres:17-alpine/arm64/17", meta)
	}
	if meta.SizeBytes <= 0 || meta.SHA256 == "" {
		t.Errorf("expected non-empty size/sha, got %d / %q", meta.SizeBytes, meta.SHA256)
	}

	archive, metaPath, err := SnapshotPaths("main", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(archive); err != nil {
		t.Errorf("archive not written: %v", err)
	}
	if _, err := os.Stat(metaPath); err != nil {
		t.Errorf("meta not written: %v", err)
	}

	// the just-written snapshot restores clean (image/arch match the fake, the
	// archive is valid zstd(tar), and inject + start succeed).
	if err := RunRestore(RestoreRequest{
		Service: "main", Driver: "postgres",
		ArchivePath: archive, MetaPath: metaPath,
	}); err != nil {
		t.Fatalf("RunRestore: %v", err)
	}
}

func TestRunSnapshotAlreadyExists(t *testing.T) {
	useTempStack(t)
	installFakeDocker(t)

	archive, _, err := SnapshotPaths("main", "dup")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(archive), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := RunSnapshot(SnapshotRequest{
		Service: "main", Driver: "postgres", Name: "dup",
	}, time.Now()); err == nil {
		t.Error("an existing snapshot without --force should be refused")
	}

	// --force overwrites without complaint
	if _, err := RunSnapshot(SnapshotRequest{
		Service: "main", Driver: "postgres", Name: "dup", Force: true,
	}, time.Now()); err != nil {
		t.Errorf("--force should overwrite, got %v", err)
	}
}

func TestRunSnapshotErrorPaths(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
	}{
		{"pgVersion", map[string]string{"FAKE_FAIL": "exec"}},
		{"arch", map[string]string{"FAKE_FAIL": "version"}},
		{"stopFails", map[string]string{"FAKE_FAIL": "stop"}},
		{"stopUnclean", map[string]string{"FAKE_INSPECT_CODE": "137"}},
		{"writeArchive", map[string]string{"FAKE_FAIL": "cp"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			useTempStack(t)
			installFakeDocker(t)
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			if _, err := RunSnapshot(SnapshotRequest{
				Service: "main", Driver: "postgres", Name: "x", WasRunning: true,
			}, time.Now()); err == nil {
				t.Errorf("%s: expected RunSnapshot to fail", c.name)
			}
		})
	}
}

func TestRunRestoreMissingArchive(t *testing.T) {
	useTempStack(t)
	if err := RunRestore(RestoreRequest{
		Service: "main", Driver: "postgres",
		ArchivePath: filepath.Join(t.TempDir(), "nope.tar.zst"),
		MetaPath:    filepath.Join(t.TempDir(), "nope.meta.json"),
	}); err == nil {
		t.Error("restore from a missing archive should error before touching docker")
	}
}

func TestRunRestoreMissingMeta(t *testing.T) {
	useTempStack(t)
	archive, metaPath := craftSnapshot(t, "nometa", SnapshotMeta{})
	if err := os.Remove(metaPath); err != nil {
		t.Fatal(err)
	}
	if err := RunRestore(RestoreRequest{
		Service: "main", Driver: "postgres", ArchivePath: archive, MetaPath: metaPath,
	}); err == nil {
		t.Error("a missing meta sidecar should fail the restore")
	}
}

func TestRunRestoreArchMismatch(t *testing.T) {
	useTempStack(t)
	installFakeDocker(t) // fake reports arm64
	archive, metaPath := craftSnapshot(t, "x86", SnapshotMeta{Arch: "amd64"})

	// without --force the arch mismatch aborts
	if err := RunRestore(RestoreRequest{
		Service: "main", Driver: "postgres", ArchivePath: archive, MetaPath: metaPath,
	}); err == nil {
		t.Error("arch mismatch without --force should abort")
	}
	// with --force the warning is logged and the restore proceeds to completion
	if err := RunRestore(RestoreRequest{
		Service: "main", Driver: "postgres", ArchivePath: archive, MetaPath: metaPath, Force: true,
	}); err != nil {
		t.Errorf("--force should override the mismatch, got %v", err)
	}
}

func TestRunRestoreFromPathVerifiesSHA(t *testing.T) {
	useTempStack(t)
	installFakeDocker(t)

	archive, metaPath := craftSnapshot(t, "trusted", SnapshotMeta{})
	// record the real sha → the FromPath hash check passes and the restore runs
	good := SnapshotMeta{SHA256: fileSHA(t, archive)}
	if err := WriteSnapshotMeta(metaPath, mergeDefaults(good)); err != nil {
		t.Fatal(err)
	}
	if err := RunRestore(RestoreRequest{
		Service: "main", Driver: "postgres", ArchivePath: archive, MetaPath: metaPath, FromPath: true,
	}); err != nil {
		t.Errorf("matching sha from an explicit path should restore, got %v", err)
	}

	// a wrong recorded sha trips the integrity check before any wipe
	if err := WriteSnapshotMeta(metaPath, mergeDefaults(SnapshotMeta{SHA256: "deadbeef"})); err != nil {
		t.Fatal(err)
	}
	if err := RunRestore(RestoreRequest{
		Service: "main", Driver: "postgres", ArchivePath: archive, MetaPath: metaPath, FromPath: true,
	}); err == nil {
		t.Error("a sha mismatch on an explicit path must abort")
	}
}

func TestRunRestoreNamedVerifiesSHA(t *testing.T) {
	useTempStack(t)
	installFakeDocker(t)
	archive, metaPath := craftSnapshot(t, "named", SnapshotMeta{})
	// corrupt the recorded checksum on a *named* (not --from-path) restore
	if err := WriteSnapshotMeta(metaPath, mergeDefaults(SnapshotMeta{SHA256: "deadbeef"})); err != nil {
		t.Fatal(err)
	}
	if err := RunRestore(RestoreRequest{
		Service: "main", Driver: "postgres", ArchivePath: archive, MetaPath: metaPath,
	}); err == nil {
		t.Error("a sha mismatch on a named snapshot must abort before the wipe")
	}
}

func TestRunRestorePgVersionGate(t *testing.T) {
	useTempStack(t)
	installFakeDocker(t) // fake reports PG_VERSION 17
	archive, metaPath := craftSnapshot(t, "pg15", SnapshotMeta{PgVersionMajor: "15"})

	if err := RunRestore(RestoreRequest{
		Service: "main", Driver: "postgres", ArchivePath: archive, MetaPath: metaPath,
	}); err == nil {
		t.Error("pg-major mismatch without --force should abort")
	}
	if err := RunRestore(RestoreRequest{
		Service: "main", Driver: "postgres", ArchivePath: archive, MetaPath: metaPath, Force: true,
	}); err != nil {
		t.Errorf("--force should override the pg-major mismatch, got %v", err)
	}
}

func mergeDefaults(m SnapshotMeta) SnapshotMeta {
	if m.Image == "" {
		m.Image = "postgres:17-alpine"
	}
	if m.Arch == "" {
		m.Arch = "arm64"
	}
	return m
}

func TestRunRestoreCorruptArchive(t *testing.T) {
	useTempStack(t)
	installFakeDocker(t)
	archive, metaPath := craftSnapshot(t, "bad", SnapshotMeta{})
	// overwrite the valid archive with non-zstd bytes → probeArchive fails
	if err := os.WriteFile(archive, []byte("not a zstd stream"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunRestore(RestoreRequest{
		Service: "main", Driver: "postgres", ArchivePath: archive, MetaPath: metaPath,
	}); err == nil {
		t.Error("a corrupt archive should fail probing before any wipe")
	}
}

func TestRunRestoreDockerFailures(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
	}{
		{"down", map[string]string{"FAKE_FAIL_COMPOSE": "down"}},
		{"up", map[string]string{"FAKE_FAIL_COMPOSE": "up"}},
		{"start", map[string]string{"FAKE_FAIL_COMPOSE": "start"}},
		{"inject", map[string]string{"FAKE_FAIL": "cpimport"}},
		{"arch", map[string]string{"FAKE_FAIL": "version"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			useTempStack(t)
			installFakeDocker(t)
			archive, metaPath := craftSnapshot(t, "snap", SnapshotMeta{})
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			if err := RunRestore(RestoreRequest{
				Service: "main", Driver: "postgres", ArchivePath: archive, MetaPath: metaPath,
			}); err == nil {
				t.Errorf("%s failure should propagate", c.name)
			}
		})
	}
}

func TestComposeImageEmpty(t *testing.T) {
	useTempStack(t)
	installFakeDocker(t)
	t.Setenv("FAKE_NO_IMAGE", "1") // compose config prints nothing
	if _, err := composeImage(filepath.Join(CorgiComposePathDir, "corgi_services", "db_services", "main")); err == nil {
		t.Error("composeImage should error when no image is resolved")
	}
}

func TestWriteSnapshotMetaBadPath(t *testing.T) {
	// a path whose parent directory does not exist surfaces the write error
	err := WriteSnapshotMeta(filepath.Join(t.TempDir(), "missing", "x.meta.json"), SnapshotMeta{})
	if err == nil {
		t.Error("writing into a nonexistent directory should fail")
	}
}

func TestWriteSnapshotArchiveCreateFails(t *testing.T) {
	useTempStack(t)
	installFakeDocker(t)
	// target path sits under a nonexistent directory → os.Create fails after the
	// docker cp has started, exercising the kill-and-cleanup branch.
	bad := filepath.Join(t.TempDir(), "no-such-dir", "out.tar.zst")
	if _, _, err := writeSnapshotArchive("postgres-main", bad); err == nil {
		t.Error("writeSnapshotArchive should fail when the archive can't be created")
	}
}

func TestTrapInterruptDeregister(t *testing.T) {
	called := false
	stop := trapInterrupt(func(os.Signal) { called = true })
	stop()
	stop() // idempotent — second call must not panic on a closed channel
	if called {
		t.Error("handler must not run on the normal (no-signal) path")
	}
}

func TestCountingWriter(t *testing.T) {
	var c countingWriter
	n, err := c.Write([]byte("hello"))
	if err != nil || n != 5 {
		t.Fatalf("Write = %d,%v want 5,nil", n, err)
	}
	if _, err := c.Write([]byte("!")); err != nil {
		t.Fatal(err)
	}
	if c.n != 6 {
		t.Errorf("counter = %d, want 6", c.n)
	}
}
