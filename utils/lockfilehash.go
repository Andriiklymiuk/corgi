package utils

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func readCacheKeyFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil || filepath.Base(path) != "package-lock.json" {
		return data, err
	}
	return withoutRootVersion(data), nil
}

// A release bump changes the root version, never a dependency.
func withoutRootVersion(data []byte) []byte {
	var lock map[string]any
	if json.Unmarshal(data, &lock) != nil {
		return data
	}
	delete(lock, "version")
	if packages, ok := lock["packages"].(map[string]any); ok {
		if root, ok := packages[""].(map[string]any); ok {
			delete(root, "version")
		}
	}
	normalized, err := json.Marshal(lock)
	if err != nil {
		return data
	}
	return normalized
}
