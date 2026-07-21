package cmd

import (
	"sync"
	"time"
)

// startLogFollow streams every service's log in the background while the
// readiness gate waits, and returns a function that stops it. The stack is
// detached by then, so its output is going to files rather than the console —
// without this a boot is silent for as long as the installs take.
func startLogFollow() func() {
	done := make(chan struct{})
	var once sync.Once
	var wg sync.WaitGroup

	// Tail forever; stopping is the caller's job once readiness resolves.
	prevIdle := logsIdleFlag
	logsIdleFlag = 0

	wg.Add(1)
	go func() {
		defer wg.Done()
		followUntil(done)
	}()

	return func() {
		once.Do(func() {
			close(done)
			// A short grace period so the last lines land before the summary.
			waited := make(chan struct{})
			go func() { wg.Wait(); close(waited) }()
			select {
			case <-waited:
			case <-time.After(2 * time.Second):
			}
			logsIdleFlag = prevIdle
		})
	}
}

// followUntil re-enters the tail whenever it returns, so a stack whose logs
// have not appeared yet is still picked up.
func followUntil(done <-chan struct{}) {
	for {
		select {
		case <-done:
			return
		default:
		}
		if err := followAllLogs(logsBase()); err != nil {
			// No logs yet; wait and look again rather than giving up.
			select {
			case <-done:
				return
			case <-time.After(time.Second):
			}
		}
		select {
		case <-done:
			return
		default:
		}
	}
}
