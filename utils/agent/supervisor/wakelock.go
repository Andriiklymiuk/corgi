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
	mode    WakeLockMode
	startFn func(pid int) (*exec.Cmd, error)
}

func NewWakeLock(mode WakeLockMode) *WakeLock {
	return &WakeLock{mode: mode, startFn: startPlatformWakeLock}
}

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

func WakeLockCommand(pid int) []string {
	switch runtime.GOOS {
	case "darwin":
		argv := []string{"caffeinate", "-i", "-m", "-s"}
		if KeepDisplay {
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

const ClamshellWarning = "on macOS, closing the lid on battery sleeps the machine regardless — " +
	"keep it plugged in, or supervise from an always-on machine"
