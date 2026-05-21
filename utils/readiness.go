package utils

import (
	"context"
	"fmt"
	"time"
)

// readinessPollInterval is how often readiness probes retry while waiting.
const readinessPollInterval = 500 * time.Millisecond

// WaitForDBReady blocks until the db is reachable or ctx is done.
// Uses HTTP healthCheck when set, else a TCP probe on the mapped port.
// If no port is known, falls back to a short fixed wait (legacy behavior).
func WaitForDBReady(ctx context.Context, db DatabaseService) error {
	if db.Port == 0 {
		// No port to probe — preserve the historical fixed wait.
		time.Sleep(3 * time.Second)
		return nil
	}
	return pollReady(ctx, db.ServiceName, db.Port, db.HealthCheck)
}

// WaitForServiceReady blocks until the service is reachable or ctx is done.
// HTTP healthCheck when set, else TCP on svc.Port. No port => returns nil immediately.
func WaitForServiceReady(ctx context.Context, svc Service) error {
	if svc.Port == 0 {
		return nil
	}
	return pollReady(ctx, svc.ServiceName, svc.Port, svc.HealthCheck)
}

// pollReady probes a target every readinessPollInterval until it is reachable
// or ctx is done. healthCheck (an HTTP path) selects an HTTP probe; otherwise a
// TCP connect is used.
func pollReady(ctx context.Context, name string, port int, healthCheck string) error {
	start := time.Now()
	for {
		if probeOnce(port, healthCheck) {
			return nil
		}
		select {
		case <-ctx.Done():
			waited := time.Since(start).Round(time.Second)
			return fmt.Errorf("%s: %s not ready after %s", ErrReadinessTimeout, name, waited)
		case <-time.After(readinessPollInterval):
		}
	}
}

func probeOnce(port int, healthCheck string) bool {
	if healthCheck != "" {
		url := fmt.Sprintf("http://localhost:%d%s", port, healthCheck)
		healthy, _, _ := IsHTTPHealthy(url, readinessPollInterval)
		return healthy
	}
	return IsPortListening(port)
}
