package utils

import (
	"os"
	"path/filepath"
	"strings"
)

var (
	dockerEnvMarkerPath = "/.dockerenv"
	initCgroupPath      = "/proc/1/cgroup"
)

func InContainer() bool {
	if _, err := os.Stat(dockerEnvMarkerPath); err == nil {
		return true
	}
	data, err := os.ReadFile(initCgroupPath)
	if err != nil {
		return false
	}
	cgroup := string(data)
	for _, marker := range []string{"docker", "containerd", "kubepods"} {
		if strings.Contains(cgroup, marker) {
			return true
		}
	}
	return false
}

type MissingEnvSource struct {
	Service  string `json:"service"`
	Declared string `json:"declared"`
	Fallback string `json:"fallback,omitempty"`
}

func MissingEnvSources(corgi *CorgiCompose) []MissingEnvSource {
	var missing []MissingEnvSource
	for _, service := range sortedServices(corgi) {
		declared := service.CopyEnvFromFilePath
		if declared == "" {
			continue
		}
		if ActiveTierName != "" {
			declared = strings.ReplaceAll(declared, "${tier}", ActiveTierName)
		}
		if fileExists(filepath.Join(CorgiComposePathDir, declared)) {
			continue
		}
		missing = append(missing, MissingEnvSource{
			Service:  service.ServiceName,
			Declared: declared,
			Fallback: resolveEnvSourceFile(CorgiComposePathDir, service, "", ActiveTierName, ActiveTierDir),
		})
	}
	return missing
}
