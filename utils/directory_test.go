package utils

import (
	"os"
	"path/filepath"
	"testing"
)

// Test for CheckIfFileExistsInDirectory function
func TestCheckIfFileExistsInDirectory(t *testing.T) {
	// Setup a temporary directory for the test
	dir := os.TempDir()

	// Create a temporary file in the temporary directory
	tmpfile, err := os.CreateTemp(dir, "testfile.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name()) // clean up

	// Get the name of the file created
	_, file := filepath.Split(tmpfile.Name())

	// Test CheckIfFileExistsInDirectory
	exists, err := CheckIfFileExistsInDirectory(dir, file)
	if err != nil {
		t.Fatalf("Failed to check file existence: %v", err)
	}
	if !exists {
		t.Fatalf("Expected the file to exist, but it doesn't")
	}
}

// Test for GetFileContent function
func TestGetFileContent(t *testing.T) {
	// Setup a temporary file for the test
	content := "Hello, world!"
	tmpfile, err := os.CreateTemp("", "testfile.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name()) // clean up

	// Write content to the file
	tmpfile.WriteString(content)
	tmpfile.Close()

	// Test GetFileContent
	result := GetFileContent(tmpfile.Name())
	if len(result) == 0 || result[0] != content {
		t.Fatalf("Expected to read '%v', but got '%v'", content, result)
	}
}
