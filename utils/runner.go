package utils

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// BuildArgs tolerates scalar YAML values of any type (NODE_VERSION: 22).
type BuildArgs map[string]string

func (b *BuildArgs) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("runner.args must be a mapping of build args")
	}
	out := map[string]string{}
	for i := 0; i+1 < len(value.Content); i += 2 {
		out[value.Content[i].Value] = value.Content[i+1].Value
	}
	*b = out
	return nil
}

// runnerAlias avoids UnmarshalYAML recursion.
type runnerAlias Runner

// UnmarshalYAML accepts both `runner: docker` and the full object form.
func (r *Runner) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		r.Name = value.Value
		return nil
	}
	var a runnerAlias
	if err := value.Decode(&a); err != nil {
		return err
	}
	*r = Runner(a)
	// runner: {image: nginx:alpine} alone means docker — spare the boilerplate.
	if r.Image != "" && r.Name == "" {
		r.Name = "docker"
	}
	return nil
}

func (r Runner) IsDocker() bool { return r.Name == "docker" }

func (s Service) DockerfileName() string {
	if s.Runner.Dockerfile != "" {
		return s.Runner.Dockerfile
	}
	return "Dockerfile"
}
