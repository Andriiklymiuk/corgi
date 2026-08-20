package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// envCheckFixture builds a compose dir with one service repo holding an
// example file, returning the compose and a cleanup-managed chdir.
func envCheckFixture(t *testing.T, example, source string, svc Service) *CorgiCompose {
	t.Helper()
	dir := t.TempDir()
	old := CorgiComposePathDir
	CorgiComposePathDir = dir
	t.Cleanup(func() { CorgiComposePathDir = old })

	repo := filepath.Join(dir, svc.ServiceName)
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	svc.AbsolutePath = repo
	if example != "" {
		if err := os.WriteFile(filepath.Join(repo, ".env.example"), []byte(example), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if source != "" {
		if err := os.WriteFile(filepath.Join(dir, svc.CopyEnvFromFilePath), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &CorgiCompose{Services: []Service{svc}}
}

func TestEnvCheck_MissingKeyFound(t *testing.T) {
	corgi := envCheckFixture(t,
		"PORT=3000\nSECRET_KEY=change-me\nOTHER=1\n",
		"OTHER=1\n",
		Service{ServiceName: "api", Port: 3000, CopyEnvFromFilePath: "api.env"},
	)
	rows, err := EnvCheckAll(corgi, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	// PORT is corgi-generated, OTHER is provided; only SECRET_KEY is a finding.
	if len(rows[0].Missing) != 1 || rows[0].Missing[0] != "SECRET_KEY" {
		t.Fatalf("missing = %v, want [SECRET_KEY]", rows[0].Missing)
	}
	if rows[0].OK() {
		t.Fatal("row with missing keys must not be OK")
	}
}

func TestEnvCheck_GeneratedDbKeysExcluded(t *testing.T) {
	corgi := envCheckFixture(t,
		"", // example written below, after the db keys are known
		"",
		Service{ServiceName: "api", Port: 3000, CopyEnvFromFilePath: "api.env",
			DependsOnDb: []DependsOnDb{{Name: "pg"}}},
	)
	corgi.DatabaseServices = []DatabaseService{
		{ServiceName: "pg", Driver: "postgres", Port: 5432, User: "u", Password: "p", DatabaseName: "d"},
	}
	// The example declares exactly what corgi generates for the db dep.
	all, err := ResolveAllEnv(corgi)
	if err != nil {
		t.Fatal(err)
	}
	var example strings.Builder
	for _, e := range all["api"] {
		example.WriteString(e.Key + "=x\n")
	}
	repo := corgi.Services[0].AbsolutePath
	if err := os.WriteFile(filepath.Join(repo, ".env.example"), []byte(example.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(CorgiComposePathDir, "api.env"), []byte("UNRELATED=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rows, err := EnvCheckAll(corgi, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows[0].Missing) != 0 {
		t.Fatalf("generated keys reported missing: %v", rows[0].Missing)
	}
}

func TestEnvCheck_SourceAbsent(t *testing.T) {
	corgi := envCheckFixture(t,
		"KEY=1\n",
		"", // declared env file never written
		Service{ServiceName: "api", CopyEnvFromFilePath: "env/source/api.env"},
	)
	rows, err := EnvCheckAll(corgi, "")
	if err != nil {
		t.Fatal(err)
	}
	if !rows[0].SourceAbsent {
		t.Fatalf("want SourceAbsent, got %+v", rows[0])
	}
}

func TestEnvCheck_NoCopyEnvSkips(t *testing.T) {
	corgi := envCheckFixture(t,
		"KEY=1\n",
		"",
		Service{ServiceName: "api"},
	)
	rows, err := EnvCheckAll(corgi, "")
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Skipped == "" || rows[0].SourceAbsent {
		t.Fatalf("service without copyEnvFromFilePath should be skipped, got %+v", rows[0])
	}
}

func TestEnvCheck_NoExampleSkips(t *testing.T) {
	corgi := envCheckFixture(t,
		"",
		"KEY=1\n",
		Service{ServiceName: "api", CopyEnvFromFilePath: "api.env"},
	)
	rows, err := EnvCheckAll(corgi, "")
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Skipped == "" {
		t.Fatalf("service without an example file should be skipped, got %+v", rows[0])
	}
}

func TestEnvCheck_FileOverride(t *testing.T) {
	corgi := envCheckFixture(t,
		"KEY=1\nCI_ONLY=1\n",
		"",
		Service{ServiceName: "api", CopyEnvFromFilePath: "api.env"},
	)
	repo := corgi.Services[0].AbsolutePath
	if err := os.WriteFile(filepath.Join(repo, ".env.ci"), []byte("KEY=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := EnvCheckAll(corgi, ".env.ci")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows[0].Missing) != 1 || rows[0].Missing[0] != "CI_ONLY" {
		t.Fatalf("missing = %v, want [CI_ONLY]", rows[0].Missing)
	}

	// And the override being absent is a finding, not a skip.
	rows, err = EnvCheckAll(corgi, ".env.staging")
	if err != nil {
		t.Fatal(err)
	}
	if !rows[0].SourceAbsent {
		t.Fatalf("absent override file should be SourceAbsent, got %+v", rows[0])
	}
}

func TestEnvCheckSummary_RefusesVacuousPass(t *testing.T) {
	summary, findings := EnvCheckSummary([]EnvCheckRow{
		{Service: "api", Skipped: "no example"},
	})
	if !findings {
		t.Fatal("zero checked services must count as a finding")
	}
	if !strings.Contains(summary, "nothing was checked") {
		t.Fatalf("summary should say nothing was checked, got:\n%s", summary)
	}
}

func TestEnvCheckSummary_CleanPass(t *testing.T) {
	summary, findings := EnvCheckSummary([]EnvCheckRow{
		{Service: "api", Example: ".env.example", Source: "api.env"},
	})
	if findings {
		t.Fatalf("clean row reported as finding:\n%s", summary)
	}
}
