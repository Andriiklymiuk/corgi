package k8s

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Exec struct {
	Run    func(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error)
	Stream func(ctx context.Context, out io.Writer, name string, args ...string) error
}

func System() Exec {
	return Exec{
		Run: func(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, name, args...)
			if stdin != nil {
				cmd.Stdin = bytes.NewReader(stdin)
			}
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			if err != nil {
				msg := strings.TrimSpace(stderr.String())
				if msg == "" {
					msg = err.Error()
				}
				return out, fmt.Errorf("%s %s: %s", name, firstArgs(args), msg)
			}
			return out, nil
		},
		Stream: func(ctx context.Context, out io.Writer, name string, args ...string) error {
			cmd := exec.CommandContext(ctx, name, args...)
			cmd.Stdout, cmd.Stderr = out, out
			return cmd.Run()
		},
	}
}

func firstArgs(args []string) string {
	if len(args) > 4 {
		args = args[:4]
	}
	return strings.Join(args, " ")
}

const (
	RolloutTimeout = 5 * time.Minute
	stopTimeout    = 90 * time.Second
)

type Session struct {
	Spec    Spec
	Exec    Exec
	Out     io.Writer
	Cluster Cluster
	image   string
	targets []string
}

func NewSession(spec Spec, x Exec, out io.Writer) *Session {
	if out == nil {
		out = os.Stdout
	}
	return &Session{Spec: spec, Exec: x, Out: out}
}

func (s *Session) say(format string, a ...any) {
	fmt.Fprintf(s.Out, "☸ %s: "+format+"\n", append([]any{s.Spec.Service}, a...)...)
}

func (s *Session) kubectl(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	full := append([]string{"--context", s.Spec.KubeContext}, args...)
	return s.Exec.Run(ctx, stdin, "kubectl", full...)
}

func (s *Session) inNamespace(args ...string) []string {
	return append(args, "-n", s.Spec.Namespace)
}

func (s *Session) ResolveContext(ctx context.Context) error {
	explicit := s.Spec.KubeContext != ""
	if !explicit {
		out, err := s.Exec.Run(ctx, nil, "kubectl", "config", "current-context")
		if err != nil {
			return fmt.Errorf("no kube context: set runner.kubeContext, or create a local cluster (kind create cluster): %w", err)
		}
		s.Spec.KubeContext = strings.TrimSpace(string(out))
	}
	s.Cluster = Classify(s.Spec.KubeContext)
	if !explicit && !s.Cluster.Local() {
		return fmt.Errorf("the current kube context %q is not a local cluster; corgi deploys to it only when named: runner.kubeContext: %s", s.Spec.KubeContext, s.Spec.KubeContext)
	}
	if s.Spec.EnvHost == "" {
		s.Spec.EnvHost = s.Cluster.hostAlias()
	}
	if _, err := s.kubectl(ctx, nil, "get", "--raw", "/readyz", "--request-timeout=10s"); err != nil {
		return fmt.Errorf("cluster %s does not answer (%s): %w", s.Spec.KubeContext, s.Cluster.startHint(), err)
	}
	return nil
}

func (s *Session) Up(ctx context.Context) error {
	if err := s.ResolveContext(ctx); err != nil {
		return err
	}
	s.say("%s in %s/%s", s.Spec.Source(), s.Spec.KubeContext, s.Spec.Namespace)
	if _, err := s.kubectl(ctx, namespaceYAML(s.Spec.Namespace), "apply", "-f", "-"); err != nil && !strings.Contains(err.Error(), "AlreadyExists") {
		return err
	}
	if err := s.prepareImage(ctx); err != nil {
		return err
	}
	if len(s.Spec.Env) > 0 {
		if _, err := s.kubectl(ctx, s.Spec.secretYAML(withHost(s.Spec.Env, s.Spec.EnvHost)), s.inNamespace("apply", "-f", "-")...); err != nil {
			return err
		}
	}
	if err := s.apply(ctx); err != nil {
		return err
	}
	return s.waitReady(ctx)
}

func (s *Session) prepareImage(ctx context.Context) error {
	s.image = s.Spec.Image
	if s.Spec.Build == nil {
		return nil
	}
	if !s.Cluster.Local() {
		return fmt.Errorf("%s is a remote cluster: a built image needs a registry - push it and set runner.image", s.Spec.KubeContext)
	}
	ref, err := s.build(ctx)
	if err != nil {
		return err
	}
	s.image = ref
	cmd := s.Cluster.loadImage(ref)
	if cmd == nil {
		return nil
	}
	s.say("loading %s into %s", ref, s.Cluster.Name)
	_, err = s.Exec.Run(ctx, nil, cmd[0], cmd[1:]...)
	return err
}

func (s *Session) build(ctx context.Context) (string, error) {
	b := s.Spec.Build
	repo := "corgi/" + DNSName(s.Spec.Stack+"-"+s.Spec.Name)
	args := []string{"build", "-t", repo + ":dev", "-f", b.Dockerfile}
	if b.Target != "" {
		args = append(args, "--target", b.Target)
	}
	for _, k := range sortedKeys(b.Args) {
		args = append(args, "--build-arg", k+"="+b.Args[k])
	}
	args = append(args, b.Context)
	s.say("building %s", filepath.Base(b.Dockerfile))
	if _, err := s.Exec.Run(ctx, nil, "docker", args...); err != nil {
		return "", err
	}
	id, err := s.Exec.Run(ctx, nil, "docker", "image", "inspect", "-f", "{{.Id}}", repo+":dev")
	if err != nil {
		return "", err
	}
	tag := strings.TrimPrefix(strings.TrimSpace(string(id)), "sha256:")
	if len(tag) > 12 {
		tag = tag[:12]
	}
	if tag == "" {
		return "", errors.New("docker gave no image id")
	}
	ref := repo + ":" + tag
	_, err = s.Exec.Run(ctx, nil, "docker", "tag", repo+":dev", ref)
	return ref, err
}

func (s *Session) apply(ctx context.Context) error {
	switch s.Spec.Source() {
	case "chart":
		return s.applyChart(ctx)
	case "manifests":
		return s.applyManifests(ctx)
	}
	if _, err := s.kubectl(ctx, s.Spec.GeneratedYAML(s.image), s.inNamespace("apply", "-f", "-")...); err != nil {
		return err
	}
	s.targets = []string{"deployment/" + s.Spec.Name}
	return nil
}

func (s *Session) manifestArgs(verb string) []string {
	if isKustomization(s.Spec.Manifests) {
		return s.inNamespace(verb, "-k", s.Spec.Manifests)
	}
	return s.inNamespace(verb, "-R", "-f", s.Spec.Manifests)
}

func isKustomization(path string) bool {
	for _, name := range []string{"kustomization.yaml", "kustomization.yml", "Kustomization"} {
		if _, err := os.Stat(filepath.Join(path, name)); err == nil {
			return true
		}
	}
	return false
}

func (s *Session) applyManifests(ctx context.Context) error {
	out, err := s.kubectl(ctx, nil, append(s.manifestArgs("apply"), "-o", "name")...)
	if err != nil {
		return err
	}
	s.targets = s.pickTargets(strings.Fields(string(out)))
	if s.image != "" && len(s.targets) > 0 {
		if _, err := s.kubectl(ctx, nil, s.inNamespace("set", "image", s.targets[0], "*="+s.image)...); err != nil {
			return fmt.Errorf("could not put the built image on %s: %w", s.targets[0], err)
		}
	}
	return nil
}

func (s *Session) applyChart(ctx context.Context) error {
	args := []string{"upgrade", "--install", s.Spec.Name, s.Spec.Chart, "--kube-context", s.Spec.KubeContext, "-n", s.Spec.Namespace,
		"--wait", "--timeout", strconv.Itoa(int(RolloutTimeout.Seconds())) + "s"}
	for _, v := range s.Spec.Values {
		args = append(args, "-f", v)
	}
	if s.image != "" {
		repo, tag := splitImage(s.image)
		args = append(args, "--set", "image.repository="+repo, "--set", "image.tag="+tag)
	}
	s.say("helm upgrade --install %s", s.Spec.Name)
	if _, err := s.Exec.Run(ctx, nil, "helm", args...); err != nil {
		return err
	}
	out, err := s.kubectl(ctx, nil, s.inNamespace("get", "deployment,statefulset,daemonset", "-l", "app.kubernetes.io/instance="+s.Spec.Name, "-o", "name")...)
	if err != nil {
		return err
	}
	s.targets = s.pickTargets(strings.Fields(string(out)))
	return nil
}

func splitImage(ref string) (repo, tag string) {
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		return ref[:i], ref[i+1:]
	}
	return ref, "latest"
}

func (s *Session) pickTargets(names []string) []string {
	if s.Spec.Resource != "" {
		return []string{s.Spec.Resource}
	}
	var workloads []string
	for _, n := range names {
		kind, name, ok := strings.Cut(n, "/")
		if !ok {
			continue
		}
		kind, _, _ = strings.Cut(kind, ".")
		switch kind {
		case "deployment", "statefulset", "daemonset":
			ref := kind + "/" + name
			if name == s.Spec.Name {
				workloads = append([]string{ref}, workloads...)
			} else {
				workloads = append(workloads, ref)
			}
		}
	}
	return workloads
}

func (s *Session) waitReady(ctx context.Context) error {
	if len(s.targets) == 0 {
		s.say("applied; no deployment, statefulset or daemonset to wait on")
		return nil
	}
	for _, t := range s.targets {
		_, err := s.kubectl(ctx, nil, s.inNamespace("rollout", "status", t, "--timeout="+strconv.Itoa(int(RolloutTimeout.Seconds()))+"s")...)
		if err != nil {
			s.explainFailure(ctx)
			return fmt.Errorf("%s is not ready: %w", t, err)
		}
	}
	s.say("%s ready", strings.Join(s.targets, ", "))
	return nil
}

func (s *Session) explainFailure(ctx context.Context) {
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	out, err := s.kubectl(cctx, nil, s.inNamespace("get", "events", "--sort-by=.lastTimestamp", "--field-selector", "type!=Normal")...)
	if err != nil || len(bytes.TrimSpace(out)) == 0 {
		return
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > 12 {
		lines = append(lines[:1], lines[len(lines)-11:]...)
	}
	s.say("what the cluster says:")
	for _, l := range lines {
		fmt.Fprintln(s.Out, "    "+l)
	}
}

func (s *Session) forwardTarget() string {
	if s.Spec.Source() == "generated" {
		return "service/" + s.Spec.Name
	}
	if s.Spec.Resource != "" {
		return s.Spec.Resource
	}
	if len(s.targets) > 0 {
		return s.targets[0]
	}
	return ""
}

func (s *Session) Serve(ctx context.Context) {
	done := make(chan struct{}, 2)
	go func() { s.supervise(ctx, "logs", s.logsArgs()); done <- struct{}{} }()
	target := s.forwardTarget()
	if s.Spec.Port != 0 && target != "" {
		s.say("localhost:%d → %s:%d", s.Spec.Port, target, s.Spec.TargetPort)
		go func() {
			s.supervise(ctx, "port-forward", s.inNamespace("port-forward", "--address", "127.0.0.1", target, fmt.Sprintf("%d:%d", s.Spec.Port, s.Spec.TargetPort)))
			done <- struct{}{}
		}()
	} else {
		done <- struct{}{}
	}
	<-done
	<-done
}

func (s *Session) logsArgs() []string {
	if s.Spec.Source() == "generated" {
		return s.inNamespace("logs", "-f", "-l", s.Spec.Selector(), "--all-containers", "--prefix", "--max-log-requests", "10", "--since", "10s")
	}
	if len(s.targets) == 0 {
		return nil
	}
	return s.inNamespace("logs", "-f", s.targets[0], "--all-containers", "--prefix", "--since", "10s")
}

func (s *Session) supervise(ctx context.Context, what string, args []string) {
	if args == nil {
		return
	}
	w := &quietWriter{out: s.Out}
	backoff := time.Second
	for ctx.Err() == nil {
		started := time.Now()
		err := s.Exec.Stream(ctx, w, "kubectl", append([]string{"--context", s.Spec.KubeContext}, args...)...)
		if ctx.Err() != nil {
			return
		}
		if time.Since(started) > 30*time.Second {
			backoff = time.Second
		}
		if what == "port-forward" {
			s.say("port-forward dropped (%v), again in %s", errOrEnd(err), backoff)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 10*time.Second {
			backoff *= 2
		}
	}
}

func errOrEnd(err error) string {
	if err == nil {
		return "ended"
	}
	return err.Error()
}

type quietWriter struct {
	out     io.Writer
	partial []byte
}

func (w *quietWriter) Write(p []byte) (int, error) {
	w.partial = append(w.partial, p...)
	for {
		i := bytes.IndexByte(w.partial, '\n')
		if i < 0 {
			return len(p), nil
		}
		line := w.partial[:i+1]
		w.partial = w.partial[i+1:]
		if bytes.HasPrefix(line, []byte("Handling connection for")) {
			continue
		}
		if _, err := w.out.Write(line); err != nil {
			return len(p), err
		}
	}
}

func (s *Session) Down(ctx context.Context) error {
	if s.Spec.KubeContext == "" || s.Cluster.Kind == "" {
		if err := s.ResolveContext(ctx); err != nil {
			return err
		}
	}
	var errs []error
	switch s.Spec.Source() {
	case "chart":
		if _, err := s.Exec.Run(ctx, nil, "helm", "uninstall", s.Spec.Name, "--kube-context", s.Spec.KubeContext, "-n", s.Spec.Namespace, "--ignore-not-found"); err != nil {
			errs = append(errs, err)
		}
	case "manifests":
		if _, err := s.kubectl(ctx, nil, append(s.manifestArgs("delete"), "--ignore-not-found", "--wait=false")...); err != nil {
			errs = append(errs, err)
		}
	}
	if _, err := s.kubectl(ctx, nil, s.inNamespace("delete", "deployment,service,secret", "-l", s.Spec.Selector()+","+labelManagedBy+"=corgi", "--ignore-not-found", "--wait=false")...); err != nil {
		errs = append(errs, err)
	}
	if len(errs) == 0 {
		s.say("removed from %s/%s", s.Spec.KubeContext, s.Spec.Namespace)
	}
	return errors.Join(errs...)
}

func StopContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), stopTimeout)
}
