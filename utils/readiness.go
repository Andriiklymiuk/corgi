package utils

import (
	"context"
	"fmt"
	"time"
)

const readinessPollInterval = 500 * time.Millisecond

const ReadinessProbeTimeout = 10 * time.Second

func WaitForDBReady(ctx context.Context, db DatabaseService) error {
	if db.Port == 0 {
		time.Sleep(3 * time.Second)
		return nil
	}
	return pollReady(ctx, db.ServiceName, db.Port, db.HealthCheck)
}

func WaitForServiceReady(ctx context.Context, svc Service) error {
	if svc.Port == 0 {
		return nil
	}
	if err := pollReady(ctx, svc.ServiceName, svc.Port, svc.HealthCheck); err != nil {
		return err
	}
	return RunWarmup(ctx, svc.ServiceName, svc.Port, svc.Warmup)
}

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
