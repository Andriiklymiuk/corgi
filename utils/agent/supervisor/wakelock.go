package supervisor

import (
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"time"
)

// WakeLockMode controls when the machine is kept awake.
type WakeLockMode string

const (
	// WakeLockSession holds the lock only while a remote-control process is
	// running. The sensible default: a laptop that never sleeps is a flat
	// battery, and a lock that outlives its process is worse than none.
	WakeLockSession WakeLockMode = "session"
	// WakeLockAlways keeps the machine awake for as long as the daemon runs.
	WakeLockAlways WakeLockMode = "always"
	// WakeLockOff never takes a lock — correct on a desktop or a mini.
	WakeLockOff WakeLockMode = "off"
	// WakeLockIdle holds the lock while the session is doing something and
	// releases it once the session has been quiet — waiting on the person — for
	// WakeLockIdleTimeout, so the laptop can sleep between turns and wakes back
	// to full speed when work resumes.
	WakeLockIdle WakeLockMode = "idle"
)

// WakeLockIdleTimeout is how long a session must produce no output before the
// idle mode lets the machine sleep. Long enough that a slow build or a thinking
// pause does not drop the lock mid-task.
const WakeLockIdleTimeout = 5 * time.Minute

// ValidWakeLockMode reports whether m is a mode the supervisor understands.
func ValidWakeLockMode(m WakeLockMode) bool {
	switch m {
	case WakeLockSession, WakeLockAlways, WakeLockOff, WakeLockIdle:
		return true
	}
	return false
}

// WakeLock keeps the machine from sleeping while a supervised session runs.
//
// On macOS the lock is tied to the supervised pid with `caffeinate -w`, so a
// crashed session cannot leave the machine awake overnight. `-d` is
// deliberately omitted: a headless supervisor has no reason to keep the display
// on, and it costs real power.
type WakeLock struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	mode    WakeLockMode
	startFn func(pid int) (*exec.Cmd, error) // test seam
}

// NewWakeLock returns a lock for the given mode.
func NewWakeLock(mode WakeLockMode) *WakeLock {
	return &WakeLock{mode: mode, startFn: startPlatformWakeLock}
}

// Acquire starts holding the lock against pid. Calling it while already held
// is a no-op, so a restart loop cannot stack up caffeinate processes.
func (w *WakeLock) Acquire(pid int) error {
	if w == nil || w.mode == WakeLockOff {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cmd != nil {
		return nil
	}
	cmd, err := w.startFn(pid)
	if err != nil {
		return err
	}
	w.cmd = cmd
	return nil
}

// Release drops the lock. Safe to call when nothing is held.
func (w *WakeLock) Release() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cmd == nil {
		return
	}
	if w.cmd.Process != nil {
		_ = w.cmd.Process.Kill()
		_, _ = w.cmd.Process.Wait()
	}
	w.cmd = nil
}

// Held reports whether a lock is currently active.
func (w *WakeLock) Held() bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.cmd != nil
}

// Supported reports whether this platform can hold a wake lock, so status and
// doctor can say so plainly rather than silently doing nothing.
func Supported() bool {
	switch runtime.GOOS {
	case "darwin", "linux":
		return true
	}
	return false
}

// WakeLockCommand returns the argv used to hold the lock on this platform, or
// nil where none exists. Exposed so `corgi agent doctor` can show it.
func WakeLockCommand(pid int) []string {
	switch runtime.GOOS {
	case "darwin":
		// -i idle, -m disk, -s system; -w ties the lock's life to pid.
		return []string{"caffeinate", "-i", "-m", "-s", "-w", strconv.Itoa(pid)}
	case "linux":
		// Wait on the supervised pid rather than sleeping forever, so the
		// inhibitor dies with the process it exists for. `sleep infinity` would
		// leave a permanent sleep inhibitor behind if the daemon were SIGKILLed
		// — the Linux equivalent of the bug caffeinate's -w avoids.
		return []string{
			"systemd-inhibit",
			"--what=idle:sleep",
			"--why=corgi agent",
			"--mode=block",
			"sh", "-c",
			fmt.Sprintf("while kill -0 %d 2>/dev/null; do sleep 5; done", pid),
		}
	}
	return nil
}

func startPlatformWakeLock(pid int) (*exec.Cmd, error) {
	argv := WakeLockCommand(pid)
	if argv == nil {
		return nil, fmt.Errorf("wake lock is not supported on %s", runtime.GOOS)
	}
	if _, err := exec.LookPath(argv[0]); err != nil {
		return nil, fmt.Errorf("wake lock needs %s: %w", argv[0], err)
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("could not start %s: %w", argv[0], err)
	}
	return cmd, nil
}

// ClamshellWarning is the limit worth stating in docs and in `status`: on macOS
// a lid closed on battery sleeps the machine no matter what caffeinate does.
const ClamshellWarning = "on macOS, closing the lid on battery sleeps the machine regardless — " +
	"keep it plugged in, or supervise from an always-on machine"
