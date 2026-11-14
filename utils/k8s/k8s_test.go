package k8s

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils"
	"gopkg.in/yaml.v3"
)

type fakeCluster struct {
	calls   []string
	stdins  map[int]string
	answers map[string]string
	fail    map[string]error
}

func (f *fakeCluster) exec() Exec {
	f.stdins = map[int]string{}
	return Exec{
		Run: func(_ context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
			call := name + " " + strings.Join(args, " ")
			f.calls = append(f.calls, call)
			if stdin != nil {
				f.stdins[len(f.calls)-1] = string(stdin)
			}
			for key, err := range f.fail {
				if strings.Contains(call, key) {
					return nil, err
				}
			}
			for key, out := range f.answers {
				if strings.Contains(call, key) {
					return []byte(out), nil
				}
			}
			return nil, nil
		},
		Stream: func(context.Context, io.Writer, string, ...string) error { return nil },
	}
}

func (f *fakeCluster) called(t *testing.T, want string) int {
	t.Helper()
	for i, c := range f.calls {
		if strings.Contains(c, want) {
			return i
		}
	}
	t.Fatalf("no call with %q in:\n%s", want, strings.Join(f.calls, "\n"))
	return -1
}

func (f *fakeCluster) never(t *testing.T, want string) {
	t.Helper()
	for _, c := range f.calls {
		if strings.Contains(c, want) {
			t.Fatalf("unexpected call %q", c)
		}
	}
}

func serviceDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestNamesFitKubernetes(t *testing.T) {
	for in, want := range map[string]string{"My API_v2": "my-api-v2", "--x--": "x", "": "corgi", strings.Repeat("a", 70): strings.Repeat("a", 63)} {
		if got := DNSName(in); got != want {
			t.Errorf("DNSName(%q) = %q", in, got)
		}
	}
	if DefaultNamespace("Shop Stack") != "corgi-shop-stack" {
		t.Fatal(DefaultNamespace("Shop Stack"))
	}
}

func TestOnlyLocalClustersAreLocal(t *testing.T) {
	for ctx, want := range map[string]ClusterKind{"kind-dev": KindCluster, "k3d-x": K3dCluster, "minikube": MinikubeCluster, "docker-desktop": SharedCluster, "orbstack": SharedCluster, "arn:aws:eks:eu-west-1:1:cluster/prod": RemoteCluster, "gke_prod": RemoteCluster} {
		if got := Classify(ctx).Kind; got != want {
			t.Errorf("%s: %s", ctx, got)
		}
	}
	if Classify("kind-dev").Name != "dev" || Classify("minikube").hostAlias() != "host.minikube.internal" || Classify("orbstack").loadImage("x") != nil {
		t.Fatal("names, host alias and the shared daemon")
	}
}

func TestEnvFilesParseAndPointAtTheHost(t *testing.T) {
	env := ParseEnv([]byte("# c\nexport A=1\nB=\"two words\"\nC='x'\nbad line\nD=postgres://localhost:5432/db\nE=http://127.0.0.1:9000\n"))
	if env["A"] != "1" || env["B"] != "two words" || env["C"] != "x" || len(env) != 5 {
		t.Fatalf("%v", env)
	}
	moved := withHost(env, "host.docker.internal")
	if moved["D"] != "postgres://host.docker.internal:5432/db" || moved["E"] != "http://host.docker.internal:9000" || env["D"] == moved["D"] {
		t.Fatalf("localhost becomes the host, the original stays: %v", moved)
	}
}

func TestASpecTakesTheContainerPortFromExpose(t *testing.T) {
	dir := serviceDir(t, map[string]string{"Dockerfile": "FROM x\nEXPOSE 8000/tcp\n", ".env": "K=v\n"})
	spec, err := FromService(utils.Service{ServiceName: "Web", AbsolutePath: dir, Port: 18000, Runner: utils.Runner{Name: "k8s"}}, "Demo")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Name != "web" || spec.Namespace != "corgi-demo" || spec.TargetPort != 8000 || spec.Port != 18000 || spec.Build == nil || spec.Env["K"] != "v" || spec.Source() != "generated" {
		t.Fatalf("%+v", spec)
	}
	manifests, err := FromService(utils.Service{ServiceName: "api", AbsolutePath: dir, Port: 3000, Runner: utils.Runner{Name: "k8s", Manifests: "deploy"}}, "demo")
	if err != nil || manifests.Build != nil || manifests.TargetPort != 3000 || manifests.Manifests != filepath.Join(dir, "deploy") {
		t.Fatalf("own manifests build nothing unless asked: %+v %v", manifests, err)
	}
	if _, err := FromService(utils.Service{ServiceName: "x", AbsolutePath: t.TempDir(), Runner: utils.Runner{Name: "k8s"}}, "d"); err == nil {
		t.Fatal("nothing to build, no image")
	}
}

func TestGeneratedManifestsAreValidAndLabelled(t *testing.T) {
	spec := Spec{Service: "web", Name: "web", Stack: "demo", Namespace: "corgi-demo", TargetPort: 8000, Command: "npm start"}
	docs := strings.Split(string(spec.GeneratedYAML("corgi/demo-web:abc")), "---\n")
	if len(docs) != 2 {
		t.Fatalf("a Deployment and a Service: %d", len(docs))
	}
	var dep map[string]any
	if err := yaml.Unmarshal([]byte(docs[0]), &dep); err != nil {
		t.Fatal(err)
	}
	c := dep["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)["containers"].([]any)[0].(map[string]any)
	if c["image"] != "corgi/demo-web:abc" || c["readinessProbe"] == nil || c["command"].([]any)[2] != "npm start" {
		t.Fatalf("%v", c)
	}
	if !strings.Contains(docs[1], "kind: Service") || !strings.Contains(docs[0], "app.kubernetes.io/managed-by: corgi") {
		t.Fatal(docs)
	}
	spec.TargetPort = 0
	if strings.Contains(string(spec.GeneratedYAML("i")), "kind: Service") {
		t.Fatal("no port, no Service")
	}
}

func TestUpBuildsLoadsAppliesAndWaitsOnKind(t *testing.T) {
	f := &fakeCluster{answers: map[string]string{"image inspect": "sha256:0123456789abcdef\n"}}
	spec := Spec{Service: "web", Name: "web", Stack: "demo", KubeContext: "kind-dev", Namespace: "corgi-demo", Port: 18000, TargetPort: 8000,
		Build: &Build{Context: "/src", Dockerfile: "/src/Dockerfile", Args: map[string]string{"B": "2", "A": "1"}}, Env: map[string]string{"DB": "localhost:5432"}}
	s := NewSession(spec, f.exec(), io.Discard)
	if err := s.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	ns := f.called(t, "kubectl --context kind-dev apply -f -")
	build := f.called(t, "docker build -t corgi/demo-web:dev -f /src/Dockerfile --build-arg A=1 --build-arg B=2 /src")
	load := f.called(t, "kind load docker-image corgi/demo-web:0123456789ab --name dev")
	wait := f.called(t, "rollout status deployment/web --timeout=300s -n corgi-demo")
	if !(ns < build && build < load && load < wait) {
		t.Fatalf("order: %v", f.calls)
	}
	var secret, deployment string
	for _, in := range f.stdins {
		if strings.Contains(in, "kind: Secret") {
			secret = in
		}
		if strings.Contains(in, "kind: Deployment") {
			deployment = in
		}
	}
	if !strings.Contains(secret, "host.docker.internal:5432") || !strings.Contains(deployment, "image: corgi/demo-web:0123456789ab") {
		t.Fatalf("secret %s\ndeployment %s", secret, deployment)
	}
	if s.forwardTarget() != "service/web" {
		t.Fatal(s.forwardTarget())
	}
}

func TestNoContextNamedNeverMeansARemoteCluster(t *testing.T) {
	f := &fakeCluster{answers: map[string]string{"current-context": "arn:aws:eks:eu-west-1:1:cluster/prod\n"}}
	s := NewSession(Spec{Service: "api", Name: "api", Namespace: "n"}, f.exec(), io.Discard)
	err := s.Up(context.Background())
	if err == nil || !strings.Contains(err.Error(), "runner.kubeContext") {
		t.Fatalf("refused, with the fix: %v", err)
	}
	f.never(t, "apply")

	f = &fakeCluster{}
	s = NewSession(Spec{Service: "api", Name: "api", Namespace: "n", KubeContext: "gke_prod", Build: &Build{}}, f.exec(), io.Discard)
	if err := s.Up(context.Background()); err == nil || !strings.Contains(err.Error(), "registry") {
		t.Fatalf("a named remote cluster cannot take a local build: %v", err)
	}
	f.never(t, "docker build")

	f = &fakeCluster{fail: map[string]error{"/readyz": errors.New("connection refused")}}
	s = NewSession(Spec{Service: "api", Name: "api", Namespace: "n", KubeContext: "kind-dev"}, f.exec(), io.Discard)
	if err := s.Up(context.Background()); err == nil || !strings.Contains(err.Error(), "kind create cluster --name dev") {
		t.Fatalf("a cluster that is down says how to start it: %v", err)
	}
}

func TestOwnManifestsPickTheWorkloadAndTakeTheBuiltImage(t *testing.T) {
	dir := serviceDir(t, map[string]string{"deploy/kustomization.yaml": "resources: []\n"})
	f := &fakeCluster{answers: map[string]string{
		"apply -k":      "service/api\ndeployment.apps/worker\ndeployment.apps/api\nconfigmap/x\n",
		"image inspect": "sha256:feedfacecafe1234",
	}}
	spec := Spec{Service: "api", Name: "api", Stack: "s", KubeContext: "docker-desktop", Namespace: "ns", Manifests: filepath.Join(dir, "deploy"), Port: 3000, TargetPort: 3000, Build: &Build{Context: dir, Dockerfile: dir + "/Dockerfile"}}
	s := NewSession(spec, f.exec(), io.Discard)
	if err := s.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.never(t, "kind load")
	f.called(t, "set image deployment/api *=corgi/s-api:feedfacecafe -n ns")
	f.called(t, "rollout status deployment/api")
	f.called(t, "rollout status deployment/worker")
	if s.forwardTarget() != "deployment/api" {
		t.Fatal(s.forwardTarget())
	}
	f.calls = nil
	if err := s.Down(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.called(t, "delete -k "+filepath.Join(dir, "deploy")+" -n ns --ignore-not-found --wait=false")
}

func TestAChartInstallsWithTheImageAndUninstalls(t *testing.T) {
	f := &fakeCluster{answers: map[string]string{"get deployment,statefulset,daemonset": "deployment.apps/api\n"}}
	spec := Spec{Service: "api", Name: "api", KubeContext: "kind-dev", Namespace: "ns", Chart: "/c", Values: []string{"/v.yaml"}, Image: "ghcr.io/acme/api:1.2", Port: 3000, TargetPort: 3000}
	s := NewSession(spec, f.exec(), io.Discard)
	if err := s.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.called(t, "helm upgrade --install api /c --kube-context kind-dev -n ns --wait --timeout 300s -f /v.yaml --set image.repository=ghcr.io/acme/api --set image.tag=1.2")
	f.called(t, "rollout status deployment/api")
	f.calls = nil
	_ = s.Down(context.Background())
	f.called(t, "helm uninstall api --kube-context kind-dev -n ns")
}

func TestPortForwardChatterIsDropped(t *testing.T) {
	var out bytes.Buffer
	w := &quietWriter{out: &out}
	_, _ = w.Write([]byte("Forwarding from 127.0.0.1:3000 -> 3000\nHandling conn"))
	_, _ = w.Write([]byte("ection for 3000\nreal log\n"))
	if out.String() != "Forwarding from 127.0.0.1:3000 -> 3000\nreal log\n" {
		t.Fatalf("%q", out.String())
	}
	if repo, tag := splitImage("localhost:5000/a/b:1"); repo != "localhost:5000/a/b" || tag != "1" {
		t.Fatal(repo, tag)
	}
	if _, tag := splitImage("localhost:5000/a/b"); tag != "latest" {
		t.Fatal(tag)
	}
}
