package utils

import (
	"context"
	"fmt"
	"time"
)

// readinessPollInterval is how often readiness probes retry while waiting.
const readinessPollInterval = 500 * time.Millisecond

// readinessProbeTimeout is how long one probe may take before it counts as a
// failure. Deliberately far longer than the poll interval: a dev server does
// real work on its first request — Vite pre-bundles dependencies, Metro builds
// a bundle, Nest warms up — and on a machine busy starting a whole stack that
// easily takes seconds. Timing out in half a second reports a healthy service
// as down.
// ReadinessProbeTimeout is exported so status probes share the same tolerance.
const ReadinessProbeTimeout = 10 * time.Second

// WaitForDBReady blocks until the db is reachable or ctx is done. With no known
// port it falls back to a short fixed wait (legacy behavior).
func WaitForDBReady(ctx context.Context, db DatabaseService) error {
	if db.Port == 0 {
		time.Sleep(3 * time.Second)
		return nil
	}
	return pollReady(ctx, db.ServiceName, db.Port, db.HealthCheck)
}

// WaitForServiceReady blocks until the service is reachable or ctx is done.
// No port => returns nil immediately.
func WaitForServiceReady(ctx context.Context, svc Service) error {
	if svc.Port == 0 {
		return nil
	}
	if err := pollReady(ctx, svc.ServiceName, svc.Port, svc.HealthCheck); err != nil {
		return err
	}
	// Only once the service is listening, and only once.
	return RunWarmup(ctx, svc.ServiceName, svc.Port, svc.Warmup)
}

// pollReady probes a target every readinessPollInterval until reachable or ctx
// is done. A non-empty healthCheck selects an HTTP probe; else a TCP connect.
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
		healthy, _, _ := IsHTTPHealthy(url, ReadinessProbeTimeout)
		return healthy
	}
	return IsPortListening(port)
}
