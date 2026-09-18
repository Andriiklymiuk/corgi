package cmd

import (
	"os"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils"
)

func startLogFollow() func() {
	done := make(chan struct{})
	var once sync.Once
	var wg sync.WaitGroup

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

func followUntil(done <-chan struct{}) {
	for {
		select {
		case <-done:
			return
		default:
		}
		if err := followAllLogs(logsBase()); err != nil {
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

const failureLogTailLines = 80

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
