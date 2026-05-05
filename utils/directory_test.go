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


func TestGetFoldersListInDirectory(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })

	dbDir := filepath.Join(CorgiComposePathDir, RootDbServicesFolder)
	if err := os.MkdirAll(filepath.Join(dbDir, "db1"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dbDir, "db2"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dbDir, "ignored.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dbDir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	got, err := GetFoldersListInDirectory()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("got %v, want 2", got)
	}
	for _, name := range got {
		if name == ".git" {
			t.Errorf(".git should not be listed")
		}
	}
}

func TestGetFoldersListInDirectoryMissing(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = "/nonexistent-zzzz"
	t.Cleanup(func() { CorgiComposePathDir = prev })

	_, err := GetFoldersListInDirectory()
	if err == nil {
		t.Error("expected err")
	}
}

func TestCheckIfFileExistsInDirectoryNot(t *testing.T) {
	dir := t.TempDir()
	got, _ := CheckIfFileExistsInDirectory(dir, "missing.txt")
	if got {
		t.Error("expected false")
	}
}

func TestCheckIfFileExistsInDirectoryMissingDir(t *testing.T) {
	_, err := CheckIfFileExistsInDirectory("/no/such/dir", "x")
	if err == nil {
		t.Error("expected err")
	}
}

func TestCheckIfFilesExistsInDirectoryGlob(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dump.sql"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := CheckIfFilesExistsInDirectory(dir, "dump.*")
	if err != nil || !got {
		t.Errorf("got %v err %v", got, err)
	}
	got, _ = CheckIfFilesExistsInDirectory(dir, "nope.*")
	if got {
		t.Error("expected false")
	}
}

func TestGetFileContentMultiline(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.env")
	if err := os.WriteFile(p, []byte("a=1\nb=2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := GetFileContent(p)
	if len(got) != 2 || got[0] != "a=1" || got[1] != "b=2" {
		t.Errorf("got %v", got)
	}
}

func TestGetFileContentMissing(t *testing.T) {
	got := GetFileContent("/no/such/path/x.txt")
	_ = got
}
