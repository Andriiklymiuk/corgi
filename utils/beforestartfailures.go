package utils

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// A beforeStart that fails leaves its service unable to start, but the run
// continues. Recording the failure lets the readiness gate say so immediately
// instead of waiting out a timeout for a service that was never coming.
var (
	beforeStartMu       sync.Mutex
	beforeStartFailures = map[string]error{}
)

func RecordBeforeStartFailure(serviceName string, err error) {
	if err == nil {
		return
	}
	beforeStartMu.Lock()
	defer beforeStartMu.Unlock()
	beforeStartFailures[serviceName] = err
}

// BeforeStartFailed reports the services whose beforeStart failed, sorted so
// the message is stable.
func BeforeStartFailed() []string {
	beforeStartMu.Lock()
	defer beforeStartMu.Unlock()
	names := make([]string, 0, len(beforeStartFailures))
	for name := range beforeStartFailures {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// BeforeStartFailureError describes every failure, or nil when there are none.
func BeforeStartFailureError() error {
	names := BeforeStartFailed()
	if len(names) == 0 {
		return nil
	}
	beforeStartMu.Lock()
	defer beforeStartMu.Unlock()
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s (%v)", name, beforeStartFailures[name]))
	}
	return fmt.Errorf("beforeStart failed for %s", strings.Join(parts, ", "))
}

// ResetBeforeStartFailures exists for tests and for a restart reusing the process.
func ResetBeforeStartFailures() {
	beforeStartMu.Lock()
	defer beforeStartMu.Unlock()
	beforeStartFailures = map[string]error{}
}
