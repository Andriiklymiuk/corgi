package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Tier selected by --tier; empty = default path. Like HostOverride.
var (
	ActiveTierName string
	ActiveTierDir  string
)

// Source env file: explicit copyEnvFromFilePath (${tier} substituted) →
// <tierDir>/<service>.env → repo .env-example/.env.example. "" if none.
func resolveEnvSourceFile(composeDir string, service Service, copyEnvFilePath, tierName, tierDir string) string {
	rel := resolveCopyEnvPath(service, copyEnvFilePath)
	if rel != "" {
		if tierName != "" {
			rel = strings.ReplaceAll(rel, "${tier}", tierName)
		}
		explicit := filepath.Join(composeDir, rel)
		if fileExists(explicit) {
			return explicit
		}
	}
	if tierName != "" && tierDir != "" {
		tierFile := filepath.Join(composeDir, tierDir, service.ServiceName+".env")
		if fileExists(tierFile) {
			return tierFile
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

// Warn if resolved env still contains a declared placeholder token. "" = none.
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
