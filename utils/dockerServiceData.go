package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// DockerServiceTemplateData feeds the generated docker-compose.yml/Makefile
// for a docker-mode service.
type DockerServiceTemplateData struct {
	DockerName      string
	Image           string // non-empty => run this registry image, no build
	BuildContext    string // absolute
	DockerfilePath  string // relative to BuildContext
	Target          string
	BuildArgs       map[string]string
	Port            int
	ContainerPort   int
	Volumes         []string // host side absolute; named volumes verbatim
	NamedVolumes    []string // docker named volumes needing a top-level block
	Command         string
	Watch           bool   // emit develop.watch (rebuild on context change)
	RepoComposeFile string // non-empty => delegate to the repo's compose file
	OverrideFile    string // env-injection overlay for repo-compose mode ("" = none)
	EnvFilePath     string // absolute path to the corgi env copy
}

func BuildDockerServiceData(s Service) (DockerServiceTemplateData, error) {
	d := DockerServiceTemplateData{
		DockerName: s.DockerName(),
		Port:       s.Port,
		Target:     s.Runner.Target,
		BuildArgs:  s.Runner.Args,
		Command:    s.Runner.Command,
		EnvFilePath: filepath.Join(
			CorgiComposePathDir, RootServicesFolder, s.ServiceName, ".env",
		),
	}

	if s.ResolvedDockerSource == SourceRepoCompose {
		err := fillRepoComposeData(&d, s)
		return d, err
	}

	if s.ResolvedDockerSource == SourceImage {
		fillImageData(&d, s)
		return d, nil
	}

	fillDockerfileBuildData(&d, s)
	return d, nil
}

func fillRepoComposeData(d *DockerServiceTemplateData, s Service) error {
	d.RepoComposeFile = RepoComposeFile(s)
	if d.RepoComposeFile == "" {
		return fmt.Errorf("service %s: compose file disappeared from %s", s.ServiceName, s.AbsolutePath)
	}
	if len(RepoComposeServiceNames(d.RepoComposeFile)) > 0 {
		d.OverrideFile = filepath.Join(
			CorgiComposePathDir, RootServicesFolder, s.ServiceName, "corgi.env.override.yml",
		)
	}
	return nil
}

func fillImageData(d *DockerServiceTemplateData, s Service) {
	d.Image = s.Runner.Image
	d.ContainerPort = s.Runner.ContainerPort
	if d.ContainerPort == 0 {
		d.ContainerPort = s.Port
	}
	appendRunnerVolumes(d, s)
}

func fillDockerfileBuildData(d *DockerServiceTemplateData, s Service) {
	d.Watch = s.Runner.Watch
	context := resolveBuildContext(s)
	d.BuildContext = context

	dockerfileAbs := filepath.Join(s.AbsolutePath, s.DockerfileName())
	rel, err := filepath.Rel(context, dockerfileAbs)
	if err != nil {
		rel = s.DockerfileName()
	}
	d.DockerfilePath = rel

	d.ContainerPort = s.Runner.ContainerPort
	if d.ContainerPort == 0 {
		d.ContainerPort = exposedPortFromDockerfile(s)
	}
	if d.ContainerPort == 0 {
		d.ContainerPort = s.Port
	}

	appendRunnerVolumes(d, s)
}

func resolveBuildContext(s Service) string {
	context := s.AbsolutePath
	if s.Runner.Context != "" {
		if filepath.IsAbs(s.Runner.Context) {
			context = s.Runner.Context
		} else {
			context = filepath.Join(s.AbsolutePath, s.Runner.Context)
		}
	}
	return context
}

func exposedPortFromDockerfile(s Service) int {
	exposed, err := GetExposedPortFromDockerfile(Service{
		AbsolutePath: s.AbsolutePath,
		Runner:       s.Runner,
	})
	if err != nil {
		return 0
	}
	port, convErr := strconv.Atoi(exposed)
	if convErr != nil {
		return 0
	}
	return port
}

func appendRunnerVolumes(d *DockerServiceTemplateData, s Service) {
	for _, v := range s.Runner.Volumes {
		spec, named := resolveVolumeSpec(s.AbsolutePath, v)
		d.Volumes = append(d.Volumes, spec)
		if named != "" {
			d.NamedVolumes = append(d.NamedVolumes, named)
		}
	}
}

// resolveVolumeSpec rewrites a relative host path in "host:container[:opts]"
// against the service dir. Anonymous volumes ("/path") and absolute binds pass
// through; a bare-name host ("mydata:/var/lib/x") is a docker named volume —
// returned as named so the compose file can declare it top-level.
func resolveVolumeSpec(base, spec string) (out, named string) {
	host, rest, found := splitVolumeSpec(spec)
	if !found || host == "" || filepath.IsAbs(host) {
		return spec, ""
	}
	if strings.HasPrefix(host, ".") || strings.ContainsAny(host, "/\\") {
		return filepath.Join(base, host) + ":" + rest, ""
	}
	return spec, host
}

func splitVolumeSpec(spec string) (host, rest string, found bool) {
	if i := strings.IndexByte(spec, ':'); i >= 0 {
		return spec[:i], spec[i+1:], true
	}
	return "", spec, false
}

// RepoComposeServiceNames lists the service keys of a repo's compose file.
// Empty on parse errors — env injection then simply stays off.
func RepoComposeServiceNames(composePath string) []string {
	raw, err := os.ReadFile(composePath)
	if err != nil {
		return nil
	}
	var doc struct {
		Services map[string]any `yaml:"services"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil
	}
	names := make([]string, 0, len(doc.Services))
	for name := range doc.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// RenderRepoComposeEnvOverride feeds corgi's env into every container of a
// repo compose file; repo-set values win.
func RenderRepoComposeEnvOverride(serviceNames []string, envFilePath string) string {
	var b strings.Builder
	b.WriteString("# 🐶 Generated by corgi — injects the corgi env into the repo's compose services.\n")
	b.WriteString("services:\n")
	for _, name := range serviceNames {
		b.WriteString("  " + name + ":\n")
		b.WriteString("    env_file:\n")
		b.WriteString("      - " + strconv.Quote(envFilePath) + "\n")
	}
	return b.String()
}
