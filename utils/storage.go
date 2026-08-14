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
	switch runtime.GOOS {
	case "darwin":
		// Prefer the historical brew location so nobody loses their saved paths
		// on upgrade, but never depend on brew: corgi runs unattended under
		// launchd, where a missing brew would leave the daemon with no state.
		if brewPath, err := GetHomebrewBinPath(); err == nil {
			return filepath.Join(brewPath, "../var/corgi"), nil
		}
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get user home directory: %w", err)
		}
		return filepath.Join(homeDir, "Library", "Application Support", "corgi"), nil
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
	if runningUnderTest() && storageFilePath == "" {
		// No explicit test path was injected, so this would hit the real
		// registry. Skip rather than pollute it.
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
