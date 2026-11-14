package utils

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

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

type runnerAlias Runner

func (r *Runner) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		r.Name = value.Value
		if r.Name == "kubernetes" {
			r.Name = RunnerK8s
		}
		return nil
	}
	var a runnerAlias
	if err := value.Decode(&a); err != nil {
		return err
	}
	*r = Runner(a)
	if r.Name == "kubernetes" {
		r.Name = RunnerK8s
	}
	if r.Image != "" && r.Name == "" {
		r.Name = "docker"
	}
	return nil
}

const RunnerK8s = "k8s"

func (r Runner) IsDocker() bool { return r.Name == "docker" }

func (r Runner) IsK8s() bool { return r.Name == RunnerK8s }

func hasK8sFields(r Runner) bool {
	return r.KubeContext != "" || r.Namespace != "" || r.Manifests != "" || r.Chart != "" || len(r.Values) > 0 || r.Resource != ""
}

func validateK8sRunner(s Service) error {
	r := s.Runner
	if !r.IsK8s() {
		if hasK8sFields(r) {
			return fmt.Errorf("service %s: kubeContext, namespace, manifests, chart, values and resource need `runner: {name: k8s}`", s.ServiceName)
		}
		return nil
	}
	if r.Manifests != "" && r.Chart != "" {
		return fmt.Errorf("service %s: runner.manifests and runner.chart are mutually exclusive", s.ServiceName)
	}
	if len(r.Values) > 0 && r.Chart == "" {
		return fmt.Errorf("service %s: runner.values belong to a helm chart - add runner.chart", s.ServiceName)
	}
	if r.ComposeFile != "" || len(r.Volumes) > 0 || r.Watch {
		return fmt.Errorf("service %s: composeFile, volumes and watch are docker runner fields; on k8s put them in your manifests or chart", s.ServiceName)
	}
	if r.Image != "" && (r.Dockerfile != "" || r.Target != "" || len(r.Args) > 0) {
		return fmt.Errorf("service %s: runner.image and build fields (dockerfile/target/args) are mutually exclusive", s.ServiceName)
	}
	for field, rel := range map[string]string{"manifests": r.Manifests, "chart": r.Chart} {
		if _, err := os.Stat(resolveServicePath(s, rel)); rel != "" && err != nil {
			return fmt.Errorf("service %s: runner.%s %q not found in %s", s.ServiceName, field, rel, s.AbsolutePath)
		}
	}
	if r.Manifests == "" && r.Chart == "" && r.Image == "" && !dockerfileExists(s) {
		return fmt.Errorf("service %s: a k8s runner needs runner.manifests, runner.chart, runner.image or a %s to build", s.ServiceName, s.DockerfileName())
	}
	return nil
}

func resolveServicePath(s Service, rel string) string {
	if filepath.IsAbs(rel) {
		return rel
	}
	return filepath.Join(s.AbsolutePath, rel)
}

func (s Service) DockerfileName() string {
	if s.Runner.Dockerfile != "" {
		return s.Runner.Dockerfile
	}
	return "Dockerfile"
}
