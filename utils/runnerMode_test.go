package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mkModeService(t *testing.T, files ...string) Service {
	t.Helper()
	dir := t.TempDir()
	for _, f := range files {
		content := "x"
		if strings.HasPrefix(filepath.Base(f), "Dockerfile") {
			content = "FROM alpine\nEXPOSE 3000\n"
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, f)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, f), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return Service{ServiceName: "s", AbsolutePath: dir}
}

func TestResolveRunnerModes(t *testing.T) {
	cases := []struct {
		name       string
		svc        func(t *testing.T) Service
		dockerFlag bool
		wantDocker bool
		wantSource DockerSource
		wantErr    bool
	}{
		{"scripts stay native", func(t *testing.T) Service {
			s := mkModeService(t, "Dockerfile")
			s.Start = []string{"npm run dev"}
			return s
		}, false, false, SourceNone, false},
		{"no scripts + Dockerfile -> docker", func(t *testing.T) Service {
			return mkModeService(t, "Dockerfile")
		}, false, true, SourceDockerfile, false},
		{"no scripts + repo compose wins over Dockerfile", func(t *testing.T) Service {
			return mkModeService(t, "Dockerfile", "docker-compose.yml")
		}, false, true, SourceRepoCompose, false},
		{"--docker flips scripted service", func(t *testing.T) Service {
			s := mkModeService(t, "Dockerfile")
			s.Start = []string{"npm run dev"}
			return s
		}, true, true, SourceDockerfile, false},
		{"--docker leaves non-capable native", func(t *testing.T) Service {
			s := mkModeService(t)
			s.Start = []string{"npm run dev"}
			return s
		}, true, false, SourceNone, false},
		{"explicit runner without any source errors", func(t *testing.T) Service {
			s := mkModeService(t)
			s.Runner.Name = "docker"
			return s
		}, false, false, SourceNone, true},
		{"declared composeFile", func(t *testing.T) Service {
			s := mkModeService(t, "compose.custom.yml")
			s.Runner.ComposeFile = "./compose.custom.yml"
			s.Runner.Name = "docker"
			return s
		}, false, true, SourceRepoCompose, false},
		{"manualRun untouched", func(t *testing.T) Service {
			s := mkModeService(t, "Dockerfile")
			s.ManualRun = true
			return s
		}, false, false, SourceNone, false},
		{"custom dockerfile name detected", func(t *testing.T) Service {
			s := mkModeService(t, "Dockerfile.dev")
			s.Runner.Dockerfile = "Dockerfile.dev"
			return s
		}, false, true, SourceDockerfile, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := ResolveRunnerModes([]Service{c.svc(t)}, c.dockerFlag, false)
			if c.wantErr != (err != nil) {
				t.Fatalf("err=%v", err)
			}
			if err != nil {
				return
			}
			if got := out[0].Runner.IsDocker(); got != c.wantDocker ||
				out[0].ResolvedDockerSource != c.wantSource {
				t.Fatalf("docker=%v source=%v", got, out[0].ResolvedDockerSource)
			}
		})
	}
}

func TestBuildFieldsBeatRepoCompose(t *testing.T) {
	s := mkModeService(t, "Dockerfile.dev", "docker-compose.yml")
	s.Runner = Runner{Name: "docker", Dockerfile: "Dockerfile.dev", Target: "dev"}
	out, err := ResolveRunnerModes([]Service{s}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].ResolvedDockerSource != SourceDockerfile {
		t.Fatal("declared build fields must pin the Dockerfile, not the repo compose")
	}
}

func TestExplicitRunnerPrefersDockerfileOverRepoCompose(t *testing.T) {
	s := mkModeService(t, "Dockerfile", "docker-compose.yml")
	s.Runner.Name = "docker"
	out, err := ResolveRunnerModes([]Service{s}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].ResolvedDockerSource != SourceDockerfile {
		t.Fatal("explicit runner: docker must keep pre-existing Dockerfile behavior")
	}
}

func TestSubdirDockerfileDetected(t *testing.T) {
	s := mkModeService(t, "docker/Dockerfile.dev")
	s.Runner = Runner{Name: "docker", Dockerfile: "docker/Dockerfile.dev"}
	out, err := ResolveRunnerModes([]Service{s}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].ResolvedDockerSource != SourceDockerfile {
		t.Fatal("dockerfile in a subdirectory must be found")
	}
}

func TestAutoDockerResolvesPortFromExpose(t *testing.T) {
	s := mkModeService(t, "Dockerfile") // EXPOSE 3000 in fixture
	out, err := ResolveRunnerModes([]Service{s}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !out[0].Runner.IsDocker() || out[0].Port != 3000 {
		t.Fatalf("want docker with port 3000 from EXPOSE, got docker=%v port=%d",
			out[0].Runner.IsDocker(), out[0].Port)
	}
}

func TestAutoDockerNoExposeFallsBackNative(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM alpine\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s := Service{ServiceName: "s", AbsolutePath: dir}
	out, err := ResolveRunnerModes([]Service{s}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Runner.IsDocker() {
		t.Fatal("no port + no EXPOSE must not flip to docker (stays inert like before)")
	}
}

func TestExplicitRunnerNoExposeNoPortErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM alpine\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s := Service{ServiceName: "s", AbsolutePath: dir}
	s.Runner.Name = "docker"
	if _, err := ResolveRunnerModes([]Service{s}, false, false); err == nil {
		t.Fatal("declared docker runner without any port must be a config error")
	}
}

func TestClonedRepoWithNothingRunnableErrors(t *testing.T) {
	s := mkModeService(t) // dir exists, empty
	s.Port = 3000
	if _, err := ResolveRunnerModes([]Service{s}, false, false); err == nil {
		t.Fatal("port + no start + no docker source must error post-clone")
	}
}

func TestComposeFileAndBuildFieldsMutuallyExclusive(t *testing.T) {
	s := mkModeService(t, "compose.yml")
	s.Runner = Runner{Name: "docker", ComposeFile: "./compose.yml", Target: "dev"}
	if _, err := ResolveRunnerModes([]Service{s}, false, false); err == nil {
		t.Fatal("want error")
	}
}

func TestRepoComposeFileDetection(t *testing.T) {
	s := mkModeService(t, "compose.yaml")
	if got := RepoComposeFile(s); got != filepath.Join(s.AbsolutePath, "compose.yaml") {
		t.Fatalf("got %q", got)
	}
	none := mkModeService(t)
	if got := RepoComposeFile(none); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestDeclaredDockerfileMissingErrors(t *testing.T) {
	s := mkModeService(t, "docker-compose.yml")
	s.Runner = Runner{Name: "docker", Dockerfile: "Dockerfile.dev"}
	if _, err := ResolveRunnerModes([]Service{s}, false, false); err == nil {
		t.Fatal("missing declared dockerfile must not silently fall back to repo compose")
	}
}

func TestDeclaredDockerfileMissingIgnoredForNativeService(t *testing.T) {
	s := mkModeService(t)
	s.Start = []string{"npm run dev"}
	s.Runner.Dockerfile = "Dockerfile.dev"
	if _, err := ResolveRunnerModes([]Service{s}, false, false); err != nil {
		t.Fatalf("native scripted run must not fail on stale dockerfile config: %v", err)
	}
}

func TestImageRunnerResolvesToSourceImage(t *testing.T) {
	s := Service{ServiceName: "pdf", AbsolutePath: t.TempDir(), Port: 3005}
	s.Runner = Runner{Name: "docker", Image: "nginx:alpine"}
	out, err := ResolveRunnerModes([]Service{s}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].ResolvedDockerSource != SourceImage {
		t.Fatalf("want SourceImage, got %v", out[0].ResolvedDockerSource)
	}
}

func TestImageRunnerWithoutPortErrors(t *testing.T) {
	s := Service{ServiceName: "pdf", AbsolutePath: t.TempDir()}
	s.Runner = Runner{Name: "docker", Image: "nginx:alpine"}
	if _, err := ResolveRunnerModes([]Service{s}, false, false); err == nil {
		t.Fatal("image without port must error")
	}
}

func TestImageAndDockerfileMutuallyExclusive(t *testing.T) {
	s := mkModeService(t, "Dockerfile")
	s.Port = 3000
	s.Runner = Runner{Name: "docker", Image: "nginx:alpine", Dockerfile: "Dockerfile"}
	if _, err := ResolveRunnerModes([]Service{s}, false, false); err == nil {
		t.Fatal("image + dockerfile must error")
	}
}
