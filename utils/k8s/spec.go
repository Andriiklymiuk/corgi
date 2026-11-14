package k8s

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"andriiklymiuk/corgi/utils"
)

type Build struct {
	Context    string
	Dockerfile string
	Target     string
	Args       map[string]string
}

type Spec struct {
	Service     string
	Name        string
	Stack       string
	KubeContext string
	Namespace   string
	Manifests   string
	Chart       string
	Values      []string
	Resource    string
	Port        int
	TargetPort  int
	Image       string
	Build       *Build
	Command     string
	Env         map[string]string
	EnvHost     string
}

func (s Spec) Source() string {
	switch {
	case s.Chart != "":
		return "chart"
	case s.Manifests != "":
		return "manifests"
	default:
		return "generated"
	}
}

func DNSName(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(raw) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	if len(out) > 63 {
		out = strings.TrimRight(out[:63], "-")
	}
	if out == "" {
		return "corgi"
	}
	return out
}

func DefaultNamespace(stack string) string {
	return DNSName("corgi-" + stack)
}

func FromService(s utils.Service, stack string) (Spec, error) {
	r := s.Runner
	spec := Spec{
		Service:     s.ServiceName,
		Name:        DNSName(s.ServiceName),
		Stack:       DNSName(stack),
		KubeContext: r.KubeContext,
		Namespace:   r.Namespace,
		Resource:    r.Resource,
		Port:        s.Port,
		TargetPort:  r.ContainerPort,
		Image:       r.Image,
		Command:     r.Command,
		EnvHost:     s.LocalhostNameInEnv,
	}
	if spec.Namespace == "" {
		spec.Namespace = DefaultNamespace(stack)
	}
	if r.Manifests != "" {
		spec.Manifests = servicePath(s, r.Manifests)
	}
	if r.Chart != "" {
		spec.Chart = servicePath(s, r.Chart)
		for _, v := range r.Values {
			spec.Values = append(spec.Values, servicePath(s, v))
		}
	}
	declaredBuild := r.Dockerfile != "" || r.Context != "" || r.Target != "" || len(r.Args) > 0
	generated := spec.Manifests == "" && spec.Chart == ""
	if spec.Image == "" && (declaredBuild || generated) {
		dockerfile := servicePath(s, s.DockerfileName())
		if _, err := os.Stat(dockerfile); err != nil {
			return spec, fmt.Errorf("service %s: no %s to build an image from, and no runner.image", s.ServiceName, s.DockerfileName())
		}
		context := s.AbsolutePath
		if r.Context != "" {
			context = servicePath(s, r.Context)
		}
		spec.Build = &Build{Context: context, Dockerfile: dockerfile, Target: r.Target, Args: r.Args}
		if spec.TargetPort == 0 {
			spec.TargetPort = exposedPort(s)
		}
	}
	if spec.TargetPort == 0 {
		spec.TargetPort = s.Port
	}
	env, err := readServiceEnv(utils.GetPathToEnv(s))
	if err != nil {
		return spec, fmt.Errorf("service %s: %w", s.ServiceName, err)
	}
	spec.Env = env
	return spec, nil
}

func exposedPort(s utils.Service) int {
	exposed, err := utils.GetExposedPortFromDockerfile(utils.Service{AbsolutePath: s.AbsolutePath, Runner: s.Runner})
	if err != nil {
		return 0
	}
	port, err := strconv.Atoi(exposed)
	if err != nil {
		return 0
	}
	return port
}

func servicePath(s utils.Service, rel string) string {
	if filepath.IsAbs(rel) {
		return rel
	}
	return filepath.Join(s.AbsolutePath, rel)
}

func readServiceEnv(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ParseEnv(raw), nil
}

func ParseEnv(raw []byte) map[string]string {
	env := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		env[key] = value
	}
	return env
}

func withHost(env map[string]string, host string) map[string]string {
	if len(env) == 0 || host == "" {
		return env
	}
	out := make(map[string]string, len(env))
	for k, v := range env {
		v = strings.ReplaceAll(v, "localhost", host)
		out[k] = strings.ReplaceAll(v, "127.0.0.1", host)
	}
	return out
}
