package utils

import (
	"os"
	"path/filepath"
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
