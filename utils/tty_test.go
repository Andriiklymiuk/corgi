package utils

import (
	"os"
	"testing"
)

func TestIsTTYWithPipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	// A pipe is not a character device, so it must report false.
	if fileIsTTY(r) {
		t.Errorf("pipe reported as TTY, want false")
	}
}

func TestFileIsTTY_StatErrorOnClosedFD(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	r.Close()
	// Stat on a closed fd errors, which must return false (not panic).
	if fileIsTTY(r) {
		t.Error("closed pipe reported as TTY")
	}
}

func TestIsTTY_CallableInTestEnv(t *testing.T) {
	// Under `go test` stdio is piped; the wrappers must run without panic.
	_ = IsTTY()
	_ = StdinIsTTY()
}
