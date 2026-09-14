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
	return filepath.Join(CorgiServicesDir(), cacheDirName,
		stepCacheDirName(service), strconv.Itoa(stepIndex))
}

// stepOutputDir is the directory the step's install produces inside the
// service, inferred from its cacheKey lockfile. Empty when corgi does not
// know one (go.sum installs only into $HOME).
func stepOutputDir(service Service, step BeforeStartStep) string {
	for _, key := range step.CacheKey {
		if dirs := serviceOutputDirs(key); len(dirs) > 0 {
			return filepath.Join(service.AbsolutePath, dirs[0])
		}
	}
	return ""
}

// stepMarkerPath puts the marker inside the output directory it vouches for
// (node_modules/.corgi-step-0), so a CI cache restores the two together: an
// older node_modules brings its older marker, the hash mismatches and the
// install runs. Kept in corgi_services/.cache as a separate cache entry, the
// marker and the dependencies expire on different days and a fresh marker
// ends up next to stale packages. Steps without a known output directory
// stay central. A worktree has its own output directory, so no scope needed.
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

// writeOutputDirMarker writes a marker into an output directory that the
// step actually produced; inventing the directory for a step that installs
// elsewhere (pip without a venv) would leave an empty .venv behind.
func writeOutputDirMarker(path, hash string) error {
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		return nil
	}
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
	if readStepHash(stepMarkerPath(service, stepIndex, step)) == hash && stepOutputPresent(service, step) {
		return false, hash
	}
	return true, hash
}

// A marker only proves the step ran once, on some machine. A central marker
// (a step with no output directory of its own) can be restored by CI while
// the dependency directory is not — skipping the install then leaves nothing
// to run against. For in-directory markers this is a no-op: the marker's
// presence already proves the directory exists.
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
