package utils

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const DefaultWarmupTimeout = 10 * time.Minute

type WarmupCheck struct {
	Path    string        `yaml:"path,omitempty"`
	Timeout time.Duration `yaml:"timeout,omitempty"`
	Expect  string        `yaml:"expect,omitempty"`
}

func (w *WarmupCheck) timeout() time.Duration {
	if w == nil || w.Timeout <= 0 {
		return DefaultWarmupTimeout
	}
	return w.Timeout
}

func (w *WarmupCheck) path() string {
	if w == nil || w.Path == "" {
		return "/"
	}
	return w.Path
}

func RunWarmup(ctx context.Context, name string, port int, warmup *WarmupCheck) error {
	if warmup == nil || port == 0 {
		return nil
	}

	url := fmt.Sprintf("http://localhost:%d%s", port, warmup.path())
	Info("warmup:", name, "→", url, "(one request, up to", warmup.timeout().String()+")")

	ctx, cancel := context.WithTimeout(ctx, warmup.timeout())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("warmup %s: %v", name, err)
	}

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return fmt.Errorf("warmup %s: %v", name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return fmt.Errorf("warmup %s: %s returned HTTP %d", name, url, resp.StatusCode)
	}
	if warmup.Expect == "" {
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("warmup %s: reading response: %v", name, err)
	}
	if !strings.Contains(string(body), warmup.Expect) {
		return fmt.Errorf("warmup %s: %s did not contain %q", name, url, warmup.Expect)
	}
	return nil
}
