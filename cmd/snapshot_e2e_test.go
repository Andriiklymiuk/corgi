//go:build e2e

package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils"
)

type e2eDB struct {
	driver      string
	service     string
	composeBody string
}

func scaffoldDBService(t *testing.T, root string, db e2eDB) string {
	t.Helper()
	dir := filepath.Join(root, "corgi_services", "db_services", db.service)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(db.composeBody), 0o644); err != nil {
		t.Fatalf("write compose: %v", err)
	}
	return dir
}

func composeIn(dir string, args ...string) ([]byte, error) {
	c := exec.Command("docker", append([]string{"compose"}, args...)...)
	c.Dir = dir
	return c.CombinedOutput()
}

func composeUp(t *testing.T, dir string) {
	t.Helper()
	if out, err := composeIn(dir, "up", "-d"); err != nil {
		t.Fatalf("compose up: %s: %v", strings.TrimSpace(string(out)), err)
	}
}

func composeDownVolumes(t *testing.T, dir string) {
	t.Helper()
	if out, err := composeIn(dir, "down", "--volumes"); err != nil {
		t.Logf("compose down cleanup (non-fatal): %s: %v", strings.TrimSpace(string(out)), err)
	}
}

func waitReady(t *testing.T, container, user, db string) {
	t.Helper()
	deadline := time.Now().Add(180 * time.Second)
	const needStreak = 6
	stable := 0
	for time.Now().Before(deadline) {
		if err := exec.Command("docker", "exec", "-i", container,
			"psql", "-tA", "-v", "ON_ERROR_STOP=1", "-U", user, "-d", db,
			"-c", "SELECT 1;").Run(); err == nil {
			stable++
			if stable >= needStreak {
				return
			}
		} else {
			stable = 0
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("container %s never became ready", container)
}

func psql(t *testing.T, container, user, db, sql string) {
	t.Helper()
	out, err := exec.Command("docker", "exec", "-i", container,
		"psql", "-v", "ON_ERROR_STOP=1", "-U", user, "-d", db, "-c", sql).CombinedOutput()
	if err != nil {
		t.Fatalf("psql %q: %s: %v", sql, strings.TrimSpace(string(out)), err)
	}
}

func psqlScalar(t *testing.T, container, user, db, sql string) string {
	t.Helper()
	out, err := exec.Command("docker", "exec", "-i", container,
		"psql", "-tA", "-v", "ON_ERROR_STOP=1", "-U", user, "-d", db, "-c", sql).CombinedOutput()
	if err != nil {
		t.Fatalf("psql scalar %q: %s: %v", sql, strings.TrimSpace(string(out)), err)
	}
	return strings.TrimSpace(string(out))
}

func TestSnapshotRestoreE2E(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	const (
		user = "corgi"
		pass = "corgi"
		dbN  = "corgi"
	)

	dbs := []e2eDB{
		{
			driver:  "postgres",
			service: "corgisnapselftestpg",
			composeBody: fmt.Sprintf(`services:
  postgres-corgisnapselftestpg:
    image: postgres:17-alpine
    container_name: postgres-corgisnapselftestpg
    environment:
      - POSTGRES_USER=%s
      - POSTGRES_PASSWORD=%s
      - POSTGRES_DB=%s
    ports:
      - "55432:5432"
    networks:
      - corgi-network
networks:
  corgi-network:
    driver: bridge
`, user, pass, dbN),
		},
		{
			driver:  "postgis",
			service: "corgisnapselftestpgis",
			composeBody: fmt.Sprintf(`services:
  postgis-corgisnapselftestpgis:
    image: postgis/postgis:17-3.5
    platform: linux/amd64
    container_name: postgis-corgisnapselftestpgis
    environment:
      - POSTGRES_USER=%s
      - POSTGRES_PASSWORD=%s
      - POSTGRES_DB=%s
    ports:
      - "55433:5432"
    volumes:
      - postgis-corgisnapselftestpgis-data:/var/lib/postgresql/data
    networks:
      - corgi-network
volumes:
  postgis-corgisnapselftestpgis-data:
networks:
  corgi-network:
    driver: bridge
`, user, pass, dbN),
		},
	}

	for _, db := range dbs {
		db := db
		t.Run(db.driver, func(t *testing.T) {
			root := t.TempDir()
			prev := utils.CorgiComposePathDir
			utils.CorgiComposePathDir = root
			t.Cleanup(func() { utils.CorgiComposePathDir = prev })

			dir := scaffoldDBService(t, root, db)
			container := utils.ContainerName(db.driver, db.service)

			composeDownVolumes(t, dir)
			t.Cleanup(func() { composeDownVolumes(t, dir) })

			composeUp(t, dir)
			waitReady(t, container, user, dbN)

			psql(t, container, user, dbN, "CREATE TABLE widget (id int PRIMARY KEY, label text);")
			psql(t, container, user, dbN, "INSERT INTO widget (id, label) VALUES (1, 'before-snapshot');")
			psql(t, container, user, dbN, "CREATE MATERIALIZED VIEW widget_mv AS SELECT id, label FROM widget;")
			psql(t, container, user, dbN, "REFRESH MATERIALIZED VIEW widget_mv;")

			if got := psqlScalar(t, container, user, dbN, "SELECT label FROM widget WHERE id = 1;"); got != "before-snapshot" {
				t.Fatalf("pre-snapshot row = %q, want before-snapshot", got)
			}
			if got := psqlScalar(t, container, user, dbN, "SELECT label FROM widget_mv WHERE id = 1;"); got != "before-snapshot" {
				t.Fatalf("pre-snapshot matview = %q, want before-snapshot", got)
			}

			psql(t, container, user, dbN, "CHECKPOINT;")

			const snapName = "e2e-build1"
			if _, err := utils.RunSnapshot(utils.SnapshotRequest{
				Service: db.service, Driver: db.driver,
				Stack: filepath.Base(root), Name: snapName, WasRunning: true,
			}, time.Now()); err != nil {
				t.Fatalf("RunSnapshot: %v", err)
			}

			items, err := utils.ListSnapshots(db.service)
			if err != nil {
				t.Fatalf("ListSnapshots: %v", err)
			}
			found := false
			for _, it := range items {
				if it.Name == snapName {
					found = true
				}
			}
			if !found {
				t.Fatalf("ListSnapshots %v does not include %q", items, snapName)
			}

			waitReady(t, container, user, dbN)
			psql(t, container, user, dbN, "UPDATE widget SET label = 'after-snapshot' WHERE id = 1;")

			archive, metaPath, err := utils.SnapshotPaths(db.service, snapName)
			if err != nil {
				t.Fatalf("SnapshotPaths: %v", err)
			}
			if err := utils.RunRestore(utils.RestoreRequest{
				Service: db.service, Driver: db.driver,
				ArchivePath: archive, MetaPath: metaPath,
			}); err != nil {
				t.Fatalf("RunRestore: %v", err)
			}

			waitReady(t, container, user, dbN)

			if got := psqlScalar(t, container, user, dbN, "SELECT label FROM widget WHERE id = 1;"); got != "before-snapshot" {
				t.Fatalf("restored row = %q, want before-snapshot", got)
			}
			if got := psqlScalar(t, container, user, dbN, "SELECT label FROM widget_mv WHERE id = 1;"); got != "before-snapshot" {
				t.Fatalf("restored matview = %q, want before-snapshot (no refresh)", got)
			}
		})
	}
}
