package utils

import "gopkg.in/yaml.v3"

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
	return nil
}

func (r Runner) IsDocker() bool { return r.Name == "docker" }

func (s Service) DockerfileName() string {
	if s.Runner.Dockerfile != "" {
		return s.Runner.Dockerfile
	}
	return "Dockerfile"
}
