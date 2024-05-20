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

const storageFileName = "corgi_exec_paths.txt"

var storageInitOnce sync.Once
var storageInitError error
var storageFilePath string

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

func getDataPath() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		brewPath, err := GetHomebrewBinPath()
		if err != nil {
			return "", fmt.Errorf("failed to get Homebrew bin path: %w", err)
		}
		return filepath.Join(brewPath, "../var/corgi"), nil
	case "linux":
		if xdgDataHome := os.Getenv("XDG_DATA_HOME"); xdgDataHome != "" {
			return filepath.Join(xdgDataHome, "corgi"), nil
		}
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get user home directory: %w", err)
		}
		return filepath.Join(homeDir, ".local", "share", "corgi"), nil
	default:
		return "", errors.New("unsupported operating system")
	}
}

func initializeStorage() error {
	var err error
	storageFilePath, err = getDataPath()
	if err != nil {
		return err
	}
	storageFilePath = filepath.Join(storageFilePath, storageFileName)

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

func SaveExecPath(name, description, path string) error {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to convert path to absolute: %w", err)
	}

	storageInitOnce.Do(func() {
		storageInitError = initializeStorage()
	})
	if storageInitError != nil {
		return storageInitError
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
		line := fmt.Sprintf("%s, %s, %s\n", ep.Name, ep.Description, ep.Path)
		if _, err := writer.WriteString(line); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func ListExecPaths() ([]CorgiExecPath, error) {
	if err := initializeStorage(); err != nil {
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
		parts := strings.Split(scanner.Text(), ",")
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
	if err := initializeStorage(); err != nil {
		return err
	}
	return os.Truncate(storageFilePath, 0)
}
