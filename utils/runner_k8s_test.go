package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestK8sRunnerParsesAndValidates(t *testing.T) {
	var s Service
	if err := yaml.Unmarshal([]byte("runner: kubernetes\n"), &s); err != nil || !s.Runner.IsK8s() {
		t.Fatalf("kubernetes is an alias: %+v %v", s.Runner, err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM x\n"), 0o644)
	cases := map[string]struct {
		runner Runner
		err    string
	}{
		"manifests":           {Runner{Name: "k8s", Manifests: "deploy"}, ""},
		"generated":           {Runner{Name: "k8s"}, ""},
		"image":               {Runner{Name: "k8s", Image: "nginx"}, ""},
		"both sources":        {Runner{Name: "k8s", Manifests: "deploy", Chart: "deploy"}, "mutually exclusive"},
		"values no chart":     {Runner{Name: "k8s", Values: []string{"v.yaml"}}, "helm chart"},
		"docker only field":   {Runner{Name: "k8s", Volumes: []string{"./x:/x"}}, "docker runner fields"},
		"missing manifests":   {Runner{Name: "k8s", Manifests: "nope"}, "not found"},
		"k8s field on docker": {Runner{Name: "docker", Namespace: "x"}, "name: k8s"},
	}
	for name, c := range cases {
		err := validateK8sRunner(Service{ServiceName: "api", AbsolutePath: dir, Runner: c.runner})
		if c.err == "" && err != nil || c.err != "" && (err == nil || !strings.Contains(err.Error(), c.err)) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := validateK8sRunner(Service{ServiceName: "api", AbsolutePath: t.TempDir(), Runner: Runner{Name: "k8s"}}); err == nil {
		t.Fatal("nothing to run")
	}
	resolved, err := ResolveRunnerModes([]Service{{ServiceName: "api", AbsolutePath: dir, Port: 3000, Runner: Runner{Name: "k8s"}}}, true, false)
	if err != nil || !resolved[0].Runner.IsK8s() || resolved[0].ResolvedDockerSource != SourceNone {
		t.Fatalf("--docker does not turn a k8s service into a docker one: %+v %v", resolved, err)
	}
}
