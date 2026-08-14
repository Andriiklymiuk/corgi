package utils

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

const (
	storageFileName = "corgi_exec_paths.txt"
	fieldSeparator  = "|"
)

var (
	storageInitMu sync.Mutex
	// storageFilePath doubles as a test seam: when tests set it before any
	// SaveExecPath/ListExecPaths call, initializeStorage skips the
	// getDataPath() computation and uses the test-injected path.
	storageFilePath string
)

// storagePathChosenByTest reports whether the current registry path was picked
// by a test rather than derived from the user's real data directory. Comparing
// against the real path keeps this correct however a test sets the seam, and
// however many reads have already primed it.
func storagePathChosenByTest() bool {
	if storageFilePath == "" {
		return false
	}
	dir, err := getDataPath()
	if err != nil {
		return true // cannot locate the real registry, so nothing to pollute
	}
	return storageFilePath != filepath.Join(dir, storageFileName)
}

type CorgiExecPath struct {
	Name        string
	Description string
	Path        string
}

func ensureDBPathExists(path string) error {
	dir := filepath.Dir(path)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return os.MkdirAll(dir, os.ModePerm)
	}
	return nil
}

// CorgiDataDir is the per-user directory corgi keeps state in. Exported so
// agent mode can put its files alongside the existing registry.
func CorgiDataDir() (string, error) { return getDataPath() }

func getDataPath() (string, error) {
	// An explicit override wins everywhere, so an unusual install can point
	// corgi at its real data directory instead of silently starting fresh.
	if dir := strings.TrimSpace(os.Getenv("CORGI_DATA_DIR")); dir != "" {
		return dir, nil
	}
	switch runtime.GOOS {
	case "darwin":
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get user home directory: %w", err)
		}
		// Keep using the historical brew location when it already holds data,
		// so nobody loses their saved paths — but decide by looking at the
		// filesystem, never by running `brew`. corgi runs unattended under
		// launchd, whose PATH does not include brew: shelling out would give
		// the daemon and the shell two different data directories, and the
		// daemon would then read an empty registry.
		//
		// HOMEBREW_PREFIX covers a custom prefix, since `brew shellenv` exports
		// it; CORGI_DATA_DIR is the explicit escape hatch when neither applies.
		for _, prefix := range []string{os.Getenv("HOMEBREW_PREFIX"), "/opt/homebrew", "/usr/local"} {
			if prefix == "" {
				continue
			}
			legacy := filepath.Join(prefix, "var", "corgi")
			if info, statErr := os.Stat(legacy); statErr == nil && info.IsDir() {
				return legacy, nil
			}
		}
		return darwinFallbackDataDir(homeDir), nil
	case "linux":
		if xdgDataHome := os.Getenv("XDG_DATA_HOME"); xdgDataHome != "" {
			return filepath.Join(xdgDataHome, "corgi"), nil
		}
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get user home directory: %w", err)
		}
		return filepath.Join(homeDir, ".local", "share", "corgi"), nil
	case "windows":
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "corgi"), nil
		}
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get user home directory: %w", err)
		}
		return filepath.Join(homeDir, ".corgi"), nil
	default:
		return "", errors.New("unsupported operating system")
	}
}

// darwinFallbackDataDir is where corgi keeps state when no Homebrew var/corgi
// directory was found.
//
// A custom Homebrew prefix without HOMEBREW_PREFIX exported lands here, and the
// user's saved paths appear to vanish from `corgi list`. Say so once rather
// than leaving them to discover an empty registry, and name the fix.
func darwinFallbackDataDir(homeDir string) string {
	dir := filepath.Join(homeDir, "Library", "Application Support", "corgi")
	warnAboutDataDirFallbackOnce.Do(func() {
		if _, err := os.Stat(filepath.Join(dir, storageFileName)); err == nil {
			return // already the established location; nothing surprising
		}
		if !brewLooksInstalled() {
			return // no Homebrew at all, so nothing was moved
		}
		fmt.Fprintf(os.Stderr,
			"corgi: using %s for its data.\n"+
				"If your saved paths look missing, your Homebrew prefix is elsewhere — set:\n"+
				"  export CORGI_DATA_DIR=\"$(brew --prefix)/var/corgi\"\n", dir)
	})
	return dir
}

var warnAboutDataDirFallbackOnce sync.Once

// brewLooksInstalled checks for a Homebrew binary without running it, so the
// answer does not depend on PATH the way the daemon's does.
func brewLooksInstalled() bool {
	for _, p := range []string{"/opt/homebrew/bin/brew", "/usr/local/bin/brew"} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

func initializeStorage() error {
	if storageFilePath == "" {
		dir, err := getDataPath()
		if err != nil {
			return err
		}
		storageFilePath = filepath.Join(dir, storageFileName)
	}

	if err := ensureDBPathExists(storageFilePath); err != nil {
		return err
	}

	file, err := os.OpenFile(storageFilePath, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return fmt.Errorf("failed to open or create storage file: %w", err)
	}
	file.Close()

	return nil
}

func ensureStorageInitialized() error {
	storageInitMu.Lock()
	defer storageInitMu.Unlock()
	return initializeStorage()
}

// runningUnderTest reports whether this process is a `go test` binary.
//
// Every compose parse calls SaveExecPath, so without this guard corgi's own
// test suite writes its temp fixture directories into the user's real global
// registry — which then shows up in `corgi list` and, worse, in agent mode's
// workspace list.
func runningUnderTest() bool {
	return strings.HasSuffix(os.Args[0], ".test") ||
		strings.Contains(os.Args[0], "/_test/") ||
		os.Getenv("CORGI_DISABLE_EXEC_PATH_REGISTRY") == "1"
}

func SaveExecPath(name, description, path string) error {
	if runningUnderTest() && !storagePathChosenByTest() {
		// No test injected an explicit path, so this would hit the user's real
		// registry. Skip rather than pollute it. Keyed on the injection flag,
		// not on storageFilePath being empty: any earlier read primes that with
		// the real path, which would silently re-enable the writes.
		return nil
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to convert path to absolute: %w", err)
	}

	if err := ensureStorageInitialized(); err != nil {
		return err
	}

	execPaths, err := ListExecPaths()
	if err != nil {
		return err
	}

	updated := false
	for i, ep := range execPaths {
		if ep.Path == absolutePath {
			execPaths[i] = CorgiExecPath{Name: name, Description: description, Path: absolutePath}
			updated = true
			break
		}
	}
	if !updated {
		execPaths = append(execPaths, CorgiExecPath{Name: name, Description: description, Path: absolutePath})
	}

	return writeExecPaths(execPaths)
}

func writeExecPaths(execPaths []CorgiExecPath) error {
	file, err := os.Create(storageFilePath)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for _, ep := range execPaths {
		line := fmt.Sprintf(
			"%s%s%s%s%s\n",
			ep.Name,
			fieldSeparator,
			ep.Description,
			fieldSeparator,
			ep.Path,
		)
		if _, err := writer.WriteString(line); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func ListExecPaths() ([]CorgiExecPath, error) {
	if err := ensureStorageInitialized(); err != nil {
		return nil, err
	}
	file, err := os.Open(storageFilePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var execPaths []CorgiExecPath
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), fieldSeparator)
		if len(parts) < 3 {
			continue
		}
		execPaths = append(execPaths, CorgiExecPath{
			Name:        strings.TrimSpace(parts[0]),
			Description: strings.TrimSpace(parts[1]),
			Path:        strings.TrimSpace(parts[2]),
		})
	}
	return execPaths, scanner.Err()
}

func GetHomebrewBinPath() (string, error) {
	cmd := exec.Command("brew", "--prefix")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to execute brew --prefix: %w", err)
	}
	return fmt.Sprintf("%s/bin", strings.TrimSpace(string(output))), nil
}

func ClearExecPaths() error {
	if err := ensureStorageInitialized(); err != nil {
		return err
	}
	return os.Truncate(storageFilePath, 0)
}
