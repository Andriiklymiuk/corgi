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

func GetExposedPortFromDockerfile(service Service) (string, error) {
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
