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
