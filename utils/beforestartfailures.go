package utils

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

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

func ResetBeforeStartFailures() {
	beforeStartMu.Lock()
	defer beforeStartMu.Unlock()
	beforeStartFailures = map[string]error{}
}
