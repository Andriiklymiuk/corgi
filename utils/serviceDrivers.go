package utils

import (
	"andriiklymiuk/corgi/templates"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ServiceConfig struct {
	Prefix        string
	EnvGenerator  func(string, Service) string
	FilesToCreate []FilenameForService
}

var ServiceConfigs = map[string]ServiceConfig{
	"docker": {
		Prefix: "SERVICE_",
		EnvGenerator: func(serviceNameInEnv string, service Service) string {
			host := fmt.Sprintf("\n%sHOST=localhost", serviceNameInEnv)
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, service.Port)

			return fmt.Sprintf("%s%s", host, port)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeService},
			{"Makefile", templates.MakefileService},
		},
	},
}

// DockerSafeName converts a service name to a docker-compose-safe name:
// lowercased, chars outside [a-z0-9_-] become '-', leading separators trimmed.
// docker compose lowercases the project name, so without this an uppercase name
// (e.g. "MyApi") yields a container_name docker rejects.
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

// DockerName is the docker-safe service name for generated compose/Makefile templates.
func (s Service) DockerName() string {
	return DockerSafeName(s.ServiceName)
}

// DockerRunnerServiceNames returns the names of docker-runner services. They run
// as containers (not tracked PIDs), so must be brought down explicitly.
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
		// If the port is already specified in the service struct, return it directly
		// This is because the port is already specified in the service struct
		// and we don't need to check the Dockerfile for it
		return fmt.Sprintf("%d", service.Port), nil
	}
	dockerfileExists, err := CheckIfFileExistsInDirectory(
		service.AbsolutePath,
		"Dockerfile",
	)
	if err != nil {
		return "", fmt.Errorf("error checking for Dockerfile: %w", err)
	}
	if !dockerfileExists {
		return "", fmt.Errorf("dockerfile not found in %s", service.AbsolutePath)
	}

	dockerfilePath := filepath.Join(service.AbsolutePath, "Dockerfile")
	content, err := os.ReadFile(dockerfilePath)
	if err != nil {
		return "", fmt.Errorf("error reading Dockerfile: %w", err)
	}

	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "EXPOSE") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return parts[1], nil
			}
		}
	}

	return "", fmt.Errorf("no EXPOSE directive found in Dockerfile - container will not be accessible from outside")
}
