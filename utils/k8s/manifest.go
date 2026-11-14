package k8s

import (
	"bytes"
	"sort"

	"gopkg.in/yaml.v3"
)

const (
	labelName      = "app.kubernetes.io/name"
	labelManagedBy = "app.kubernetes.io/managed-by"
	labelStack     = "corgi.dev/stack"
)

type meta struct {
	Name      string            `yaml:"name"`
	Namespace string            `yaml:"namespace,omitempty"`
	Labels    map[string]string `yaml:"labels,omitempty"`
}

type object struct {
	APIVersion string            `yaml:"apiVersion"`
	Kind       string            `yaml:"kind"`
	Metadata   meta              `yaml:"metadata"`
	Type       string            `yaml:"type,omitempty"`
	StringData map[string]string `yaml:"stringData,omitempty"`
	Spec       any               `yaml:"spec,omitempty"`
}

type containerPort struct {
	ContainerPort int `yaml:"containerPort"`
}

type probe struct {
	TCPSocket struct {
		Port int `yaml:"port"`
	} `yaml:"tcpSocket"`
	PeriodSeconds int `yaml:"periodSeconds"`
}

type container struct {
	Name            string           `yaml:"name"`
	Image           string           `yaml:"image"`
	ImagePullPolicy string           `yaml:"imagePullPolicy"`
	Command         []string         `yaml:"command,omitempty"`
	Ports           []containerPort  `yaml:"ports,omitempty"`
	EnvFrom         []map[string]any `yaml:"envFrom,omitempty"`
	ReadinessProbe  *probe           `yaml:"readinessProbe,omitempty"`
}

func (s Spec) labels() map[string]string {
	return map[string]string{labelName: s.Name, labelManagedBy: "corgi", labelStack: s.Stack}
}

func (s Spec) selector() map[string]string {
	return map[string]string{labelName: s.Name, labelStack: s.Stack}
}

func (s Spec) Selector() string {
	return labelName + "=" + s.Name + "," + labelStack + "=" + s.Stack
}

func (s Spec) envSecretName() string { return DNSName("corgi-" + s.Name + "-env") }

func (s Spec) objectMeta(name string) meta {
	return meta{Name: name, Namespace: s.Namespace, Labels: s.labels()}
}

func namespaceYAML(ns string) []byte {
	return encode(object{APIVersion: "v1", Kind: "Namespace", Metadata: meta{Name: ns, Labels: map[string]string{labelManagedBy: "corgi"}}})
}

func (s Spec) secretYAML(env map[string]string) []byte {
	return encode(object{APIVersion: "v1", Kind: "Secret", Metadata: s.objectMeta(s.envSecretName()), Type: "Opaque", StringData: env})
}

func (s Spec) GeneratedYAML(image string) []byte {
	c := container{
		Name:            s.Name,
		Image:           image,
		ImagePullPolicy: "IfNotPresent",
		EnvFrom:         []map[string]any{{"secretRef": map[string]any{"name": s.envSecretName(), "optional": true}}},
	}
	if s.Command != "" {
		c.Command = []string{"sh", "-c", s.Command}
	}
	if s.TargetPort != 0 {
		c.Ports = []containerPort{{ContainerPort: s.TargetPort}}
		p := &probe{PeriodSeconds: 2}
		p.TCPSocket.Port = s.TargetPort
		c.ReadinessProbe = p
	}
	deployment := object{APIVersion: "apps/v1", Kind: "Deployment", Metadata: s.objectMeta(s.Name), Spec: map[string]any{
		"replicas": 1,
		"selector": map[string]any{"matchLabels": s.selector()},
		"template": map[string]any{
			"metadata": map[string]any{"labels": s.labels()},
			"spec":     map[string]any{"containers": []container{c}},
		},
	}}
	out := encode(deployment)
	if s.TargetPort == 0 {
		return out
	}
	service := object{APIVersion: "v1", Kind: "Service", Metadata: s.objectMeta(s.Name), Spec: map[string]any{
		"selector": s.selector(),
		"ports":    []map[string]any{{"name": "http", "port": s.TargetPort, "targetPort": s.TargetPort}},
	}}
	return append(append(out, []byte("---\n")...), encode(service)...)
}

func encode(o object) []byte {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	_ = enc.Encode(o)
	_ = enc.Close()
	return buf.Bytes()
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
