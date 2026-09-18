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
	if fileIsTTY(r) {
		t.Error("closed pipe reported as TTY")
	}
}

func TestIsTTY_CallableInTestEnv(t *testing.T) {
	_ = IsTTY()
	_ = StdinIsTTY()
}
