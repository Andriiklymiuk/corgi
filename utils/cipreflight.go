package utils

import (
	"os"
	"path/filepath"
	"strings"
)

// Overridable so the container probe can be tested without one.
var (
	dockerEnvMarkerPath = "/.dockerenv"
	initCgroupPath      = "/proc/1/cgroup"
)

// InContainer reports whether this process is running inside a container.
//
// It matters because corgi's databases run as containers that publish to
// localhost, and every generated connection string assumes the services share
// that localhost. A CI job running inside its own container does not, so the
// stack boots and then every service fails to reach its database — which reads
// as "postgres is down" rather than "wrong runner".
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

// MissingEnvSource is a service whose declared env file is not on disk.
type MissingEnvSource struct {
	Service string `json:"service"`
	// Declared is copyEnvFromFilePath as written, with ${tier} substituted.
	Declared string `json:"declared"`
	// Fallback is what corgi would silently use instead — usually a committed
	// .env-example, whose placeholder values start the service and then fail at
	// the first request, far from the cause.
	Fallback string `json:"fallback,omitempty"`
}

// MissingEnvSources lists the services whose copyEnvFromFilePath does not
// resolve. Those files are almost always gitignored, so a CI runner has none of
// them — the most common reason a first pipeline never boots.
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
