package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ServiceConfig struct {
	Prefix       string
	EnvGenerator func(string, Service) string
}

var ServiceConfigs = map[string]ServiceConfig{
	"docker": {
		Prefix: "SERVICE_",
		EnvGenerator: func(serviceNameInEnv string, service Service) string {
			host := fmt.Sprintf("\n%sHOST=localhost", serviceNameInEnv)
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, service.Port)

			return fmt.Sprintf("%s%s", host, port)
		},
	},
}

func DockerSafeName(name string) string {
	lower := strings.ToLower(name)
	var b strings.Builder
	for _, r := range lower {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.TrimLeft(b.String(), "_-")
	if out == "" {
		return "service"
	}
	return out
}

func (s Service) DockerName() string {
	return ServiceContainerName(s.ServiceName)
}

func DockerRunnerServiceNames(services []Service) []string {
	var names []string
	for _, s := range services {
		if s.Runner.Name == "docker" {
			names = append(names, s.ServiceName)
		}
	}
	return names
}

func GetExposedPortFromDockerfile(service Service) (string, error) {
	if service.Port != 0 {
		return fmt.Sprintf("%d", service.Port), nil
	}
	dockerfilePath := filepath.Join(service.AbsolutePath, service.DockerfileName())
	content, err := os.ReadFile(dockerfilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("dockerfile not found in %s", service.AbsolutePath)
		}
		return "", fmt.Errorf("error reading Dockerfile: %w", err)
	}

	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.EqualFold(fields[0], "EXPOSE") {
			port, _, _ := strings.Cut(fields[1], "/")
			return port, nil
		}
	}

	return "", fmt.Errorf("no EXPOSE directive found in Dockerfile - container will not be accessible from outside")
}
