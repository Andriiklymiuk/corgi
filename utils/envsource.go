package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// resolveEnvSourceFile picks the source env file for a service: the explicit
// copyEnvFromFilePath when it exists, otherwise the service repo's .env-example
// / .env.example. Returns "" when none exist. copyEnvFilePath overrides the
// service field (used by scripts).
func resolveEnvSourceFile(composeDir string, service Service, copyEnvFilePath string) string {
	rel := resolveCopyEnvPath(service, copyEnvFilePath)
	if rel != "" {
		explicit := filepath.Join(composeDir, rel)
		if fileExists(explicit) {
			return explicit
		}
	}
	for _, name := range []string{".env-example", ".env.example"} {
		candidate := filepath.Join(service.AbsolutePath, name)
		if fileExists(candidate) {
			return candidate
		}
	}
	return ""
}

// placeholderWarning returns a warning when the resolved env still contains any
// of the service's declared placeholder tokens. Empty = nothing to warn.
func placeholderWarning(service Service, envBody string) string {
	var found []string
	for _, token := range service.EnvPlaceholdersToCheck {
		if token != "" && strings.Contains(envBody, token) {
			found = append(found, token)
		}
	}
	if len(found) == 0 {
		return ""
	}
	return fmt.Sprintf("⚠️  %s env still has placeholder(s): %s — replace with real values",
		service.ServiceName, strings.Join(found, ", "))
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
