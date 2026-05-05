package tunnel

import (
	"context"
	"strings"
	"sync"
	"testing"
)

type fakeProvider struct {
	emptyCmd bool
}

func (fakeProvider) Name() string { return "fake" }
func (f fakeProvider) Cmd(port int) []string {
	if f.emptyCmd {
		return nil
	}
	return []string{"/bin/echo", "https://fake.example.com"}
}
func (fakeProvider) ExtractURL(line string) string {
	if strings.Contains(line, "https://") {
		return strings.TrimSpace(line)
	}
	return ""
}
func (fakeProvider) InstallHint() string { return "fake install" }
func (fakeProvider) AcceptsStdin() bool  { return false }
func (fakeProvider) PreflightAuth() error { return nil }
func (fakeProvider) CmdNamed(port int, cfg NamedConfig) ([]string, error) {
	return []string{"/bin/echo", "https://" + cfg.Hostname}, nil
}
func (fakeProvider) PreflightNamedAuth(cfg NamedConfig) error { return nil }

func TestRunPropagatesURLAndDone(t *testing.T) {
	events := make(chan Event, 16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		Run(ctx, fakeProvider{}, "svc", 3000, nil, events)
		close(events)
	}()
	wg.Wait()

	var sawURL, sawDone bool
	for ev := range events {
		if ev.URL != "" {
			sawURL = true
		}
		if ev.Done {
			sawDone = true
		}
	}
	if !sawURL || !sawDone {
		t.Errorf("sawURL=%v sawDone=%v", sawURL, sawDone)
	}
}

func TestRunEmptyCmd(t *testing.T) {
	events := make(chan Event, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		Run(ctx, fakeProvider{emptyCmd: true}, "svc", 3000, nil, events)
		close(events)
	}()
	var sawErr bool
	for ev := range events {
		if ev.Err != nil {
			sawErr = true
		}
	}
	if !sawErr {
		t.Error("expected err event")
	}
}

func TestRunMissingBinary(t *testing.T) {
	missingProvider := fakeProviderMissing{}
	events := make(chan Event, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		Run(ctx, missingProvider, "svc", 3000, nil, events)
		close(events)
	}()
	var sawErr bool
	for ev := range events {
		if ev.Err != nil {
			sawErr = true
		}
	}
	if !sawErr {
		t.Error("expected err event")
	}
}

type fakeProviderMissing struct{}

func (fakeProviderMissing) Name() string { return "missing" }
func (fakeProviderMissing) Cmd(port int) []string {
	return []string{"this-binary-cannot-exist-zzz", "--port", "3000"}
}
func (fakeProviderMissing) ExtractURL(line string) string         { return "" }
func (fakeProviderMissing) InstallHint() string                    { return "" }
func (fakeProviderMissing) AcceptsStdin() bool                     { return false }
func (fakeProviderMissing) PreflightAuth() error                   { return nil }
func (fakeProviderMissing) CmdNamed(int, NamedConfig) ([]string, error) {
	return nil, nil
}
func (fakeProviderMissing) PreflightNamedAuth(NamedConfig) error { return nil }

func TestRunNamedHostnameEmitted(t *testing.T) {
	events := make(chan Event, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		Run(ctx, fakeProvider{}, "svc", 3000, &NamedConfig{Hostname: "x.example.com"}, events)
		close(events)
	}()
	urls := []string{}
	for ev := range events {
		if ev.URL != "" {
			urls = append(urls, ev.URL)
		}
	}
	if len(urls) == 0 {
		t.Error("expected url event")
	}
	hadHost := false
	for _, u := range urls {
		if u == "https://x.example.com" {
			hadHost = true
		}
	}
	if !hadHost {
		t.Errorf("missing hostname url: %v", urls)
	}
}
