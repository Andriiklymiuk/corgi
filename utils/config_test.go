package utils

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func TestServicesCanBeAdded(t *testing.T) {
	if !servicesCanBeAdded(nil) {
		t.Error("want true for nil")
	}
	if !servicesCanBeAdded([]string{"a", "b"}) {
		t.Error("want true")
	}
	if servicesCanBeAdded([]string{"a", "none", "b"}) {
		t.Error("want false when 'none' present")
	}
}

func TestIsServiceIncludedInFlag(t *testing.T) {
	if !IsServiceIncludedInFlag(nil, "x") {
		t.Error("want true when no flag")
	}
	if !IsServiceIncludedInFlag([]string{"a", "b"}, "a") {
		t.Error("want true")
	}
	if IsServiceIncludedInFlag([]string{"a", "b"}, "z") {
		t.Error("want false")
	}
}

func TestGetDbServiceByNameFound(t *testing.T) {
	dbs := []DatabaseService{
		{ServiceName: "x", Driver: "postgres"},
		{ServiceName: "y", Driver: "mysql"},
	}
	got, err := GetDbServiceByName("y", dbs)
	if err != nil {
		t.Fatal(err)
	}
	if got.Driver != "mysql" {
		t.Errorf("got %+v", got)
	}
}

func TestGetDbServiceByNameMissing(t *testing.T) {
	_, err := GetDbServiceByName("nope", nil)
	if err == nil {
		t.Error("want err")
	}
}

func TestBuildBaseCorgi(t *testing.T) {
	y := CorgiComposeYaml{
		Init:        []string{"a"},
		BeforeStart: []string{"b"},
		Start:       []string{"c"},
		AfterStart:  []string{"d"},
		UseDocker:   true,
		UseAwsVpn:   true,
		Name:        "n",
		Description: "d",
	}
	got := buildBaseCorgi(y)
	if got.Name != "n" || got.Description != "d" || !got.UseDocker || !got.UseAwsVpn {
		t.Errorf("got %+v", got)
	}
	if len(got.Init) != 1 || len(got.BeforeStart) != 1 || len(got.Start) != 1 || len(got.AfterStart) != 1 {
		t.Errorf("lifecycle wrong: %+v", got)
	}
}

func TestMergeSeedFromDbNoEnv(t *testing.T) {
	got := mergeSeedFromDb(DatabaseService{
		SeedFromDb: SeedFromDb{Host: "x"},
	})
	if got.Host != "x" {
		t.Errorf("got %+v", got)
	}
}

func TestMergeSeedFromDbFromEnvFile(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	body := "DB_HOST=foo\nDB_NAME=bar\nDB_USER=u\nDB_PASSWORD=p\nDB_PORT=1234\n"
	if err := os.WriteFile(envFile, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	got := mergeSeedFromDb(DatabaseService{SeedFromDbEnvPath: envFile})
	if got.Host != "foo" || got.DatabaseName != "bar" || got.User != "u" || got.Password != "p" || got.Port != 1234 {
		t.Errorf("got %+v", got)
	}
}

func TestMergeSeedFromDbOverridesEnvWithSpec(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	if err := os.WriteFile(envFile, []byte("DB_HOST=fromenv\nDB_PORT=10\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := mergeSeedFromDb(DatabaseService{
		SeedFromDbEnvPath: envFile,
		SeedFromDb:        SeedFromDb{Host: "override", Port: 99},
	})
	if got.Host != "override" || got.Port != 99 {
		t.Errorf("got %+v", got)
	}
}

func TestNormalizeServicePath(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"./api", "./api"},
		{"api", "./api"},
		{".", "./."},
		{"", ""},
	}
	for _, tt := range tests {
		s := Service{Path: tt.in}
		normalizeServicePath(&s)
		if s.Path != tt.want {
			t.Errorf("normalize(%q) = %q want %q", tt.in, s.Path, tt.want)
		}
	}
}

func TestComputeAbsolutePath(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = "/proj"
	t.Cleanup(func() { CorgiComposePathDir = prev })

	if got := computeAbsolutePath("./api"); got != "/proj/api" {
		t.Errorf("got %q", got)
	}
	if got := computeAbsolutePath("plain"); got != "/proj/plain" {
		t.Errorf("got %q", got)
	}
}

func TestResolveServicePathFromCloneFromGitURL(t *testing.T) {
	s := Service{CloneFrom: "git@github.com:foo/bar.git"}
	resolveServicePathFromCloneFrom(&s)
	if s.Path != "./bar" {
		t.Errorf("got %q", s.Path)
	}
}

func TestResolveServicePathFromCloneFromNoSuffix(t *testing.T) {
	s := Service{CloneFrom: "https://example.com/foo"}
	resolveServicePathFromCloneFrom(&s)
	if s.Path != "" {
		t.Errorf("non-.git should not set path, got %q", s.Path)
	}
}

func TestResolveServicePathExistingPathPreserved(t *testing.T) {
	s := Service{CloneFrom: "x.git", Path: "./already"}
	resolveServicePathFromCloneFrom(&s)
	if s.Path != "./already" {
		t.Errorf("got %q", s.Path)
	}
}

func TestParseRequiredEmpty(t *testing.T) {
	if got := parseRequired(nil, false); got != nil {
		t.Errorf("got %v", got)
	}
}

func TestParseRequiredPopulated(t *testing.T) {
	got := parseRequired(map[string]Required{
		"node": {Why: []string{"runtime"}, Install: []string{"brew install node"}, CheckCmd: "node -v"},
	}, false)
	if len(got) != 1 || got[0].Name != "node" {
		t.Errorf("got %+v", got)
	}
}

func TestParseDatabaseServicesEmpty(t *testing.T) {
	got, err := parseDatabaseServices(nil, false)
	if err != nil || got != nil {
		t.Errorf("got %v err %v", got, err)
	}
}

func TestParseDatabaseServicesAddsDefaults(t *testing.T) {
	got, err := parseDatabaseServices(map[string]DatabaseService{
		"db": {Port: 5432},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %v", got)
	}
	if got[0].Driver != "postgres" {
		t.Errorf("driver default not applied: %q", got[0].Driver)
	}
	if got[0].Host != "localhost" {
		t.Errorf("host default not applied: %q", got[0].Host)
	}
}

func TestBuildDatabaseServiceLocalstackInvalid(t *testing.T) {
	_, err := buildDatabaseService("svc", DatabaseService{
		Driver: "localstack",
		Subscriptions: []SnsSubscription{
			{Topic: "missing", Queue: "q"},
		},
	})
	if err == nil {
		t.Error("want validation err")
	}
}

func TestParseServicesEmpty(t *testing.T) {
	if got := parseServices(nil, false); got != nil {
		t.Errorf("got %v", got)
	}
}

func TestParseServicesNonePresent(t *testing.T) {
	prev := ServicesItemsFromFlag
	ServicesItemsFromFlag = []string{"none"}
	t.Cleanup(func() { ServicesItemsFromFlag = prev })
	if got := parseServices(map[string]Service{"x": {}}, false); got != nil {
		t.Errorf("got %v", got)
	}
}

func TestParseServices_PopulatesSkippedServices(t *testing.T) {
	prev := ServicesItemsFromFlag
	ServicesItemsFromFlag = []string{"api"}
	t.Cleanup(func() {
		ServicesItemsFromFlag = prev
		SkippedServices = map[string]bool{}
	})
	parseServices(map[string]Service{
		"api":          {},
		"broker":       {},
		"notification": {},
	}, false)
	if SkippedServices["api"] {
		t.Error("api should not be in SkippedServices")
	}
	if !SkippedServices["broker"] || !SkippedServices["notification"] {
		t.Errorf("expected broker+notification skipped, got %+v", SkippedServices)
	}
}

func TestParseServices_NoneMarksAllSkipped(t *testing.T) {
	prev := ServicesItemsFromFlag
	ServicesItemsFromFlag = []string{"none"}
	t.Cleanup(func() {
		ServicesItemsFromFlag = prev
		SkippedServices = map[string]bool{}
	})
	parseServices(map[string]Service{"a": {}, "b": {}}, false)
	if !SkippedServices["a"] || !SkippedServices["b"] {
		t.Errorf("expected all skipped, got %+v", SkippedServices)
	}
}

func TestParseDatabaseServices_PopulatesSkippedDbServices(t *testing.T) {
	prev := DbServicesItemsFromFlag
	DbServicesItemsFromFlag = []string{"main-db"}
	t.Cleanup(func() {
		DbServicesItemsFromFlag = prev
		SkippedDbServices = map[string]bool{}
	})
	_, err := parseDatabaseServices(map[string]DatabaseService{
		"main-db":  {Port: 5432},
		"cache-db": {Port: 5433},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if SkippedDbServices["main-db"] {
		t.Error("main-db should not be skipped")
	}
	if !SkippedDbServices["cache-db"] {
		t.Errorf("expected cache-db skipped, got %+v", SkippedDbServices)
	}
}

func TestParseDatabaseServices_NoneMarksAllSkipped(t *testing.T) {
	prev := DbServicesItemsFromFlag
	DbServicesItemsFromFlag = []string{"none"}
	t.Cleanup(func() {
		DbServicesItemsFromFlag = prev
		SkippedDbServices = map[string]bool{}
	})
	_, err := parseDatabaseServices(map[string]DatabaseService{
		"a": {}, "b": {},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !SkippedDbServices["a"] || !SkippedDbServices["b"] {
		t.Errorf("expected all skipped, got %+v", SkippedDbServices)
	}
}

func TestBuildService(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = "/proj"
	t.Cleanup(func() { CorgiComposePathDir = prev })

	got := buildService("api", Service{Path: "./api"})
	if got.ServiceName != "api" {
		t.Errorf("got %+v", got)
	}
	if got.AbsolutePath != "/proj/api" {
		t.Errorf("absolute path = %q", got.AbsolutePath)
	}
}

func TestBuildServiceCloneFromInferred(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = "/p"
	t.Cleanup(func() { CorgiComposePathDir = prev })

	got := buildService("svc", Service{CloneFrom: "git@x.com:foo/svc.git"})
	if got.Path != "./svc" {
		t.Errorf("path = %q", got.Path)
	}
}

func TestGetDbSourceFromPath(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	body := "DB_HOST=h\nDB_USER=u\nDB_PORT=42\n"
	if err := os.WriteFile(envFile, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	got := getDbSourceFromPath(envFile)
	if got.Host != "h" || got.User != "u" || got.Port != 42 {
		t.Errorf("got %+v", got)
	}
}

func TestGetDbSourceFromPathBadPort(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	if err := os.WriteFile(envFile, []byte("DB_PORT=notanint\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := getDbSourceFromPath(envFile)
	if got.Port != 0 {
		t.Errorf("expected 0 for bad port, got %d", got.Port)
	}
}

func TestCompareCorgiFilesEqual(t *testing.T) {
	c := &CorgiCompose{Name: "x"}
	if !CompareCorgiFiles(c, c) {
		t.Error("same pointer should be equal")
	}
}

func TestCompareCorgiFilesDifferent(t *testing.T) {
	a := &CorgiCompose{Name: "x"}
	b := &CorgiCompose{Name: "y"}
	if CompareCorgiFiles(a, b) {
		t.Error("expected not equal")
	}
}

func TestCleanCorgiServicesFolderNoop(t *testing.T) {
	prev, _ := os.Getwd()
	dir := t.TempDir()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(prev) })
	CleanCorgiServicesFolder()
}

func TestResolveDockerExposedPortNonDocker(t *testing.T) {
	s := Service{Runner: Runner{Name: ""}}
	resolveDockerExposedPort(&s)
	if s.Port != 0 {
		t.Errorf("got %d", s.Port)
	}
}

func TestResolveDockerExposedPortAlreadySet(t *testing.T) {
	s := Service{Runner: Runner{Name: "docker"}, Port: 8080}
	resolveDockerExposedPort(&s)
	if s.Port != 8080 {
		t.Errorf("got %d", s.Port)
	}
}

func TestResolveDockerExposedPortFromDockerfile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM alpine\nEXPOSE 9000\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s := Service{Runner: Runner{Name: "docker"}, AbsolutePath: dir}
	resolveDockerExposedPort(&s)
	if s.Port != 9000 {
		t.Errorf("got %d", s.Port)
	}
}

func newCobraWithRootFlags() *cobra.Command {
	c := &cobra.Command{}
	for _, f := range []string{"filename", "fromTemplate", "fromTemplateName", "privateToken", "dockerContext"} {
		c.Flags().String(f, "", "")
	}
	for _, f := range []string{"exampleList", "describe", "fromScratch", "runOnce"} {
		c.Flags().Bool(f, false, "")
	}
	return c
}

func TestResolveTemplatePathNoFlags(t *testing.T) {
	c := newCobraWithRootFlags()
	got, handled, err := resolveTemplatePath(c, "")
	if err != nil {
		t.Fatal(err)
	}
	if handled {
		t.Errorf("expected not handled, got %v", got)
	}
}

func TestResolveTemplatePathMissingFlag(t *testing.T) {
	c := &cobra.Command{}
	_, handled, err := resolveTemplatePath(c, "")
	if !handled || err == nil {
		t.Errorf("expected handled+err: handled=%v err=%v", handled, err)
	}
}

func TestDescribeServiceInfo(t *testing.T) {
	describeServiceInfo(map[string]int{"a": 1})
}

func TestCleanFromScratchDisabled(t *testing.T) {
	c := newCobraWithRootFlags()
	c.Flags().Set("fromScratch", "false")
	CleanFromScratch(c, CorgiCompose{})
}

func TestCleanFromScratchMissingFlag(t *testing.T) {
	CleanFromScratch(&cobra.Command{}, CorgiCompose{})
}

func TestResolveGlobalPathEmpty(t *testing.T) {
	prev := storageFilePath
	storageFilePath = "/no/such/zzz.txt"
	t.Cleanup(func() { storageFilePath = prev })
	_, err := resolveGlobalPath()
	if err == nil {
		t.Error("expected err")
	}
}

func TestToMapDatabaseServices(t *testing.T) {
	slice := []DatabaseService{
		{ServiceName: "db1", Driver: "postgres"},
		{ServiceName: "db2", Driver: "mysql"},
	}
	m := toMap(slice)
	if len(m) != 2 {
		t.Fatalf("want 2, got %d", len(m))
	}
	if _, ok := m["db1"]; !ok {
		t.Error("missing db1")
	}
	if _, ok := m["db2"]; !ok {
		t.Error("missing db2")
	}
}

func TestCompareCorgiFilesDifferentName(t *testing.T) {
	c1 := &CorgiCompose{Name: "a"}
	c2 := &CorgiCompose{Name: "b"}
	if CompareCorgiFiles(c1, c2) {
		t.Error("different names should not be equal")
	}
}

func TestCompareCorgiFilesDifferentServices(t *testing.T) {
	c1 := &CorgiCompose{Services: []Service{{ServiceName: "a"}}}
	c2 := &CorgiCompose{Services: []Service{{ServiceName: "b"}}}
	if CompareCorgiFiles(c1, c2) {
		t.Error("different services should not be equal")
	}
}

func TestCompareCorgiFilesDifferentInit(t *testing.T) {
	c1 := &CorgiCompose{Init: []string{"make setup"}}
	c2 := &CorgiCompose{Init: []string{"make other"}}
	if CompareCorgiFiles(c1, c2) {
		t.Error("different init should not be equal")
	}
}

func TestNormalizeServicePathAddsPrefix(t *testing.T) {
	s := &Service{Path: "myapp"}
	normalizeServicePath(s)
	if s.Path != "./myapp" {
		t.Errorf("got %q", s.Path)
	}
}

func TestNormalizeServicePathDotPrefixed(t *testing.T) {
	s := &Service{Path: "."}
	normalizeServicePath(s)
	if s.Path != "./." {
		t.Errorf("got %q", s.Path)
	}
}

func TestNormalizeServicePathAlreadyPrefixed(t *testing.T) {
	s := &Service{Path: "./svc"}
	normalizeServicePath(s)
	if s.Path != "./svc" {
		t.Errorf("got %q", s.Path)
	}
}

func TestNormalizeServicePathEmpty(t *testing.T) {
	s := &Service{Path: ""}
	normalizeServicePath(s)
	if s.Path != "" {
		t.Errorf("got %q", s.Path)
	}
}

func TestResolveServicePathFromCloneFrom(t *testing.T) {
	s := &Service{CloneFrom: "https://github.com/user/myrepo.git"}
	resolveServicePathFromCloneFrom(s)
	if s.Path != "./myrepo" {
		t.Errorf("got %q", s.Path)
	}
}

func TestResolveServicePathFromCloneFromNotGit(t *testing.T) {
	s := &Service{CloneFrom: "https://github.com/user/myrepo"}
	resolveServicePathFromCloneFrom(s)
	if s.Path != "" {
		t.Errorf("expected empty, got %q", s.Path)
	}
}

func TestResolveServicePathFromCloneFromPathAlreadySet(t *testing.T) {
	s := &Service{Path: "./existing", CloneFrom: "https://github.com/user/repo.git"}
	resolveServicePathFromCloneFrom(s)
	if s.Path != "./existing" {
		t.Errorf("path should not change, got %q", s.Path)
	}
}

func TestCleanCorgiServicesFolderMissing(t *testing.T) {
	// Should not panic when folder doesn't exist
	CleanCorgiServicesFolder()
}

func TestCleanFromScratchEnabled(t *testing.T) {
	c := newCobraWithRootFlags()
	if err := c.Flags().Set("fromScratch", "true"); err != nil {
		t.Fatal(err)
	}

	cwd, _ := os.Getwd()
	dir := t.TempDir()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })

	// No DatabaseServices → just calls CleanCorgiServicesFolder (os.RemoveAll on tmp)
	CleanFromScratch(c, CorgiCompose{})
}

func TestCleanFromScratchEnabledWithDbs(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })

	c := newCobraWithRootFlags()
	if err := c.Flags().Set("fromScratch", "true"); err != nil {
		t.Fatal(err)
	}
	dbDir := filepath.Join(CorgiComposePathDir, RootDbServicesFolder, "mydb")
	os.MkdirAll(dbDir, 0755)
	os.WriteFile(filepath.Join(dbDir, "Makefile"), []byte("remove:\n\t@echo noop\n"), 0644)

	cwd, _ := os.Getwd()
	dir := t.TempDir()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })

	CleanFromScratch(c, CorgiCompose{
		DatabaseServices: []DatabaseService{{ServiceName: "mydb"}},
	})
}

func TestGetCorgiConfigFilePathExists(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, CorgiComposeDefaultName), []byte("name: t\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })

	got, err := getCorgiConfigFilePath()
	if err != nil {
		t.Fatal(err)
	}
	if got != CorgiComposeDefaultName {
		t.Errorf("got %q", got)
	}
}

func TestGetCorgiServicesWithDbService(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "corgi-compose.yml")
	content := `name: testapp
db_services:
  mydb:
    driver: postgres
    host: localhost
    user: root
    password: secret
    databaseName: mydb
    port: 5432
services:
  api:
    port: 3000
    cloneFrom: ""
    path: ""
`
	if err := os.WriteFile(yml, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })

	c := newCobraWithRootFlags()
	c.Flags().Bool("global", false, "")
	corgi, err := GetCorgiServices(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(corgi.DatabaseServices) != 1 {
		t.Errorf("expected 1 db service, got %d", len(corgi.DatabaseServices))
	}
	if len(corgi.Services) != 1 {
		t.Errorf("expected 1 service, got %d", len(corgi.Services))
	}
}

func TestDetermineCorgiComposePathWithFilenameFlag(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "custom.yml")
	if err := os.WriteFile(yml, []byte("name: custom\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })

	c := newCobraWithRootFlags()
	c.Flags().Bool("global", false, "")
	c.Flags().Set("filename", "custom.yml")
	path, err := determineCorgiComposePath(c)
	if err != nil {
		t.Fatal(err)
	}
	if path != "custom.yml" {
		t.Errorf("expected custom.yml, got %q", path)
	}
}

func TestParseDatabaseServicesWithRabbitMQ(t *testing.T) {
	dbMap := map[string]DatabaseService{
		"mq": {
			Driver:       "rabbitmq",
			Port:         5672,
			User:         "guest",
			Password:     "guest",
			DatabaseName: "vhost",
		},
	}
	prev := DbServicesItemsFromFlag
	DbServicesItemsFromFlag = nil
	t.Cleanup(func() { DbServicesItemsFromFlag = prev })
	services, err := parseDatabaseServices(dbMap, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(services))
	}
	if services[0].Driver != "rabbitmq" {
		t.Errorf("expected rabbitmq driver, got %s", services[0].Driver)
	}
}

func TestCleanCorgiServicesFolderWithData(t *testing.T) {
	prev, _ := os.Getwd()
	dir := t.TempDir()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(prev) })

	if err := os.MkdirAll(filepath.Join(dir, "corgi_services", "db_services", "pg"), 0755); err != nil {
		t.Fatal(err)
	}
	CleanCorgiServicesFolder()
	if _, err := os.Stat(filepath.Join(dir, "corgi_services")); !os.IsNotExist(err) {
		t.Error("expected corgi_services to be removed")
	}
}

func TestMetadataJSONValid(t *testing.T) {
	u := SupabaseAuthUser{Email: "test@example.com"}
	j := u.MetadataJSON()
	if j == "" {
		t.Error("expected non-empty JSON")
	}
}

func TestParseDatabaseServicesNoneFlag(t *testing.T) {
	prev := DbServicesItemsFromFlag
	DbServicesItemsFromFlag = []string{"none"}
	t.Cleanup(func() { DbServicesItemsFromFlag = prev })
	got, err := parseDatabaseServices(map[string]DatabaseService{"x": {Driver: "postgres"}}, false)
	if err != nil || got != nil {
		t.Errorf("expected nil/nil, got %v, %v", got, err)
	}
}

func TestParseServicesNoneFlag(t *testing.T) {
	prev := ServicesItemsFromFlag
	ServicesItemsFromFlag = []string{"none"}
	t.Cleanup(func() { ServicesItemsFromFlag = prev })
	if got := parseServices(map[string]Service{"x": {}}, false); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestParseDatabaseServicesFiltersByFlag(t *testing.T) {
	prev := DbServicesItemsFromFlag
	DbServicesItemsFromFlag = []string{"keep"}
	t.Cleanup(func() { DbServicesItemsFromFlag = prev })
	got, err := parseDatabaseServices(map[string]DatabaseService{
		"keep": {Driver: "postgres"},
		"skip": {Driver: "mysql"},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ServiceName != "keep" {
		t.Errorf("expected only keep, got %+v", got)
	}
}

func TestParseDatabaseServicesDescribe(t *testing.T) {
	prev := DbServicesItemsFromFlag
	DbServicesItemsFromFlag = nil
	t.Cleanup(func() { DbServicesItemsFromFlag = prev })
	got, err := parseDatabaseServices(map[string]DatabaseService{"x": {Driver: "postgres"}}, true)
	if err != nil || len(got) != 1 {
		t.Errorf("got %v %v", got, err)
	}
}

func TestParseServicesDescribe(t *testing.T) {
	prev := ServicesItemsFromFlag
	ServicesItemsFromFlag = nil
	t.Cleanup(func() { ServicesItemsFromFlag = prev })
	got := parseServices(map[string]Service{"x": {Port: 1234}}, true)
	if len(got) != 1 {
		t.Errorf("got %d", len(got))
	}
}

func TestMergeSeedFromDbAllOverrides(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	if err := os.WriteFile(envFile, []byte("DB_HOST=h\nDB_NAME=n\nDB_USER=u\nDB_PASSWORD=p\nDB_PORT=10\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := mergeSeedFromDb(DatabaseService{
		SeedFromDbEnvPath: envFile,
		SeedFromDb: SeedFromDb{
			Host:         "OV_H",
			DatabaseName: "OV_N",
			User:         "OV_U",
			Password:     "OV_P",
			Port:         99,
		},
	})
	if got.Host != "OV_H" || got.DatabaseName != "OV_N" || got.User != "OV_U" || got.Password != "OV_P" || got.Port != 99 {
		t.Errorf("overrides not applied: %+v", got)
	}
}

func TestBuildDatabaseServiceLocalstackInjects(t *testing.T) {
	got, err := buildDatabaseService("ls", DatabaseService{
		Driver:   "localstack",
		Services: []string{"s3", "sqs"},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Driver != "localstack" {
		t.Errorf("got driver %q", got.Driver)
	}
}

func TestResolveTemplatePathFromTemplateName(t *testing.T) {
	c := newCobraWithRootFlags()
	c.Flags().Set("fromTemplateName", "nonexistent-example-name")
	_, handled, _ := resolveTemplatePath(c, "")
	if !handled {
		t.Error("expected handled=true when fromTemplateName set")
	}
}

func TestResolveTemplatePathExampleList(t *testing.T) {
	c := newCobraWithRootFlags()
	c.Flags().Set("exampleList", "true")
	_, handled, _ := resolveTemplatePath(c, "")
	if !handled {
		t.Error("expected handled=true when exampleList set")
	}
}

func TestResolveTemplatePathFromTemplateBadURL(t *testing.T) {
	c := newCobraWithRootFlags()
	c.Flags().Set("fromTemplate", "://broken")
	_, handled, err := resolveTemplatePath(c, "")
	if !handled || err == nil {
		t.Errorf("expected handled+err, got handled=%v err=%v", handled, err)
	}
}

func TestSelectGlobalExecPathEmpty(t *testing.T) {
	prev := storageFilePath
	storageFilePath = "/no/such/zzz.txt"
	t.Cleanup(func() { storageFilePath = prev })
	_, err := selectGlobalExecPath()
	if err == nil {
		t.Error("expected err")
	}
}

func TestDescribeServiceInfoUnsupported(t *testing.T) {
	describeServiceInfo(make(chan int))
}

func TestDetermineCorgiComposePathGlobalNoData(t *testing.T) {
	prev := storageFilePath
	storageFilePath = "/no/such/zzz.txt"
	t.Cleanup(func() { storageFilePath = prev })
	c := newCobraWithRootFlags()
	c.Flags().Bool("global", true, "")
	_, err := determineCorgiComposePath(c)
	if err == nil {
		t.Error("expected err for no global path")
	}
}

