package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils"
)

func TestResolvePostgresService(t *testing.T) {
	dbs := []utils.DatabaseService{
		{ServiceName: "main", Driver: "postgis"},
		{ServiceName: "cache", Driver: "redis"},
	}
	svc, err := resolvePostgresService("", dbs)
	if err != nil || svc.ServiceName != "main" {
		t.Fatalf("sole postgres should resolve to main, got %v / %v", svc, err)
	}
	if _, err := resolvePostgresService("cache", dbs); err == nil {
		t.Error("redis should be refused (not postgres-family)")
	}
	if _, err := resolvePostgresService("nope", dbs); err == nil {
		t.Error("unknown service should error")
	}
}

func TestResolvePostgresServiceAmbiguous(t *testing.T) {
	dbs := []utils.DatabaseService{
		{ServiceName: "a", Driver: "postgres"},
		{ServiceName: "b", Driver: "postgis"},
	}
	if _, err := resolvePostgresService("", dbs); err == nil {
		t.Error("two postgres-family dbs with no arg should require a service name")
	}
}

func TestResolvePostgresServiceRmNamedServiceMultiDB(t *testing.T) {
	dbs := []utils.DatabaseService{
		{ServiceName: "main", Driver: "postgres"},
		{ServiceName: "build1", Driver: "postgis"},
	}
	svc, err := resolvePostgresService("main", dbs)
	if err != nil || svc.ServiceName != "main" {
		t.Fatalf("named service in multi-db stack should resolve to main, got %v / %v", svc, err)
	}
}

func TestResolveRestoreSource(t *testing.T) {
	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	// named snapshot → paths under the service snapshots dir, fromPath=false
	arc, meta, fromPath, err := resolveRestoreSource("main", "build1")
	if err != nil {
		t.Fatalf("named: %v", err)
	}
	if fromPath {
		t.Error("a bare name must not be treated as an explicit path")
	}
	wantArc, wantMeta, _ := utils.SnapshotPaths("main", "build1")
	if arc != wantArc || meta != wantMeta {
		t.Errorf("named paths = %q,%q want %q,%q", arc, meta, wantArc, wantMeta)
	}

	// absolute path → fromPath=true, meta derived by trimming .tar.zst
	arc, meta, fromPath, err = resolveRestoreSource("main", "/abs/x.tar.zst")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if !fromPath || arc != "/abs/x.tar.zst" || meta != "/abs/x.meta.json" {
		t.Errorf("abs path = %q,%q,%v", arc, meta, fromPath)
	}

	// relative path with a separator → fromPath=true
	if _, _, fromPath, _ := resolveRestoreSource("main", "rel/x.tar.zst"); !fromPath {
		t.Error("a value with a separator must be treated as a path")
	}

	// suffix only, no separator → still a path, meta sidecar derived
	arc, meta, fromPath, err = resolveRestoreSource("main", "x.tar.zst")
	if err != nil {
		t.Fatalf("suffix: %v", err)
	}
	if !fromPath || arc != "x.tar.zst" || meta != "x.meta.json" {
		t.Errorf("suffix path = %q,%q,%v", arc, meta, fromPath)
	}
}

func TestListSnapshots(t *testing.T) {
	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	// no snapshots dir yet → the "no snapshots" branch (text) and an empty JSON array
	listSnapshots("main")
	prevJSON := utils.JSONOutput
	utils.JSONOutput = true
	t.Cleanup(func() { utils.JSONOutput = prevJSON })
	listSnapshots("main")

	// one valid pair → both the JSON and the text listing branches
	arc, meta, err := utils.SnapshotPaths("main", "build1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(arc), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(arc, []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := utils.WriteSnapshotMeta(meta, utils.SnapshotMeta{
		Service: "main", PgVersionMajor: "17", Arch: "arm64", SizeBytes: 1, CreatedAt: "2026-06-07T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	listSnapshots("main") // JSON
	utils.JSONOutput = false
	listSnapshots("main") // text
}

func TestSnapshotRemovePaths(t *testing.T) {
	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	// a path-escaping name is rejected before any path is returned
	if _, _, err := snapshotRemovePaths("main", "../evil"); err == nil {
		t.Error("a name with a path separator must be rejected")
	}

	// a good name resolves to the pair and removeSnapshot deletes both files
	arc, meta, err := snapshotRemovePaths("main", "good")
	if err != nil {
		t.Fatalf("good name: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(arc), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(arc, []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(meta, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(arc, "good.tar.zst") || !strings.HasSuffix(meta, "good.meta.json") {
		t.Errorf("unexpected paths %q / %q", arc, meta)
	}

	removeSnapshot("main", "good")
	if _, err := os.Stat(arc); !os.IsNotExist(err) {
		t.Errorf("archive should be removed, stat err = %v", err)
	}
	if _, err := os.Stat(meta); !os.IsNotExist(err) {
		t.Errorf("meta should be removed, stat err = %v", err)
	}
}
