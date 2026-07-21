package utils

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultWarmupTimeout is generous because warmup exists for work that is slow
// by nature — bundling an app, compiling on first request.
const DefaultWarmupTimeout = 10 * time.Minute

// WarmupCheck is one expensive request performed once a service is listening,
// before it counts as ready.
type WarmupCheck struct {
	Path    string        `yaml:"path,omitempty"`
	Timeout time.Duration `yaml:"timeout,omitempty"`
	// Expect is an optional substring the response body must contain. Without
	// it any non-5xx response passes.
	Expect string `yaml:"expect,omitempty"`
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

// RunWarmup performs the warmup request once and waits for it to complete.
// Deliberately not a poll: a server that builds on demand holds the connection
// until it is done, and asking again only queues a second build.
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
