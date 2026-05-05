package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetEnvFromFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	body := "a=1\n# 🐶 Auto generated vars by corgi\nb=2\n"
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	got := getEnvFromFile(p, "# 🐶 Auto generated vars by corgi")
	if !strings.Contains(got, "a=1") || !strings.Contains(got, "b=2") {
		t.Errorf("got %q", got)
	}
	if strings.Contains(got, "Auto generated") {
		t.Errorf("should strip generated marker: %q", got)
	}
}

func TestAppendEnvironmentLinesEmpty(t *testing.T) {
	got, err := appendEnvironmentLines("base\n", Service{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "base\n" {
		t.Errorf("got %q", got)
	}
}

func TestAppendEnvironmentLinesSubstitutesOwn(t *testing.T) {
	got, err := appendEnvironmentLines("HOST=localhost\n", Service{
		Environment: []string{"URL=http://${HOST}:3000"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "URL=http://localhost:3000") {
		t.Errorf("got %q", got)
	}
}

func TestAppendEnvironmentLinesCrossRefError(t *testing.T) {
	prev := currentExportsMap
	currentExportsMap = ExportsMap{}
	t.Cleanup(func() { currentExportsMap = prev })

	_, err := appendEnvironmentLines("", Service{
		ServiceName: "consumer",
		Environment: []string{"X=${producer.URL}"},
	})
	if err == nil {
		t.Error("expected err")
	}
}

func TestBuildServiceEnvBodyWithDb(t *testing.T) {
	corgi := &CorgiCompose{
		DatabaseServices: []DatabaseService{
			{ServiceName: "main", Driver: "postgres", Host: "h", Port: 5432, User: "u", Password: "p", DatabaseName: "d"},
		},
	}
	s := Service{
		ServiceName: "api",
		DependsOnDb: []DependsOnDb{{Name: "main"}},
		Port:        3000,
	}
	got, err := buildServiceEnvBody(s, corgi, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "PORT=3000") {
		t.Errorf("got %q", got)
	}
}

func TestBuildServiceEnvBodyIgnoreDeps(t *testing.T) {
	got, err := buildServiceEnvBody(Service{ServiceName: "api", Port: 3000}, &CorgiCompose{}, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "PORT") {
		t.Errorf("expected no port when ignoring deps: %q", got)
	}
}

func TestBuildServiceEnvBodyPortAlias(t *testing.T) {
	got, err := buildServiceEnvBody(Service{ServiceName: "api", Port: 3000, PortAlias: "API_PORT"}, &CorgiCompose{}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "API_PORT=3000") {
		t.Errorf("got %q", got)
	}
}

func TestRecordExportsForServiceNoMap(t *testing.T) {
	prev := currentExportsMap
	currentExportsMap = nil
	t.Cleanup(func() { currentExportsMap = prev })

	if err := recordExportsForService(Service{Exports: []string{"X"}}, "X=1"); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRecordExportsForServiceAlready(t *testing.T) {
	prev := currentExportsMap
	currentExportsMap = ExportsMap{"x": {"A": "1"}}
	t.Cleanup(func() { currentExportsMap = prev })

	if err := recordExportsForService(Service{ServiceName: "x", Exports: []string{"X"}}, "X=2"); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestRecordExportsForServiceNew(t *testing.T) {
	prev := currentExportsMap
	currentExportsMap = ExportsMap{}
	t.Cleanup(func() { currentExportsMap = prev })

	if err := recordExportsForService(Service{ServiceName: "y", Exports: []string{"X"}}, "X=2"); err != nil {
		t.Errorf("err: %v", err)
	}
	if currentExportsMap["y"]["X"] != "2" {
		t.Errorf("got %v", currentExportsMap)
	}
}

func TestRecordExportsForServiceMissingVar(t *testing.T) {
	prev := currentExportsMap
	currentExportsMap = ExportsMap{}
	t.Cleanup(func() { currentExportsMap = prev })

	err := recordExportsForService(Service{ServiceName: "y", Exports: []string{"NOPE"}}, "")
	if err == nil {
		t.Error("expected err")
	}
}

func TestRenderEnvFileContent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	if err := os.WriteFile(p, []byte("FOO=bar\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := renderEnvFileContent(p, "ADDED=1", Service{})
	if !strings.Contains(got, "ADDED=1") || !strings.Contains(got, "FOO=bar") {
		t.Errorf("got %q", got)
	}
}

func TestRenderEnvFileContentReplaceLocalhost(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	if err := os.WriteFile(p, []byte("URL=http://localhost\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := renderEnvFileContent(p, "", Service{LocalhostNameInEnv: "host.docker.internal"})
	if strings.Contains(got, "localhost") {
		t.Errorf("localhost not replaced: %q", got)
	}
}

func TestWriteEnvFileEmpty(t *testing.T) {
	if err := writeEnvFile("/whatever", ""); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestWriteEnvFileWrites(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	if err := writeEnvFile(p, "FOO=bar"); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(p)
	if string(body) != "FOO=bar" {
		t.Errorf("got %q", body)
	}
}

func TestWriteEnvFileBadPath(t *testing.T) {
	if err := writeEnvFile("/no/such/dir/zzz.env", "x"); err == nil {
		t.Error("expected err")
	}
}

func TestGenerateEnvForServiceIgnoreEnv(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateEnvForService(&CorgiCompose{}, Service{
		ServiceName:  "x",
		IgnoreEnv:    true,
		AbsolutePath: dir,
	}, "", false); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestGenerateEnvForServiceWritesFile(t *testing.T) {
	dir := t.TempDir()
	prev := CorgiComposePathDir
	CorgiComposePathDir = dir
	t.Cleanup(func() { CorgiComposePathDir = prev })

	if err := GenerateEnvForService(&CorgiCompose{}, Service{
		ServiceName:  "x",
		AbsolutePath: dir,
		Port:         3000,
	}, "", false); err != nil {
		t.Errorf("err: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "PORT=3000") {
		t.Errorf("got %q", body)
	}
}

func TestGenerateEnvForServicesNoServices(t *testing.T) {
	if err := GenerateEnvForServices(&CorgiCompose{}); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestGenerateEnvForServicesSimple(t *testing.T) {
	dir := t.TempDir()
	prev := CorgiComposePathDir
	CorgiComposePathDir = dir
	t.Cleanup(func() { CorgiComposePathDir = prev })

	c := &CorgiCompose{
		Services: []Service{
			{ServiceName: "a", AbsolutePath: dir, Port: 3000},
		},
	}
	if err := GenerateEnvForServices(c); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestGenerateEnvForServicesCodependentFallback(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	prev := CorgiComposePathDir
	CorgiComposePathDir = dir1
	t.Cleanup(func() { CorgiComposePathDir = prev })

	// A and B reference each other but only via different VARs (not a true cycle)
	c := &CorgiCompose{
		Services: []Service{
			{
				ServiceName:  "a",
				AbsolutePath: dir1,
				Port:         3001,
				Exports:      []string{"A_URL=http://localhost:3001"},
				DependsOnServices: []DependsOnService{{Name: "b"}},
				Environment: []string{"X=${b.B_URL}"},
			},
			{
				ServiceName:  "b",
				AbsolutePath: dir2,
				Port:         3002,
				Exports:      []string{"B_URL=http://localhost:3002"},
				DependsOnServices: []DependsOnService{{Name: "a"}},
				Environment: []string{"Y=${a.A_URL}"},
			},
		},
	}
	if err := GenerateEnvForServices(c); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestCreateFileForPath(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })

	CreateFileForPath("envs/.env")
	if _, err := os.Stat(filepath.Join(CorgiComposePathDir, "envs/.env")); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestCreateFileForPathEmpty(t *testing.T) {
	CreateFileForPath("")
}
