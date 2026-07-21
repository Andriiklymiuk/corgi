package cmd

import (
	"os"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils"
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

// failureLogTailLines is enough to show a stack trace or a failed install
// without burying the error that preceded it.
const failureLogTailLines = 80

// printFailureLogs shows the tail of every service's newest log after a failed
// boot. Grouped per service so a reader can find the one that matters.
func printFailureLogs() {
	base := logsBase()
	services, err := utils.ListLoggedServices(base)
	if err != nil || len(services) == 0 {
		return
	}
	utils.Info("\n─── service logs ───")
	for _, svc := range services {
		runs, runErr := utils.ListServiceRuns(base, svc)
		if runErr != nil || len(runs) == 0 {
			continue
		}
		utils.Infof("\n── %s ──\n", svc)
		for _, line := range tailLines(runs[0], failureLogTailLines) {
			utils.Info(line)
		}
	}
}

func tailLines(path string, n int) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}
