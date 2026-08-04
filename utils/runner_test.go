package utils

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRunnerUnmarshalScalar(t *testing.T) {
	var s Service
	if err := yaml.Unmarshal([]byte("runner: docker\n"), &s); err != nil {
		t.Fatal(err)
	}
	if s.Runner.Name != "docker" {
		t.Fatalf("want docker, got %q", s.Runner.Name)
	}
}

func TestRunnerUnmarshalObject(t *testing.T) {
	src := `
runner:
  name: docker
  dockerfile: Dockerfile.dev
  context: ./sub
  target: dev
  args:
    NODE_VERSION: "22"
  volumes:
    - ./src:/app/src
  containerPort: 3000
  command: npm run dev
`
	var s Service
	if err := yaml.Unmarshal([]byte(src), &s); err != nil {
		t.Fatal(err)
	}
	r := s.Runner
	if r.Dockerfile != "Dockerfile.dev" || r.Target != "dev" ||
		r.Args["NODE_VERSION"] != "22" || len(r.Volumes) != 1 ||
		r.ContainerPort != 3000 || r.Command != "npm run dev" || r.Context != "./sub" {
		t.Fatalf("bad parse: %+v", r)
	}
}

func TestRunnerUnmarshalComposeFile(t *testing.T) {
	src := "runner:\n  name: docker\n  composeFile: ./docker-compose.dev.yml\n"
	var s Service
	if err := yaml.Unmarshal([]byte(src), &s); err != nil {
		t.Fatal(err)
	}
	if s.Runner.ComposeFile != "./docker-compose.dev.yml" {
		t.Fatalf("bad composeFile: %q", s.Runner.ComposeFile)
	}
}

func TestRunnerArgsAcceptUnquotedScalars(t *testing.T) {
	src := "runner:\n  name: docker\n  args:\n    NODE_VERSION: 22\n    DEBUG: true\n"
	var s Service
	if err := yaml.Unmarshal([]byte(src), &s); err != nil {
		t.Fatal(err)
	}
	if s.Runner.Args["NODE_VERSION"] != "22" || s.Runner.Args["DEBUG"] != "true" {
		t.Fatalf("scalar args must coerce to strings: %+v", s.Runner.Args)
	}
}

func TestDockerfileNameDefault(t *testing.T) {
	s := Service{}
	if s.DockerfileName() != "Dockerfile" {
		t.Fatal("default must be Dockerfile")
	}
	s.Runner.Dockerfile = "Dockerfile.dev"
	if s.DockerfileName() != "Dockerfile.dev" {
		t.Fatal("declared name must win")
	}
}

func TestRunnerImageImpliesDocker(t *testing.T) {
	var s Service
	if err := yaml.Unmarshal([]byte("runner:\n  image: nginx:alpine\n"), &s); err != nil {
		t.Fatal(err)
	}
	if !s.Runner.IsDocker() || s.Runner.Image != "nginx:alpine" {
		t.Fatalf("image alone must imply docker: %+v", s.Runner)
	}
}
