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

func stepCachePath(service Service, stepIndex int) string {
	return filepath.Join(CorgiComposePathDir, "corgi_services", cacheDirName,
		sanitizeName(service.ServiceName), strconv.Itoa(stepIndex))
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
	if readStepHash(stepCachePath(service, stepIndex)) == hash {
		return false, hash
	}
	return true, hash
}

// PersistStepHash records a step's hash so an unchanged future run can skip it.
func PersistStepHash(service Service, stepIndex int, hash string) {
	if hash == "" {
		return
	}
	_ = writeStepHash(stepCachePath(service, stepIndex), hash)
}
