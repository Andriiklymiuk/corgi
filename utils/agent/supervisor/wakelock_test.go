package supervisor

import (
	"errors"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"testing"
)

// fakeLock returns a WakeLock whose held process is a trivial local command,
// so the tests never depend on caffeinate or systemd-inhibit being installed.
func fakeLock(t *testing.T, mode WakeLockMode) (*WakeLock, *int) {
	t.Helper()
	starts := 0
	w := NewWakeLock(mode)
	w.startFn = func(int) (*exec.Cmd, error) {
		starts++
		cmd := exec.Command("sleep", "30")
		if err := cmd.Start(); err != nil {
			t.Fatalf("could not start stand-in process: %v", err)
		}
		return cmd, nil
	}
	t.Cleanup(w.Release)
	return w, &starts
}

func TestWakeLockOffNeverAcquires(t *testing.T) {
	w, starts := fakeLock(t, WakeLockOff)

	if err := w.Acquire(1234); err != nil {
		t.Fatalf("Acquire() on an off lock should be a no-op, got %v", err)
	}
	if w.Held() {
		t.Error("mode off must not hold a lock — someone on a desktop asked for this explicitly")
	}
	if *starts != 0 {
		t.Errorf("mode off started %d processes, want 0", *starts)
	}
}

func TestWakeLockAcquireIsIdempotent(t *testing.T) {
	w, starts := fakeLock(t, WakeLockSession)

	for range 3 {
		if err := w.Acquire(1234); err != nil {
			t.Fatalf("Acquire() = %v", err)
		}
	}

	if *starts != 1 {
		t.Errorf("started %d lock processes, want 1 — a restart loop must not stack them up", *starts)
	}
	if !w.Held() {
		t.Error("lock should be held")
	}
}

func TestWakeLockReleaseThenReacquire(t *testing.T) {
	w, starts := fakeLock(t, WakeLockSession)

	if err := w.Acquire(1); err != nil {
		t.Fatal(err)
	}
	w.Release()
	if w.Held() {
		t.Fatal("lock should be released")
	}
	if err := w.Acquire(2); err != nil {
		t.Fatal(err)
	}
	if !w.Held() {
		t.Error("lock should be held again after re-acquiring")
	}
	if *starts != 2 {
		t.Errorf("started %d lock processes, want 2", *starts)
	}
}

func TestWakeLockReleaseWithoutAcquireIsSafe(t *testing.T) {
	w, _ := fakeLock(t, WakeLockSession)
	w.Release()
	w.Release()
}

func TestWakeLockNilIsSafe(t *testing.T) {
	var w *WakeLock
	if err := w.Acquire(1); err != nil {
		t.Errorf("nil lock Acquire() = %v, want nil", err)
	}
	w.Release()
	if w.Held() {
		t.Error("nil lock must not report held")
	}
}

func TestWakeLockPropagatesStartFailure(t *testing.T) {
	w := NewWakeLock(WakeLockSession)
	sentinel := errors.New("no caffeinate here")
	w.startFn = func(int) (*exec.Cmd, error) { return nil, sentinel }

	if err := w.Acquire(1); !errors.Is(err, sentinel) {
		t.Errorf("Acquire() = %v, want %v — a missing binary must surface, not be swallowed", err, sentinel)
	}
	if w.Held() {
		t.Error("a failed acquire must not report the lock as held")
	}
}

func TestWakeLockCommandTiesLifetimeToPid(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("caffeinate is macOS only")
	}
	argv := WakeLockCommand(4321)

	if argv[0] != "caffeinate" {
		t.Fatalf("argv[0] = %q, want caffeinate", argv[0])
	}
	i := slices.Index(argv, "-w")
	if i < 0 || i+1 >= len(argv) || argv[i+1] != strconv.Itoa(4321) {
		t.Errorf("argv %v must pass -w <pid> so a crashed session cannot leave the machine awake overnight", argv)
	}
	if slices.Contains(argv, "-d") {
		t.Error("-d keeps the display on; a headless supervisor has no reason to and it costs real power")
	}
}

func TestValidWakeLockMode(t *testing.T) {
	for _, m := range []WakeLockMode{WakeLockSession, WakeLockAlways, WakeLockOff} {
		if !ValidWakeLockMode(m) {
			t.Errorf("%q should be valid", m)
		}
	}
	if ValidWakeLockMode("forever-and-ever") {
		t.Error("unknown modes must be rejected so a typo fails at startup")
	}
}
