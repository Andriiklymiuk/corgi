package utils

import "fmt"

// Per-service auto-heal policy for detached runs.
type RestartPolicy struct {
	Mode           string `yaml:"mode,omitempty"`           // "on-failure" | "never" (default)
	MaxRetries     int    `yaml:"maxRetries,omitempty"`     // 0 = no retries
	BackoffSeconds int    `yaml:"backoffSeconds,omitempty"` // delay between retries
}

// ValidateRestartPolicy checks mode + non-negative counters. nil = valid (off).
func ValidateRestartPolicy(p *RestartPolicy) error {
	if p == nil {
		return nil
	}
	switch p.Mode {
	case "", "never", "on-failure":
	default:
		return fmt.Errorf("restartPolicy.mode %q invalid (want on-failure or never)", p.Mode)
	}
	if p.MaxRetries < 0 {
		return fmt.Errorf("restartPolicy.maxRetries must be >= 0")
	}
	if p.BackoffSeconds < 0 {
		return fmt.Errorf("restartPolicy.backoffSeconds must be >= 0")
	}
	return nil
}
