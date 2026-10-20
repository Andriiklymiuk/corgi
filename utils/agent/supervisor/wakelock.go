package supervisor

import (
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"time"
)

type WakeLockMode string

const (
	WakeLockSession WakeLockMode = "session"
	WakeLockAlways  WakeLockMode = "always"
	WakeLockOff     WakeLockMode = "off"
	WakeLockIdle    WakeLockMode = "idle"
)

const WakeLockIdleTimeout = 5 * time.Minute

func ValidWakeLockMode(m WakeLockMode) bool {
	switch m {
	case WakeLockSession, WakeLockAlways, WakeLockOff, WakeLockIdle:
		return true
	}
	return false
}

type WakeLock struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	pid     int
	mode    WakeLockMode
	display bool
	startFn func(pid int, display bool) (*exec.Cmd, error)
}

func NewWakeLock(mode WakeLockMode) *WakeLock {
	return &WakeLock{mode: mode, display: KeepDisplay, startFn: startPlatformWakeLock}
}

// FakeWakeLock holds a sleeping process instead of caffeinate, for tests.
func FakeWakeLock(mode WakeLockMode) *WakeLock {
	w := NewWakeLock(mode)
	w.startFn = func(int, bool) (*exec.Cmd, error) {
		cmd := exec.Command("sleep", "30")
		return cmd, cmd.Start()
	}
	return w
}

func (w *WakeLock) Acquire(pid int) error {
	if w == nil || w.mode == WakeLockOff {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.acquireLocked(pid)
}

func (w *WakeLock) acquireLocked(pid int) error {
	if w.cmd != nil {
		return nil
	}
	cmd, err := w.startFn(pid, w.display)
	if err != nil {
		return err
	}
	w.cmd, w.pid = cmd, pid
	return nil
}

func (w *WakeLock) Release() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.releaseLocked()
}

func (w *WakeLock) releaseLocked() {
	if w.cmd == nil {
		return
	}
	if w.cmd.Process != nil {
		_ = w.cmd.Process.Kill()
		_, _ = w.cmd.Process.Wait()
	}
	w.cmd = nil
}

// SetDisplay keeps the screen lit with the lock or lets it sleep; a held lock
// is taken again the new way. Lid closed: no screen to keep lit.
func (w *WakeLock) SetDisplay(on bool) error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.display == on {
		return nil
	}
	w.display = on
	if w.cmd == nil {
		return nil
	}
	w.releaseLocked()
	return w.acquireLocked(w.pid)
}

func (w *WakeLock) Display() bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.display
}

func (w *WakeLock) Held() bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.cmd != nil
}

var KeepDisplay bool

func Supported() bool {
	switch runtime.GOOS {
	case "darwin", "linux":
		return true
	}
	return false
}

func WakeLockCommand(pid int) []string { return wakeLockCommand(pid, KeepDisplay) }

func wakeLockCommand(pid int, display bool) []string {
	switch runtime.GOOS {
	case "darwin":
		argv := []string{"caffeinate", "-i", "-m", "-s"}
		if display {
			argv = append(argv, "-d")
		}
		return append(argv, "-w", strconv.Itoa(pid))
	case "linux":
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

func startPlatformWakeLock(pid int, display bool) (*exec.Cmd, error) {
	argv := wakeLockCommand(pid, display)
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

const ClamshellWarning = "on macOS, closing the lid sleeps the machine unless `sudo pmset -a disablesleep 1` - " +
	"plugged in; corgi agent doctor --away checks it"
