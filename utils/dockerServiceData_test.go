package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func dockerSvc(t *testing.T, dockerfileContent string) Service {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(dockerfileContent), 0644); err != nil {
		t.Fatal(err)
	}
	s := Service{ServiceName: "Api", AbsolutePath: dir, Port: 3084}
	s.Runner.Name = "docker"
	s.ResolvedDockerSource = SourceDockerfile
	return s
}

func TestBuildDockerServiceDataDefaults(t *testing.T) {
	s := dockerSvc(t, "FROM alpine\nEXPOSE 3000\n")
	d, err := BuildDockerServiceData(s)
	if err != nil {
		t.Fatal(err)
	}
	if d.DockerName != "api" || d.BuildContext != s.AbsolutePath ||
		d.DockerfilePath != "Dockerfile" || d.Port != 3084 || d.ContainerPort != 3000 {
		t.Fatalf("%+v", d)
	}
}

func TestBuildDockerServiceDataVolumesRewritten(t *testing.T) {
	s := dockerSvc(t, "EXPOSE 3000\n")
	s.Runner.Volumes = []string{"./src:/app/src", "/abs:/data", "/app/node_modules"}
	d, err := BuildDockerServiceData(s)
	if err != nil {
		t.Fatal(err)
	}
	want0 := filepath.Join(s.AbsolutePath, "src") + ":/app/src"
	if d.Volumes[0] != want0 || d.Volumes[1] != "/abs:/data" || d.Volumes[2] != "/app/node_modules" {
		t.Fatalf("%v", d.Volumes)
	}
}

func TestBuildDockerServiceDataNamedAndRoVolumes(t *testing.T) {
	s := dockerSvc(t, "EXPOSE 3000\n")
	s.Runner.Volumes = []string{"mydata:/var/lib/x", "./src:/app/src:ro"}
	d, err := BuildDockerServiceData(s)
	if err != nil {
		t.Fatal(err)
	}
	if d.Volumes[0] != "mydata:/var/lib/x" {
		t.Fatalf("named volume must pass through, got %q", d.Volumes[0])
	}
	if len(d.NamedVolumes) != 1 || d.NamedVolumes[0] != "mydata" {
		t.Fatalf("named volume must be declared top-level, got %v", d.NamedVolumes)
	}
	wantRo := filepath.Join(s.AbsolutePath, "src") + ":/app/src:ro"
	if d.Volumes[1] != wantRo {
		t.Fatalf("ro bind must keep options, got %q", d.Volumes[1])
	}
}

func TestBuildDockerServiceDataContainerPortPrecedence(t *testing.T) {
	s := dockerSvc(t, "EXPOSE 3000\n")
	s.Runner.ContainerPort = 8080
	d, _ := BuildDockerServiceData(s)
	if d.ContainerPort != 8080 {
		t.Fatalf("containerPort must win, got %d", d.ContainerPort)
	}
}

func TestBuildDockerServiceDataNoExposeFallsBackToPort(t *testing.T) {
	s := dockerSvc(t, "FROM alpine\n")
	d, _ := BuildDockerServiceData(s)
	if d.ContainerPort != 3084 {
		t.Fatalf("want fallback 3084, got %d", d.ContainerPort)
	}
}

func TestBuildDockerServiceDataRepoCompose(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services: {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s := Service{ServiceName: "api", AbsolutePath: dir, Port: 3084}
	s.Runner.Name = "docker"
	s.ResolvedDockerSource = SourceRepoCompose
	d, err := BuildDockerServiceData(s)
	if err != nil {
		t.Fatal(err)
	}
	if d.RepoComposeFile != filepath.Join(dir, "docker-compose.yml") {
		t.Fatalf("%+v", d)
	}
	if d.EnvFilePath == "" {
		t.Fatal("EnvFilePath must be set")
	}
}

func TestBuildDockerServiceDataCustomContext(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "deploy")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile.dev"), []byte("EXPOSE 3000\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s := Service{ServiceName: "api", AbsolutePath: dir, Port: 3084}
	s.Runner = Runner{Name: "docker", Context: "./deploy", Dockerfile: "Dockerfile.dev"}
	s.ResolvedDockerSource = SourceDockerfile
	d, err := BuildDockerServiceData(s)
	if err != nil {
		t.Fatal(err)
	}
	if d.BuildContext != sub || d.DockerfilePath != filepath.Join("..", "Dockerfile.dev") {
		t.Fatalf("%+v", d)
	}
}

func TestBuildDockerServiceDataImageMode(t *testing.T) {
	s := Service{ServiceName: "pdf", AbsolutePath: t.TempDir(), Port: 3005}
	s.Runner = Runner{Name: "docker", Image: "gotenberg/gotenberg:8", ContainerPort: 3000}
	s.ResolvedDockerSource = SourceImage
	d, err := BuildDockerServiceData(s)
	if err != nil {
		t.Fatal(err)
	}
	if d.Image != "gotenberg/gotenberg:8" || d.ContainerPort != 3000 || d.Port != 3005 {
		t.Fatalf("%+v", d)
	}
	if d.BuildContext != "" {
		t.Fatal("image mode must not set a build context")
	}
}

func TestBuildDockerServiceDataImageContainerPortDefaultsToPort(t *testing.T) {
	s := Service{ServiceName: "pdf", AbsolutePath: t.TempDir(), Port: 3005}
	s.Runner = Runner{Name: "docker", Image: "nginx:alpine"}
	s.ResolvedDockerSource = SourceImage
	d, _ := BuildDockerServiceData(s)
	if d.ContainerPort != 3005 {
		t.Fatalf("want 3005, got %d", d.ContainerPort)
	}
}

func TestRepoComposeServiceNames(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(p, []byte("services:\n  web:\n    image: x\n  db:\n    image: y\n"), 0644); err != nil {
		t.Fatal(err)
	}
	names := RepoComposeServiceNames(p)
	if len(names) != 2 || names[0] != "db" || names[1] != "web" {
		t.Fatalf("got %v", names)
	}
	if RepoComposeServiceNames(filepath.Join(dir, "missing.yml")) != nil {
		t.Fatal("missing file must yield nil")
	}
	bad := filepath.Join(dir, "bad.yml")
	os.WriteFile(bad, []byte(":{not yaml"), 0644)
	if RepoComposeServiceNames(bad) != nil {
		t.Fatal("unparsable file must yield nil")
	}
}

func TestRenderRepoComposeEnvOverride(t *testing.T) {
	out := RenderRepoComposeEnvOverride([]string{"db", "web"}, "/some dir/.env")
	for _, want := range []string{"services:", "  db:", "  web:", `- "/some dir/.env"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRepoComposeDataGetsOverrideFile(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	defer func() { CorgiComposePathDir = prev }()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"),
		[]byte("services:\n  web:\n    image: x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s := Service{ServiceName: "api", AbsolutePath: dir}
	s.Runner.Name = "docker"
	s.ResolvedDockerSource = SourceRepoCompose
	d, err := BuildDockerServiceData(s)
	if err != nil {
		t.Fatal(err)
	}
	if d.OverrideFile == "" || !strings.HasSuffix(d.OverrideFile, "corgi.env.override.yml") {
		t.Fatalf("override file expected, got %q", d.OverrideFile)
	}
}
