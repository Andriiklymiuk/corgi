package supervisor

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeClaude puts a stand-in `claude` on PATH so the process layer can be
// exercised without a real binary, a subscription, or a network.
func fakeClaude(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in binary is a shell script")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	body := "#!/bin/sh\n" + script + "\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func execConfig(t *testing.T) SpawnConfig {
	return SpawnConfig{WorkspaceID: "acme", Dir: t.TempDir(), WakeLock: WakeLockOff}
}

func TestStartProcessRejectsABinaryPath(t *testing.T) {
	cfg := execConfig(t)
	cfg.Bin = "/tmp/evil.sh"

	_, err := StartProcess(context.Background(), cfg)

	if err == nil {
		t.Fatal("a bin containing a path must be rejected — otherwise a config file chooses which program the daemon runs")
	}
	if !strings.Contains(err.Error(), "not a path") {
		t.Errorf("error should explain the rule, got %q", err)
	}
}

func TestSanitizeBin(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		// Empty stays empty here; ResolveBin fills it from the kind, so the
		// default lives in one place rather than two.
		{"", "", false},
		{"claude", "claude", false},
		{"  claude  ", "claude", false},
		{"claude-alt", "claude-alt", false},
		{"/usr/local/bin/claude", "", true},
		{"./evil.sh", "", true},
		{"../../evil", "", true},
		{`C:\evil.exe`, "", true},
		{"-rf", "", true},
	}

	for _, tt := range tests {
		got, err := SanitizeBin(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("SanitizeBin(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && got != tt.want {
			t.Errorf("SanitizeBin(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestStartProcessReportsAMissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	cfg := execConfig(t)

	_, err := StartProcess(context.Background(), cfg)

	if err == nil {
		t.Fatal("a missing binary must fail at start, not silently do nothing")
	}
	if !strings.Contains(err.Error(), "not found on PATH") {
		t.Errorf("error should name the problem, got %q", err)
	}
}

func TestProcessCapturesOutputForClassification(t *testing.T) {
	fakeClaude(t, `echo "Remote Control requires a claude.ai subscription" >&2; exit 1`)

	p, err := StartProcess(context.Background(), execConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	code, output := p.Wait()

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(output, "claude.ai subscription") {
		t.Fatalf("output %q must reach the classifier, or an auth failure would be retried forever", output)
	}
	if Classify(Exit{Code: code, Output: output}, 0) != CauseAuthFailure {
		t.Error("the captured output should classify as an auth failure end to end")
	}
}

func TestProcessStopTerminatesIt(t *testing.T) {
	fakeClaude(t, `trap 'exit 0' TERM INT; while true; do sleep 0.05; done`)

	p, err := StartProcess(context.Background(), execConfig(t))
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() { defer close(done); p.Wait() }()

	p.Stop()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("Stop() did not terminate the process — shutdown would hang")
	}
}

func TestProcessStopIsIdempotent(t *testing.T) {
	fakeClaude(t, `trap 'exit 0' TERM INT; while true; do sleep 0.05; done`)

	p, err := StartProcess(context.Background(), execConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); p.Wait() }()

	p.Stop()
	p.Stop()
	p.Stop()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("repeated Stop() calls must not deadlock")
	}
}

func TestCancellingContextStopsTheProcess(t *testing.T) {
	fakeClaude(t, `trap 'exit 0' TERM INT; while true; do sleep 0.05; done`)

	ctx, cancel := context.WithCancel(context.Background())
	p, err := StartProcess(ctx, execConfig(t))
	if err != nil {
		cancel()
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() { defer close(done); p.Wait() }()
	cancel()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("cancelling the supervisor's context must not leave an orphan holding the workspace")
	}
}

// The watcher goroutine must end when the process ends, not only when the
// context is cancelled — otherwise every restart leaks one for the daemon's
// lifetime, and this daemon is meant to run for weeks.
func TestNoGoroutineLeakAcrossManyRestarts(t *testing.T) {
	fakeClaude(t, `exit 0`)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	settle := func() int {
		for range 20 {
			runtime.GC()
			time.Sleep(20 * time.Millisecond)
		}
		return runtime.NumGoroutine()
	}

	// Warm up so one-off runtime goroutines are not counted as growth.
	for range 3 {
		p, err := StartProcess(ctx, execConfig(t))
		if err != nil {
			t.Fatal(err)
		}
		p.Wait()
	}
	before := settle()

	for range 25 {
		p, err := StartProcess(ctx, execConfig(t))
		if err != nil {
			t.Fatal(err)
		}
		p.Wait()
	}
	after := settle()

	if after > before+5 {
		t.Errorf("goroutines grew from %d to %d across 25 restarts — the process watcher is leaking, and this daemon runs for weeks", before, after)
	}
}

func TestRingBufferKeepsOnlyTheTail(t *testing.T) {
	r := newRingBuffer(16)

	r.Write([]byte(strings.Repeat("a", 100)))
	r.Write([]byte("TAIL"))

	got := r.String()
	if len(got) != 16 {
		t.Errorf("buffer length = %d, want it capped at 16 — an unbounded buffer is a slow leak in a process that runs for days", len(got))
	}
	if !strings.HasSuffix(got, "TAIL") {
		t.Errorf("buffer = %q, want the most recent bytes kept", got)
	}
}

func TestRingBufferHandlesSmallWrites(t *testing.T) {
	r := newRingBuffer(1024)
	r.Write([]byte("hello "))
	r.Write([]byte("world"))

	if got := r.String(); got != "hello world" {
		t.Errorf("buffer = %q, want %q", got, "hello world")
	}
}

func TestRingBufferIsSafeForConcurrentWriters(t *testing.T) {
	// stdout and stderr are both wired to this buffer, so two writers is real.
	r := newRingBuffer(4096)
	done := make(chan struct{})

	for range 4 {
		go func() {
			defer func() { done <- struct{}{} }()
			for range 100 {
				r.Write([]byte("chunk"))
				_ = r.String()
			}
		}()
	}
	for range 4 {
		<-done
	}
}

func TestProcessEnvironmentExcludesAmbientCredentials(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-should-not-reach-the-child")
	fakeClaude(t, `env | grep -c ANTHROPIC_API_KEY; exit 0`)

	p, err := StartProcess(context.Background(), execConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	_, output := p.Wait()

	if strings.Contains(output, "sk-should-not-reach-the-child") {
		t.Fatal("the ambient api key reached the child process")
	}
	if !strings.Contains(output, "0") {
		t.Errorf("child saw ANTHROPIC_API_KEY; output = %q", output)
	}
}

func TestOutputIsNotMirroredByDefault(t *testing.T) {
	// In `serve` mode corgi's stderr is a log file, and a session's output can
	// contain env values, tokens, and file contents. It must not land there
	// unless a person explicitly asked to watch.
	fakeClaude(t, `echo "secret-value-from-the-session"; exit 0`)

	cfg := execConfig(t)
	if cfg.MirrorOutput {
		t.Fatal("MirrorOutput must default to off")
	}

	captured := captureStderr(t, func() {
		p, err := StartProcess(context.Background(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		p.Wait()
	})

	if strings.Contains(captured, "secret-value-from-the-session") {
		t.Error("session output reached corgi's stderr without MirrorOutput; in serve mode that is a log file on disk")
	}
}

func TestOutputIsMirroredWhenAsked(t *testing.T) {
	fakeClaude(t, `echo "visible-in-foreground"; exit 0`)

	cfg := execConfig(t)
	cfg.MirrorOutput = true

	captured := captureStderr(t, func() {
		p, err := StartProcess(context.Background(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		p.Wait()
	})

	if !strings.Contains(captured, "visible-in-foreground") {
		t.Errorf("--foreground should show what the process is doing, got %q", captured)
	}
}

// captureStderr swaps os.Stderr for a pipe while fn runs.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = original }()

	out := make(chan string, 1)
	go func() {
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				b.Write(buf[:n])
			}
			if err != nil {
				break
			}
		}
		out <- b.String()
	}()

	fn()
	w.Close()
	return <-out
}
