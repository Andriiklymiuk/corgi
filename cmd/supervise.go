package cmd

import (
	"time"

	"andriiklymiuk/corgi/utils"
)

// Relaunch detached procs that crashed at startup, per restartPolicy.
func healCrashedDetached(corgi *utils.CorgiCompose, procs []detachedProc) {
	for i := range procs {
		if procs[i].status != "crashed" {
			continue
		}
		svc := findService(corgi, procs[i].name)
		if svc == nil || svc.RestartPolicy == nil {
			continue
		}
		relaunch := func() bool {
			pid, command, err := relaunchDetachedService(*svc)
			if err != nil || pid == 0 {
				return false
			}
			procs[i].pid, procs[i].pgid, procs[i].command = pid, pid, command
			time.Sleep(300 * time.Millisecond)
			return utils.PidAlive(pid, command)
		}
		if recovered, attempts := healCrashed(svc.RestartPolicy, relaunch, time.Sleep); recovered {
			procs[i].status = "running"
			utils.Infof("♻️  %s recovered after %d retry(ies)\n", procs[i].name, attempts)
		}
	}
}

// Retry relaunch up to maxRetries with backoff. Bounded, no goroutine. relaunch/sleep injected for tests.
func healCrashed(policy *utils.RestartPolicy, relaunch func() bool, sleep func(time.Duration)) (recovered bool, attempts int) {
	if policy == nil || policy.Mode != "on-failure" || policy.MaxRetries <= 0 {
		return false, 0
	}
	for attempts < policy.MaxRetries {
		if policy.BackoffSeconds > 0 {
			sleep(time.Duration(policy.BackoffSeconds) * time.Second)
		}
		attempts++
		if relaunch() {
			return true, attempts
		}
	}
	return false, attempts
}
