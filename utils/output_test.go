package utils

import (
	"bytes"
	"sync"
	"testing"
)

func TestConsoleOverrideRedirectsInfo(t *testing.T) {
	var buf bytes.Buffer
	SetConsoleOverride(&buf)
	t.Cleanup(ClearConsoleOverride)

	Info("hello")
	if !bytes.Contains(buf.Bytes(), []byte("hello")) {
		t.Fatalf("override buffer did not receive Info output: %q", buf.String())
	}

	ClearConsoleOverride()
	if OverrideWriter() != nil {
		t.Fatal("expected OverrideWriter nil after ClearConsoleOverride")
	}
}

// lockedWriter is a goroutine-safe sink so the race test exercises the
// override's atomic swap rather than bytes.Buffer's lack of thread-safety.
type lockedWriter struct {
	mu sync.Mutex
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(p), nil
}

func TestConsoleOverrideRaceSafe(t *testing.T) {
	w := &lockedWriter{}
	SetConsoleOverride(w)
	t.Cleanup(ClearConsoleOverride)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				Info("x")
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 100; j++ {
			SetConsoleOverride(w)
			ClearConsoleOverride()
		}
	}()
	wg.Wait()
}
