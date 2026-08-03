package utils

import (
	"fmt"
	"path/filepath"
	"strconv"
)

// DockerServiceTemplateData feeds the generated docker-compose.yml/Makefile
// for a docker-mode service.
type DockerServiceTemplateData struct {
	DockerName      string
	BuildContext    string // absolute
	DockerfilePath  string // relative to BuildContext
	Target          string
	BuildArgs       map[string]string
	Port            int
	ContainerPort   int
	Volumes         []string // host side absolute
	Command         string
	RepoComposeFile string // non-empty => delegate to the repo's compose file
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
		d.RepoComposeFile = RepoComposeFile(s)
		if d.RepoComposeFile == "" {
			return d, fmt.Errorf("service %s: compose file disappeared from %s", s.ServiceName, s.AbsolutePath)
		}
		return d, nil
	}

	context := s.AbsolutePath
	if s.Runner.Context != "" {
		if filepath.IsAbs(s.Runner.Context) {
			context = s.Runner.Context
		} else {
			context = filepath.Join(s.AbsolutePath, s.Runner.Context)
		}
	}
	d.BuildContext = context

	dockerfileAbs := filepath.Join(s.AbsolutePath, s.DockerfileName())
	rel, err := filepath.Rel(context, dockerfileAbs)
	if err != nil {
		rel = s.DockerfileName()
	}
	d.DockerfilePath = rel

	d.ContainerPort = s.Runner.ContainerPort
	if d.ContainerPort == 0 {
		if exposed, err := GetExposedPortFromDockerfile(Service{
			AbsolutePath: s.AbsolutePath,
			Runner:       s.Runner,
		}); err == nil {
			if p, convErr := strconv.Atoi(exposed); convErr == nil {
				d.ContainerPort = p
			}
		}
	}
	if d.ContainerPort == 0 {
		d.ContainerPort = s.Port
	}

	for _, v := range s.Runner.Volumes {
		d.Volumes = append(d.Volumes, absoluteVolumeSpec(s.AbsolutePath, v))
	}
	return d, nil
}

// absoluteVolumeSpec rewrites a relative host path in "host:container[:opts]"
// against the service dir; anonymous volumes ("/path") pass through.
func absoluteVolumeSpec(base, spec string) string {
	host, rest, found := splitVolumeSpec(spec)
	if !found || host == "" || filepath.IsAbs(host) {
		return spec
	}
	return filepath.Join(base, host) + ":" + rest
}

func splitVolumeSpec(spec string) (host, rest string, found bool) {
	for i := 0; i < len(spec); i++ {
		if spec[i] == ':' {
			return spec[:i], spec[i+1:], true
		}
	}
	return "", spec, false
}
