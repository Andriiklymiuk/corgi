package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const cacheDirName = ".cache"

// Hash the contents of the cacheKey files (relative to baseDir). Missing files
// hash to a stable marker so a step still has a deterministic key.
func hashCacheKeyFiles(baseDir string, files []string) string {
	h := sha256.New()
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(baseDir, f))
		if err != nil {
			fmt.Fprintf(h, "%s\x00MISSING\n", f)
			continue
		}
		sum := sha256.Sum256(data)
		fmt.Fprintf(h, "%s\x00%x\n", f, sum)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// CacheScopeForDir derives a stable marker suffix for a relocated working dir.
// Hashed on the resolved path so two spellings of one directory share a scope.
func CacheScopeForDir(dir string) string {
	if resolved, ok := realPath(dir); ok {
		dir = resolved
	}
	sum := sha256.Sum256([]byte(dir))
	return hex.EncodeToString(sum[:])[:8]
}

func stepCacheDirName(service Service) string {
	name := sanitizeName(service.ServiceName)
	if service.CacheScope == "" {
		return name
	}
	return name + "-" + service.CacheScope
}

func stepCachePath(service Service, stepIndex int) string {
	return filepath.Join(CorgiComposePathDir, "corgi_services", cacheDirName,
		stepCacheDirName(service), strconv.Itoa(stepIndex))
}

func readStepHash(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func writeStepHash(path, hash string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	EnsureCorgiServicesIgnore(filepath.Join(CorgiComposePathDir, "corgi_services"), cacheDirName+"/")
	return os.WriteFile(path, []byte(hash), 0o644)
}

// StepNeedsRun reports whether a beforeStart step must run, plus the current
// cacheKey hash to persist after success. No cacheKey = always run. noCache forces run.
func StepNeedsRun(service Service, stepIndex int, step BeforeStartStep, noCache bool) (run bool, hash string) {
	if len(step.CacheKey) == 0 {
		return true, ""
	}
	hash = hashCacheKeyFiles(service.AbsolutePath, step.CacheKey)
	if noCache {
		return true, hash
	}
	if readStepHash(stepCachePath(service, stepIndex)) == hash && stepOutputPresent(service, step) {
		return false, hash
	}
	return true, hash
}

// A marker only proves the step ran once, on some machine. CI stores the
// markers and the dependency directories as separate cache entries that expire
// independently, so the marker can come back while node_modules does not —
// skipping the install then leaves nothing to run against.
func stepOutputPresent(service Service, step BeforeStartStep) bool {
	for _, key := range step.CacheKey {
		for _, dir := range serviceOutputDirs(key) {
			if _, err := os.Stat(filepath.Join(service.AbsolutePath, dir)); err != nil {
				return false
			}
		}
	}
	return true
}

// PersistStepHash records a step's hash so an unchanged future run can skip it.
func PersistStepHash(service Service, stepIndex int, hash string) {
	if hash == "" {
		return
	}
	_ = writeStepHash(stepCachePath(service, stepIndex), hash)
}
