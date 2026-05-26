package utils

import (
	"os"
	"path/filepath"
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

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
