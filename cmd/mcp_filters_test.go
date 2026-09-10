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

const mcpFilterStamp = "2006-01-02T15:04:05.000Z"

// writeStampedLog drops a captured-run log for service under the cwd's
// corgi_services so mcpLogs can find it; each entry is (age, content).
func writeStampedLog(t *testing.T, dir, service string, entries [][2]string) {
	t.Helper()
	logDir := filepath.Join(dir, "corgi_services", ".logs", service)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, e := range entries {
		age, _ := time.ParseDuration(e[0])
		b.WriteString(time.Now().Add(-age).UTC().Format(mcpFilterStamp) + " " + e[1] + "\n")
	}
	if err := os.WriteFile(filepath.Join(logDir, "run.log"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMCPLogsFilters(t *testing.T) {
	dir := chdirToTempCompose(t, mcpComposeFixture)
	writeStampedLog(t, dir, "api", [][2]string{
		{"2h", "boot: listening on 3000"},
		{"2h", "ERROR: db connection refused"},
		{"1m", "GET /health 200"},
		{"30s", "panic: nil deref in handler(x)"},
		{"10s", "GET /users 200"},
	})

	all, err := mcpLogs(logsArgs{Service: "api"})
	if err != nil {
		t.Fatalf("mcpLogs: %v", err)
	}
	if len(all.Lines) != 5 || all.Truncated || strings.HasPrefix(all.Lines[0], "20") {
		t.Fatalf("unfiltered: want 5 stripped lines, got %+v", all)
	}

	errs, err := mcpLogs(logsArgs{Service: "api", ErrorsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(errs.Lines) != 2 || !strings.HasPrefix(errs.Lines[0], "ERROR") || !strings.HasPrefix(errs.Lines[1], "panic") {
		t.Errorf("errorsOnly: got %v", errs.Lines)
	}

	grep, err := mcpLogs(logsArgs{Service: "api", Grep: "GET /\\w+ 200"})
	if err != nil {
		t.Fatal(err)
	}
	if len(grep.Lines) != 2 {
		t.Errorf("grep regexp: got %v", grep.Lines)
	}

	literal, err := mcpLogs(logsArgs{Service: "api", Grep: "handler(x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(literal.Lines) != 1 {
		t.Errorf("grep literal fallback: got %v", literal.Lines)
	}

	since, err := mcpLogs(logsArgs{Service: "api", Since: "10m"})
	if err != nil {
		t.Fatal(err)
	}
	if len(since.Lines) != 3 {
		t.Errorf("since 10m: got %v", since.Lines)
	}

	tail, err := mcpLogs(logsArgs{Service: "api", Grep: "GET", Lines: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(tail.Lines) != 1 || !tail.Truncated || tail.Lines[0] != "GET /users 200" {
		t.Errorf("filter before tail: got %+v", tail)
	}

	if _, err := mcpLogs(logsArgs{Service: "api", Since: "yesterday"}); err == nil || !strings.Contains(err.Error(), utils.ErrUsage) {
		t.Errorf("bad since must be E_USAGE, got %v", err)
	}
}

const mcpComposeStatusFixture = `name: mcp-status
db_services:
  pg:
    driver: postgres
    port: 48731
    user: u
    password: p
    databaseName: d
services:
  api:
    port: 48732
    start:
      - go run .
  worker:
    start:
      - go run .
`

func TestMCPStatusFilters(t *testing.T) {
	chdirToTempCompose(t, mcpComposeStatusFixture)
	mcpCache.reset()

	all, err := mcpStatus(statusArgs{})
	if err != nil {
		t.Fatalf("mcpStatus: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("want 2 probed targets, got %+v", all)
	}

	one, err := mcpStatus(statusArgs{Service: "api"})
	if err != nil {
		t.Fatal(err)
	}
	if len(one) != 1 || one[0].Label != "services.api" {
		t.Errorf("service filter: got %+v", one)
	}

	db, err := mcpStatus(statusArgs{Service: "pg"})
	if err != nil {
		t.Fatal(err)
	}
	if len(db) != 1 || db[0].Port != 48731 {
		t.Errorf("db_service filter: got %+v", db)
	}

	// Nothing listens on those ports, so unhealthyOnly is everything; a
	// declared service without a port is a legitimate empty answer.
	down, err := mcpStatus(statusArgs{UnhealthyOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(down) != 2 {
		t.Errorf("unhealthyOnly: got %+v", down)
	}
	portless, err := mcpStatus(statusArgs{Service: "worker"})
	if err != nil || len(portless) != 0 {
		t.Errorf("portless service: got %+v err %v", portless, err)
	}

	if _, err := mcpStatus(statusArgs{Service: "ghost"}); err == nil || !strings.Contains(err.Error(), utils.ErrServiceNotFound) {
		t.Errorf("unknown service must be E_SERVICE_NOT_FOUND, got %v", err)
	}

	// The sweep above was cached; the filtered calls must not have replaced it.
	if _, ok := mcpCache.cachedStatus(utils.CorgiComposePath); !ok {
		t.Error("status sweep not cached")
	}
}

func TestStatusEntryName(t *testing.T) {
	for label, want := range map[string]string{
		"db_services.pg (postgres)": "pg",
		"services.api":              "api",
		"services.my-svc (x)":       "my-svc",
	} {
		if got := statusEntryName(label); got != want {
			t.Errorf("%q: got %q want %q", label, got, want)
		}
	}
}

func TestMCPEnvFilters(t *testing.T) {
	chdirToTempCompose(t, mcpComposeFixture)

	full, err := mcpEnv(envArgs{})
	if err != nil {
		t.Fatalf("mcpEnv: %v", err)
	}
	var someKey string
	for k := range full["api"] {
		someKey = k
		break
	}
	if someKey == "" {
		t.Fatal("fixture resolves no env for api")
	}

	one, err := mcpEnv(envArgs{Service: "api"})
	if err != nil {
		t.Fatal(err)
	}
	if len(one) != 1 || len(one["api"]) != len(full["api"]) {
		t.Errorf("service filter: got %+v", one)
	}

	key, err := mcpEnv(envArgs{Key: someKey})
	if err != nil {
		t.Fatal(err)
	}
	if len(key["api"]) != 1 || key["api"][someKey] != full["api"][someKey] {
		t.Errorf("key filter: got %+v", key)
	}
	none, err := mcpEnv(envArgs{Key: "NO_SUCH_VAR_HERE"})
	if err != nil || len(none) != 0 {
		t.Errorf("key nobody has: got %+v err %v", none, err)
	}

	if _, err := mcpEnv(envArgs{Service: "ghost"}); err == nil || !strings.Contains(err.Error(), utils.ErrServiceNotFound) {
		t.Errorf("unknown service must be E_SERVICE_NOT_FOUND, got %v", err)
	}
}

func TestCapEnvDoc(t *testing.T) {
	big := map[string]envEntry{}
	for i := 0; i < 45; i++ {
		big[fmt.Sprintf("VAR_%02d", i)] = envEntry{Value: "v", Source: "s"}
	}
	doc := map[string]map[string]envEntry{
		"big":   big,
		"small": {"ONLY": {Value: "1", Source: "s"}},
	}
	capEnvDoc(doc, 40)
	if len(doc["big"]) != 41 {
		t.Fatalf("big: want 40 vars + marker, got %d", len(doc["big"]))
	}
	marker, ok := doc["big"][envTruncatedKey]
	if !ok || !strings.Contains(marker.Value, "5 more vars hidden") || marker.Source != envTruncatedLabel {
		t.Errorf("marker: %+v", marker)
	}
	if len(doc["small"]) != 1 {
		t.Errorf("small service must be untouched: %+v", doc["small"])
	}
}

const mcpComposeChangedFixture = `name: mcp-changed
services:
  api:
    path: ./repo
    port: 3000
    start:
      - go run .
    scripts:
      - name: test
        commands:
          - "true"
`

func TestMCPTestChanged(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := chdirToTempCompose(t, mcpComposeChangedFixture)
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "init", "-q")
	gitIn(t, repo, "checkout", "-q", "-B", "main")
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "-q", "-m", "init")

	clean, err := mcpTest(testArgs{Changed: true})
	if err != nil {
		t.Fatalf("mcpTest: %v", err)
	}
	if !clean.Passed || len(clean.Services) != 0 || !strings.Contains(clean.Note, "nothing to test") {
		t.Errorf("unchanged repo: got %+v", clean)
	}

	if err := os.WriteFile(filepath.Join(repo, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirty, err := mcpTest(testArgs{Changed: true, Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if !dirty.Passed || len(dirty.Services) != 1 || !dirty.Services[0].Passed || dirty.Note != "" {
		t.Errorf("dirty repo: got %+v", dirty)
	}
}

const mcpComposeE2EFixture = `name: mcp-e2e
services:
  api:
    port: 3000
    start:
      - go run .
e2e:
  workdir: .
  install: echo installing
  run: |
    echo hello from e2e
    exit %d
`

func TestMCPTestE2E(t *testing.T) {
	chdirToTempCompose(t, strings.Replace(mcpComposeE2EFixture, "%d", "0", 1))
	pass, err := mcpTest(testArgs{E2E: true})
	if err != nil {
		t.Fatalf("mcpTest e2e: %v", err)
	}
	if !pass.Passed || len(pass.Services) != 1 || pass.Services[0].Name != "e2e" {
		t.Fatalf("passing suite: got %+v", pass)
	}
	msg := pass.Services[0].Message
	if !strings.Contains(msg, "installing") || !strings.Contains(msg, "hello from e2e") {
		t.Errorf("captured output missing: %q", msg)
	}

	chdirToTempCompose(t, strings.Replace(mcpComposeE2EFixture, "%d", "3", 1))
	fail, err := mcpTest(testArgs{E2E: true})
	if err != nil {
		t.Fatal(err)
	}
	if fail.Passed || fail.Services[0].ExitCode != 3 {
		t.Errorf("failing suite: got %+v", fail)
	}

	chdirToTempCompose(t, mcpComposeFixture)
	if _, err := mcpTest(testArgs{E2E: true}); err == nil || !strings.Contains(err.Error(), utils.ErrConfig) {
		t.Errorf("no e2e block must be E_CONFIG, got %v", err)
	}
}

func TestMCPDBSnapshotAndRestore(t *testing.T) {
	chdirStack(t)
	installFakeDockerCmd(t)
	mcpCache.reset()

	snap, err := mcpDBSnapshot(dbSnapshotArgs{Name: "snap1"})
	if err != nil {
		t.Fatalf("mcpDBSnapshot: %v", err)
	}
	if snap.Service != "main" || snap.Name != "snap1" {
		t.Errorf("snapshot result: %+v", snap)
	}
	if _, err := os.Stat(snap.Archive); err != nil {
		t.Fatalf("archive not written: %v", err)
	}

	restored, err := mcpDBRestore(dbRestoreArgs{Name: "snap1"})
	if err != nil {
		t.Fatalf("mcpDBRestore: %v", err)
	}
	if restored.Archive != snap.Archive {
		t.Errorf("restore archive %q want %q", restored.Archive, snap.Archive)
	}

	auto, err := mcpDBSnapshot(dbSnapshotArgs{})
	if err != nil || auto.Name == "" {
		t.Errorf("default name: %+v err %v", auto, err)
	}

	if _, err := mcpDBRestore(dbRestoreArgs{}); err == nil || !strings.Contains(err.Error(), utils.ErrUsage) {
		t.Errorf("restore without name must be E_USAGE, got %v", err)
	}
	if _, err := mcpDBSnapshot(dbSnapshotArgs{Name: "../escape"}); err == nil || !strings.Contains(err.Error(), utils.ErrUsage) {
		t.Errorf("bad snapshot name must be E_USAGE, got %v", err)
	}
	if _, err := mcpDBSnapshot(dbSnapshotArgs{Service: "ghost"}); err == nil || !strings.Contains(err.Error(), utils.ErrServiceNotFound) {
		t.Errorf("unknown db must be E_SERVICE_NOT_FOUND, got %v", err)
	}
}
