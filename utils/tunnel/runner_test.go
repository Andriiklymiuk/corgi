package tunnel

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
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
func (fakeProvider) InstallHint() string  { return "fake install" }
func (fakeProvider) AcceptsStdin() bool   { return false }
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

func TestRunSendsRespectCancel(t *testing.T) {
	// Unbuffered: any send blocks unless drained. We never drain, so the only
	// way Run returns is by abandoning its sends on ctx cancel.
	events := make(chan Event)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		Run(ctx, fakeProvider{}, "svc", 3000, nil, events)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel (send blocked on dead consumer)")
	}
}

func TestRunSupervisedRestartsWithBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan Event, 64)

	var mu sync.Mutex
	var doneCount int
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		for ev := range events {
			if ev.Done {
				mu.Lock()
				doneCount++
				mu.Unlock()
			}
		}
	}()

	supervisedDone := make(chan struct{})
	go func() {
		defer close(supervisedDone)
		RunSupervised(ctx, fakeProvider{}, "svc", 3000, nil, events,
			BackoffConfig{Base: 5 * time.Millisecond, Max: 20 * time.Millisecond})
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case <-supervisedDone:
	case <-time.After(2 * time.Second):
		t.Fatal("RunSupervised did not return within 2s of cancel")
	}
	close(events)
	<-drainDone

	mu.Lock()
	got := doneCount
	mu.Unlock()
	if got < 2 {
		t.Fatalf("expected >=2 Done events (restarted), got %d", got)
	}
}

type fakeProviderMissing struct{}

func (fakeProviderMissing) Name() string { return "missing" }
func (fakeProviderMissing) Cmd(port int) []string {
	return []string{"this-binary-cannot-exist-zzz", "--port", "3000"}
}
func (fakeProviderMissing) ExtractURL(line string) string { return "" }
func (fakeProviderMissing) InstallHint() string           { return "" }
func (fakeProviderMissing) AcceptsStdin() bool            { return false }
func (fakeProviderMissing) PreflightAuth() error          { return nil }
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
