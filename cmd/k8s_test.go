package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils"
)

func TestDoctorNamesTheToolsAK8sServiceNeeds(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM x\nEXPOSE 80\n"), 0o644)
	corgi := &utils.CorgiCompose{Name: "demo", Services: []utils.Service{
		{ServiceName: "web", AbsolutePath: dir, Port: 8080, Runner: utils.Runner{Name: "k8s", KubeContext: "kind-dev"}},
		{ServiceName: "api", AbsolutePath: dir, Port: 3000, Runner: utils.Runner{Name: "k8s", KubeContext: "kind-dev", Chart: "chart", Image: "x"}},
		{ServiceName: "native", Start: []string{"go run ."}},
	}}
	old := k8sLookPath
	defer func() { k8sLookPath = old }()
	k8sLookPath = func(tool string) (string, error) {
		if tool == "kind" {
			return "", errors.New("missing")
		}
		return "/bin/" + tool, nil
	}
	var names []string
	var missing string
	for _, c := range k8sToolChecks(corgi) {
		names = append(names, c.Name)
		if !c.OK {
			missing = c.Name + " " + c.Detail
		}
	}
	if strings.Join(names, ",") != "k8s:docker,k8s:helm,k8s:kind,k8s:kubectl" || !strings.Contains(missing, "brew install kind") {
		t.Fatalf("%v %q", names, missing)
	}
	if len(k8sToolChecks(&utils.CorgiCompose{Services: []utils.Service{{ServiceName: "native"}}})) != 0 {
		t.Fatal("no k8s service, no k8s check")
	}
	svc := withK8sStart(corgi.Services[0])
	if len(svc.Start) != 1 || !strings.Contains(svc.Start[0], " k8s up web --filename ") {
		t.Fatalf("%v", svc.Start)
	}
	if got := withK8sStart(corgi.Services[2]); got.Start[0] != "go run ." {
		t.Fatal("a native service keeps its start")
	}
}
