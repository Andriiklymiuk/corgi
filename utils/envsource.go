package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ActiveTierName / ActiveTierDir hold the tier selected by `corgi run --tier`
// (and `corgi env --tier`). Empty = no tier; env resolution stays on the
// default path. Mirrors the HostOverride global convention.
var (
	ActiveTierName string
	ActiveTierDir  string
)

// resolveEnvSourceFile picks the source env file for a service, in order:
// explicit copyEnvFromFilePath (with ${tier} substituted) if it exists → the
// active tier's convention file <tierDir>/<service>.env → the service repo's
// .env-example / .env.example. Returns "" when none exist. copyEnvFilePath
// overrides the service field (used by scripts). tierName/tierDir are empty
// when no tier is active, leaving resolution byte-for-byte the default path.
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
