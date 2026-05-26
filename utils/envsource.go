package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Tier selected by --tier; empty = default path. Like HostOverride.
var (
	EnvTierFromFlag string
	ActiveTierName  string
	ActiveTierDir   string
)

// Resolve --tier against the compose's envTiers: set the active tier globals
// and, unless the user passed --dbServices, apply the tier's db default.
func applyEnvTier(corgi *CorgiCompose) error {
	ActiveTierName, ActiveTierDir = "", ""
	if EnvTierFromFlag == "" {
		return nil
	}
	tier, ok := corgi.EnvTiers[EnvTierFromFlag]
	if !ok {
		return fmt.Errorf("unknown tier %q (declared envTiers: %s)", EnvTierFromFlag, strings.Join(tierNames(corgi.EnvTiers), ", "))
	}
	ActiveTierName, ActiveTierDir = EnvTierFromFlag, tier.Dir
	if tier.DbServices != "" && len(DbServicesItemsFromFlag) == 0 {
		DbServicesItemsFromFlag = strings.Split(tier.DbServices, ",")
		for i := range DbServicesItemsFromFlag {
			DbServicesItemsFromFlag[i] = strings.TrimSpace(DbServicesItemsFromFlag[i])
		}
	}
	return nil
}

func tierNames(tiers map[string]EnvTier) []string {
	names := make([]string, 0, len(tiers))
	for n := range tiers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

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
