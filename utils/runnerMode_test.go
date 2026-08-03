package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func mkModeService(t *testing.T, files ...string) Service {
	t.Helper()
	dir := t.TempDir()
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0644); err != nil {
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
			out, err := ResolveRunnerModes([]Service{c.svc(t)}, c.dockerFlag)
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

func TestComposeFileAndBuildFieldsMutuallyExclusive(t *testing.T) {
	s := mkModeService(t, "compose.yml")
	s.Runner = Runner{Name: "docker", ComposeFile: "./compose.yml", Target: "dev"}
	if _, err := ResolveRunnerModes([]Service{s}, false); err == nil {
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
