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
	return filepath.Join(CorgiServicesDir(), cacheDirName,
		stepCacheDirName(service), strconv.Itoa(stepIndex))
}

func stepOutputDir(service Service, step BeforeStartStep) string {
	for _, key := range step.CacheKey {
		if dirs := serviceOutputDirs(key); len(dirs) > 0 {
			return filepath.Join(service.AbsolutePath, dirs[0])
		}
	}
	return ""
}

func stepMarkerPath(service Service, stepIndex int, step BeforeStartStep) string {
	if dir := stepOutputDir(service, step); dir != "" {
		return filepath.Join(dir, ".corgi-step-"+strconv.Itoa(stepIndex))
	}
	return stepCachePath(service, stepIndex)
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
	EnsureCorgiServicesIgnore(CorgiServicesDir(), cacheDirName+"/")
	return os.WriteFile(path, []byte(hash), 0o644)
}

func writeOutputDirMarker(path, hash string) error {
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		return nil
	}
	return os.WriteFile(path, []byte(hash), 0o644)
}

func StepNeedsRun(service Service, stepIndex int, step BeforeStartStep, noCache bool) (run bool, hash string) {
	if len(step.CacheKey) == 0 {
		return true, ""
	}
	hash = hashCacheKeyFiles(service.AbsolutePath, step.CacheKey)
	if noCache {
		return true, hash
	}
	if readStepHash(stepMarkerPath(service, stepIndex, step)) == hash && stepOutputPresent(service, step) {
		return false, hash
	}
	return true, hash
}

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

func PersistStepHash(service Service, stepIndex int, step BeforeStartStep, hash string) {
	if hash == "" {
		return
	}
	if stepOutputDir(service, step) != "" {
		_ = writeOutputDirMarker(stepMarkerPath(service, stepIndex, step), hash)
		return
	}
	_ = writeStepHash(stepCachePath(service, stepIndex), hash)
}
