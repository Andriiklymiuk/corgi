package utils

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

func TestIsPostgresFamilyDriver(t *testing.T) {
	yes := []string{"postgres", "postgis", "pgvector", "timescaledb"}
	no := []string{"mysql", "mongodb", "cockroach", "yugabytedb", "supabase", "redis", ""}
	for _, d := range yes {
		if !IsPostgresFamilyDriver(d) {
			t.Errorf("expected %q to be postgres-family", d)
		}
	}
	for _, d := range no {
		if IsPostgresFamilyDriver(d) {
			t.Errorf("expected %q NOT to be postgres-family", d)
		}
	}
}

func TestContainerName(t *testing.T) {
	if got := ContainerName("postgis", "main"); got != "postgis-main" {
		t.Errorf("ContainerName = %q, want postgis-main", got)
	}
}

func TestSnapshotPaths(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })

	arc, meta, err := SnapshotPaths("main", "build1")
	if err != nil {
		t.Fatalf("SnapshotPaths err: %v", err)
	}
	wantDir := filepath.Join(CorgiComposePathDir, "corgi_services", "db_services", "main", "snapshots")
	if filepath.Dir(arc) != wantDir {
		t.Errorf("archive dir = %q, want %q", filepath.Dir(arc), wantDir)
	}
	if !strings.HasSuffix(arc, "build1.tar.zst") {
		t.Errorf("archive = %q, want suffix build1.tar.zst", arc)
	}
	if !strings.HasSuffix(meta, "build1.meta.json") {
		t.Errorf("meta = %q, want suffix build1.meta.json", meta)
	}
}

func TestDefaultSnapshotName(t *testing.T) {
	at := time.Date(2026, 6, 7, 15, 30, 0, 0, time.UTC)
	if got := DefaultSnapshotName(at); got != "2026-06-07-1530" {
		t.Errorf("DefaultSnapshotName = %q, want 2026-06-07-1530", got)
	}
}

func TestSanitizeSnapshotName(t *testing.T) {
	if _, err := SanitizeSnapshotName("../evil"); err == nil {
		t.Error("expected error for name with path separator")
	}
	if got, err := SanitizeSnapshotName("ok-name_1"); err != nil || got != "ok-name_1" {
		t.Errorf("SanitizeSnapshotName(ok) = %q,%v", got, err)
	}
}

func TestSnapshotMetaRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.meta.json")
	in := SnapshotMeta{
		Stack: "demo", Service: "main", PgVersionMajor: "16",
		Image: "postgis/postgis:16-3.4", Arch: "arm64",
		DataPath: PostgresDataDir, CreatedAt: "2026-06-07T15:30:00Z",
		SizeBytes: 123, SHA256: "deadbeef",
	}
	if err := WriteSnapshotMeta(path, in); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadSnapshotMeta(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != in {
		t.Errorf("round-trip mismatch:\n got %+v\nwant %+v", got, in)
	}
}

func TestReadSnapshotMetaMissing(t *testing.T) {
	if _, err := ReadSnapshotMeta(filepath.Join(t.TempDir(), "nope.meta.json")); err == nil {
		t.Error("expected error reading missing meta")
	}
}

func TestListSnapshotsSkipsIncompletePairs(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })

	dir, _ := SnapshotsDir("main")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "good.tar.zst"), []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteSnapshotMeta(filepath.Join(dir, "good.meta.json"), SnapshotMeta{Service: "main", PgVersionMajor: "16"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "orphan.tar.zst"), []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ListSnapshots("main")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].Name != "good" {
		t.Fatalf("expected only [good], got %+v", got)
	}
}

func TestListSnapshotsEmptyDir(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })
	got, err := ListSnapshots("main")
	if err != nil {
		t.Fatalf("list on missing dir should not error, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty, got %+v", got)
	}
}

func TestCheckRestoreCompatibility(t *testing.T) {
	m := SnapshotMeta{PgVersionMajor: "16", Arch: "arm64", Image: "postgis/postgis:16-3.4"}

	if err := CheckRestoreCompatibility(m, "postgis/postgis:16-3.4", "arm64"); err != nil {
		t.Errorf("matching should pass, got %v", err)
	}
	if err := CheckRestoreCompatibility(m, "postgres:16-alpine", "arm64"); err == nil {
		t.Error("image mismatch should fail")
	}
	if err := CheckRestoreCompatibility(m, "postgis/postgis:16-3.4", "amd64"); err == nil {
		t.Error("arch mismatch should fail")
	}
}

func TestIsStackSupervised(t *testing.T) {
	dir := t.TempDir()
	if IsStackSupervised(dir) {
		t.Error("no .state.json should mean not supervised")
	}
	// write a state file with a running container-managed service (PID 0 is the
	// container-managed convention; ReconcileRunState leaves it as-is rather than
	// pid-probing an unowned pid, so the running status survives on any platform).
	st := RunState{
		Services: []RunStateEntry{{Status: "running", PID: 0, Command: "x"}},
	}
	if err := WriteRunState(RunStatePath(dir), st); err != nil {
		t.Fatal(err)
	}
	if !IsStackSupervised(dir) {
		t.Error("a state file with a running service should be supervised")
	}
}

func TestParsePgVersionMajor(t *testing.T) {
	cases := map[string]string{"16\n": "16", "17.2\n": "17", " 15 ": "15"}
	for in, want := range cases {
		if got := parsePgVersionMajor(in); got != want {
			t.Errorf("parsePgVersionMajor(%q) = %q, want %q", in, got, want)
		}
	}
}

// writeZstdTar builds a valid zstd-wrapped tar (one file entry) at path, using
// the same libs the prod code reads with, so probeArchive runs under plain go test.
func writeZstdTar(t *testing.T, path string) {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	body := []byte("data")
	if err := tw.WriteHeader(&tar.Header{Name: "data/PG_VERSION", Mode: 0o644, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := zstd.NewWriter(f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := enc.Write(buf.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestProbeArchive(t *testing.T) {
	dir := t.TempDir()

	good := filepath.Join(dir, "good.tar.zst")
	writeZstdTar(t, good)
	if err := probeArchive(good); err != nil {
		t.Errorf("valid zstd tar should probe clean, got %v", err)
	}

	notZstd := filepath.Join(dir, "plain.tar.zst")
	if err := os.WriteFile(notZstd, []byte("not zstd at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := probeArchive(notZstd); err == nil {
		t.Error("non-zstd file should fail to probe")
	}

	// valid zstd header wrapping garbage that is not a tar
	corrupt := filepath.Join(dir, "corrupt.tar.zst")
	cf, err := os.Create(corrupt)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := zstd.NewWriter(cf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := enc.Write([]byte("this is zstd-compressed but not a tar stream")); err != nil {
		t.Fatal(err)
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cf.Close(); err != nil {
		t.Fatal(err)
	}
	if err := probeArchive(corrupt); err == nil {
		t.Error("zstd wrapping a non-tar payload should fail to probe")
	}

	if err := probeArchive(filepath.Join(dir, "missing.tar.zst")); err == nil {
		t.Error("missing path should return the open error")
	}
}

func TestVerifyArchiveSHA(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.tar.zst")
	content := []byte("the quick brown fox")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	want := hex.EncodeToString(sum[:])

	if err := verifyArchiveSHA(path, want); err != nil {
		t.Errorf("matching checksum should pass, got %v", err)
	}
	if err := verifyArchiveSHA(path, "deadbeef"); err == nil {
		t.Error("wrong checksum should fail")
	}
	if err := verifyArchiveSHA(filepath.Join(dir, "nope.tar.zst"), want); err == nil {
		t.Error("missing file should return the open error")
	}
}

func TestCleanSnapshots(t *testing.T) {
	t.Chdir(t.TempDir())

	root := filepath.Join("corgi_services", "db_services")
	for _, svc := range []string{"main", "other"} {
		snapDir := filepath.Join(root, svc, "snapshots")
		if err := os.MkdirAll(snapDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(snapDir, "build1.tar.zst"), []byte("z"), 0o644); err != nil {
			t.Fatal(err)
		}
		// a sibling file under the service dir that must survive
		if err := os.WriteFile(filepath.Join(root, svc, "docker-compose.yml"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	CleanSnapshots()

	for _, svc := range []string{"main", "other"} {
		if _, err := os.Stat(filepath.Join(root, svc, "snapshots")); !os.IsNotExist(err) {
			t.Errorf("snapshots dir for %s should be gone, stat err = %v", svc, err)
		}
		if _, err := os.Stat(filepath.Join(root, svc, "docker-compose.yml")); err != nil {
			t.Errorf("sibling file for %s should survive, got %v", svc, err)
		}
		if _, err := os.Stat(filepath.Join(root, svc)); err != nil {
			t.Errorf("service dir %s should survive, got %v", svc, err)
		}
	}
}

func TestCleanSnapshotsMissingRoot(t *testing.T) {
	t.Chdir(t.TempDir())
	// no corgi_services/db_services → CleanSnapshots returns without error
	CleanSnapshots()
}
