package cmd

import (
	"andriiklymiuk/corgi/utils"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetPathToFileName(t *testing.T) {
	tests := []struct {
		in, wantFile, wantPath string
	}{
		{"docker-compose.yml", "docker-compose.yml", ""},
		{"bootstrap/bootstrap.sh", "bootstrap.sh", "bootstrap/"},
		{"a/b/c.txt", "c.txt", "a/b/"},
	}
	for _, tt := range tests {
		gotFile, gotPath := getPathToFileName(tt.in)
		if gotFile != tt.wantFile || gotPath != tt.wantPath {
			t.Errorf("getPathToFileName(%q) = (%q,%q) want (%q,%q)", tt.in, gotFile, gotPath, tt.wantFile, tt.wantPath)
		}
	}
}

func TestGetGitignoreServicePath(t *testing.T) {
	services := []utils.Service{
		{ServiceName: "no-clone", Path: "./svc"},
		{ServiceName: "no-path", CloneFrom: "x"},
		{ServiceName: "with-parent", Path: "../up", CloneFrom: "x"},
		{ServiceName: "deep", Path: "./a/b", CloneFrom: "x"},
		{ServiceName: "ok", Path: "./api", CloneFrom: "x"},
	}
	got := getGitignoreServicePath(services, []string{"# header"})
	if len(got) != 2 || got[1] != "api" {
		t.Errorf("got %v", got)
	}
}

func TestGetFilesToCreateKnownDriver(t *testing.T) {
	files := getFilesToCreate("postgres")
	if len(files) == 0 {
		t.Errorf("expected files for postgres")
	}
}

func TestGetFilesToCreateUnknownFallsBackToDefault(t *testing.T) {
	files := getFilesToCreate("nonexistent-driver-xyz")
	if len(files) == 0 {
		t.Errorf("expected default fallback")
	}
}

func TestGetServiceFilesToCreate(t *testing.T) {
	files := getServiceFilesToCreate("docker")
	if len(files) == 0 {
		t.Errorf("expected docker service files")
	}
	if got := getServiceFilesToCreate("nope"); got != nil {
		t.Errorf("expected nil for unknown driver")
	}
}

func TestShouldCreateServiceNotDocker(t *testing.T) {
	if shouldCreateService(utils.Service{Runner: utils.Runner{Name: ""}}) {
		t.Error("want false for empty runner")
	}
	if shouldCreateService(utils.Service{Runner: utils.Runner{Name: "node"}}) {
		t.Error("want false for non-docker runner")
	}
}

func TestShouldCreateServiceDockerNoPort(t *testing.T) {
	if shouldCreateService(utils.Service{Runner: utils.Runner{Name: "docker"}, Port: 0}) {
		t.Error("want false when no port")
	}
}

func TestShouldCreateServiceDockerNoDockerfile(t *testing.T) {
	dir := t.TempDir()
	if shouldCreateService(utils.Service{
		Runner:       utils.Runner{Name: "docker"},
		Port:         8080,
		AbsolutePath: dir,
	}) {
		t.Error("want false when no Dockerfile")
	}
}

func TestShouldCreateServiceDockerWithDockerfile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM alpine"), 0644); err != nil {
		t.Fatal(err)
	}
	if !shouldCreateService(utils.Service{
		Runner:       utils.Runner{Name: "docker"},
		Port:         8080,
		AbsolutePath: dir,
	}) {
		t.Error("want true when Dockerfile present")
	}
}

func TestCheckClonedReposExistenceNoClones(t *testing.T) {
	services := []utils.Service{
		{ServiceName: "x", Path: "./x"},
	}
	if CheckClonedReposExistence(services) {
		t.Error("want false")
	}
}

func TestCheckClonedReposExistenceMissing(t *testing.T) {
	services := []utils.Service{
		{ServiceName: "x", Path: "./x", CloneFrom: "g", AbsolutePath: "/nonexistent/abc"},
	}
	if !CheckClonedReposExistence(services) {
		t.Error("want true (missing path)")
	}
}

func TestCheckClonedReposExistenceWithBranch(t *testing.T) {
	dir := t.TempDir()
	services := []utils.Service{
		{ServiceName: "x", Path: "./x", CloneFrom: "g", Branch: "main", AbsolutePath: dir},
	}
	if !CheckClonedReposExistence(services) {
		t.Error("want true (branch set)")
	}
}

func TestCreateMissingEnvFilesIdempotent(t *testing.T) {
	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	CreateMissingEnvFiles([]utils.Service{
		{CopyEnvFromFilePath: "envs/.env"},
	})
	if _, err := os.Stat(filepath.Join(utils.CorgiComposePathDir, "envs/.env")); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestCloneOneServiceNoPath(t *testing.T) {
	cloneOneService(utils.Service{ServiceName: "x", Path: ""})
}

func TestApplyDriverPostInitNonSupabase(t *testing.T) {
	if err := applyDriverPostInit(utils.DatabaseService{Driver: "postgres"}); err != nil {
		t.Errorf("expected nil: %v", err)
	}
}

func TestApplyDriverPostInitSupabaseNoConfig(t *testing.T) {
	if err := applyDriverPostInit(utils.DatabaseService{Driver: "supabase"}); err != nil {
		t.Errorf("expected nil: %v", err)
	}
}

func TestApplyDriverPostInitSupabaseMissingFile(t *testing.T) {
	err := applyDriverPostInit(utils.DatabaseService{
		Driver:         "supabase",
		ConfigTomlPath: "/no/such/file/here.toml",
	})
	if err == nil || !strings.Contains(err.Error(), "read configTomlPath") {
		t.Errorf("expected read error, got %v", err)
	}
}

func TestApplyDriverPostInitSupabaseCopiesFile(t *testing.T) {
	src := t.TempDir()
	srcFile := filepath.Join(src, "config.toml")
	if err := os.WriteFile(srcFile, []byte("[api]\nport=1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	err := applyDriverPostInit(utils.DatabaseService{
		Driver:         "supabase",
		ServiceName:    "supa",
		ConfigTomlPath: srcFile,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	dest := filepath.Join(utils.CorgiComposePathDir, utils.RootDbServicesFolder, "supa", "supabase", "config.toml")
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("dest missing: %v", err)
	}
	if !strings.Contains(string(body), "port=1") {
		t.Errorf("contents wrong: %s", body)
	}
}

func TestCreateDatabaseServicesEmpty(t *testing.T) {
	CreateDatabaseServices(nil)
}

func TestCreateServicesEmpty(t *testing.T) {
	CreateServices(nil)
}

func TestCreateSingleServiceSkipsNonDocker(t *testing.T) {
	createSingleService(utils.Service{ServiceName: "x", Runner: utils.Runner{Name: ""}})
}

func TestCreateFileFromTemplate(t *testing.T) {
	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	err := createFileFromTemplate(
		utils.DatabaseService{ServiceName: "db1", Driver: "postgres"},
		"out.txt",
		"hello {{.ServiceName}}",
		"db1",
		"corgi_services/db_services",
	)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	out := filepath.Join(utils.CorgiComposePathDir, "corgi_services/db_services/db1/out.txt")
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("missing: %v", err)
	}
	if !strings.Contains(string(body), "hello db1") {
		t.Errorf("got %q", body)
	}
}

func TestCreateFileFromTemplateNestedPath(t *testing.T) {
	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	err := createFileFromTemplate(
		utils.DatabaseService{ServiceName: "db", Driver: "postgres"},
		"sub/file.sh",
		"x",
		"db",
		"corgi_services/db_services",
	)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	out := filepath.Join(utils.CorgiComposePathDir, "corgi_services/db_services/db/sub/file.sh")
	if _, err := os.Stat(out); err != nil {
		t.Errorf("file missing: %v", err)
	}
}

func TestCopyEnvFileWithSubstitutionsMissing(t *testing.T) {
	err := copyEnvFileWithSubstitutions(utils.Service{
		ServiceName:  "api",
		AbsolutePath: t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("expected missing error, got %v", err)
	}
}

func TestRunGitCloneRejectsExitStatus128(t *testing.T) {
	dir := t.TempDir()
	ok := runGitClone(utils.Service{
		ServiceName:  "x",
		CloneFrom:    "fake://cloneurl",
		AbsolutePath: dir,
	}, dir)
	if ok {
		t.Error("expected false")
	}
}

func TestHandleExistingServiceDirNoOps(t *testing.T) {
	handleExistingServiceDir(utils.Service{ServiceName: "x"})
	handleExistingServiceDir(utils.Service{ServiceName: "x", CloneFrom: "y"})
}

func TestMaybeRunNestedCorgiInitNoCompose(t *testing.T) {
	maybeRunNestedCorgiInit(utils.Service{
		ServiceName:  "x",
		AbsolutePath: t.TempDir(),
		CloneFrom:    "y",
	})
}


func TestAddFileToGitignoreNew(t *testing.T) {
	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	if err := addFileToGitignore("foo/*"); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(utils.CorgiComposePathDir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "foo/*") {
		t.Errorf("missing entry: %s", body)
	}
}

func TestAddFileToGitignoreIdempotent(t *testing.T) {
	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	if err := addFileToGitignore(".env"); err != nil {
		t.Fatal(err)
	}
	if err := addFileToGitignore(".env"); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(utils.CorgiComposePathDir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(body), ".env") != 1 {
		t.Errorf("duplicate entry, got %s", body)
	}
}

func TestCloneMissingServiceDirNoCloneFrom(t *testing.T) {
	if cloneMissingServiceDir(utils.Service{ServiceName: "x", AbsolutePath: t.TempDir()}) {
		t.Error("expected false")
	}
}

func TestCreateDatabaseServicesAddsFiles(t *testing.T) {
	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	CreateDatabaseServices([]utils.DatabaseService{
		{ServiceName: "db1", Driver: "postgres", Host: "localhost", User: "u", Password: "p", DatabaseName: "d", Port: 5432},
	})
	dest := filepath.Join(utils.CorgiComposePathDir, utils.RootDbServicesFolder, "db1", "docker-compose.yml")
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestWriteServiceFiles(t *testing.T) {
	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM alpine\nEXPOSE 80\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := writeServiceFiles(utils.Service{
		ServiceName:  "api",
		Runner:       utils.Runner{Name: "docker"},
		Port:         80,
		AbsolutePath: dir,
	})
	if !got {
		t.Error("expected true")
	}
}

func TestCloneServicesEmpty(t *testing.T) {
	CloneServices(nil)
}

func TestCreateServicesIterates(t *testing.T) {
	CreateServices([]utils.Service{
		{ServiceName: "x", Runner: utils.Runner{Name: ""}},
	})
}

func TestRunBranchCheckoutPathMissing(t *testing.T) {
	runBranchCheckout(utils.Service{
		ServiceName:  "x",
		AbsolutePath: "/nonexistent/zzz",
		Branch:       "main",
	})
}

func TestHandleExistingServiceDirNoCloneFrom(t *testing.T) {
	handleExistingServiceDir(utils.Service{ServiceName: "x"})
}

func TestHandleExistingServiceDirNoBranch(t *testing.T) {
	handleExistingServiceDir(utils.Service{ServiceName: "x", CloneFrom: "g"})
}
