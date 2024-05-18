package utils

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	_ "github.com/mattn/go-sqlite3"
)

const dbFile = "corgi_paths.db"

var DB *sql.DB

var DbInitOnce sync.Once
var DbInitErr error

// InitDBWrapper calls InitDB and stores the result, to be called via sync.Once
func InitDBWrapper() {
	DbInitOnce.Do(func() {
		DbInitErr = InitDB()
	})
}

// GetDBInitError provides access to the initialization error after InitDBWrapper has been called
func GetDBInitError() error {
	return DbInitErr
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

func InitDB() error {
	dataPath, err := getDataPath()
	if err != nil {
		return fmt.Errorf("data path error: %w", err)
	}

	dbPath := filepath.Join(dataPath, dbFile)
	if err := ensureDBPathExists(dbPath); err != nil {
		return fmt.Errorf("failed to ensure database directory exists: %w", err)
	}

	DB, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	createTableQuery := `
	CREATE TABLE IF NOT EXISTS executed_paths (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		path TEXT NOT NULL UNIQUE
	);`
	if _, err = DB.Exec(createTableQuery); err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	return nil
}

func SaveExecPath(path string) error {
	InitDBWrapper()
	if err := GetDBInitError(); err != nil {
		return fmt.Errorf("database initialization failed: %w", err)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to convert path to absolute: %w", err)
	}

	insertQuery := `INSERT OR IGNORE INTO executed_paths (path) VALUES (?);`
	_, err = DB.Exec(insertQuery, absPath)
	return err
}

func ListExecPaths() ([]string, error) {
	InitDBWrapper()
	if err := GetDBInitError(); err != nil {
		return nil, fmt.Errorf("database initialization failed: %w", err)
	}

	selectQuery := `SELECT path FROM executed_paths;`
	rows, err := DB.Query(selectQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, nil
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
	InitDBWrapper()
	if err := GetDBInitError(); err != nil {
		return fmt.Errorf("database initialization failed: %w", err)
	}

	deleteQuery := `DELETE FROM executed_paths;`
	if _, err := DB.Exec(deleteQuery); err != nil {
		return fmt.Errorf("failed to clear executed paths: %w", err)
	}

	return nil
}
