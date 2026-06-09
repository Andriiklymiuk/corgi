package tunnel

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeProviderNamedErr fails CmdNamed so Run's named-mode error branch runs.
type fakeProviderNamedErr struct{ fakeProvider }

func (fakeProviderNamedErr) CmdNamed(int, NamedConfig) ([]string, error) {
	return nil, errors.New("named config rejected")
}

func TestRunNamedCmdError(t *testing.T) {
	events := make(chan Event, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		Run(ctx, fakeProviderNamedErr{}, "svc", 3000, &NamedConfig{Hostname: "x"}, events)
		close(events)
	}()
	var sawErr bool
	for ev := range events {
		if ev.Err != nil {
			sawErr = true
		}
	}
	if !sawErr {
		t.Error("a CmdNamed error must surface as an Err event")
	}
}

// Zero BackoffConfig must fall back to the default base/max delays.
func TestRunSupervisedDefaultBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan Event, 64)

	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for range events {
		}
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		RunSupervised(ctx, fakeProvider{}, "svc", 3000, nil, events, BackoffConfig{}) // zero → defaults
	}()

	// The default base is 500ms; cancel during the first wait so the loop exits
	// via ctx.Done without us sitting through the full delay.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunSupervised with default backoff did not return after cancel")
	}
	close(events)
	<-drained
}

// Backoff that overshoots the ceiling must clamp to max.
func TestRunSupervisedClampsToMax(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan Event, 64)

	var mu sync.Mutex
	var dones int
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for ev := range events {
			if ev.Done {
				mu.Lock()
				dones++
				mu.Unlock()
			}
		}
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		// 15ms*2 = 30ms overshoots the 20ms ceiling, so delay clamps to 20ms.
		RunSupervised(ctx, fakeProvider{}, "svc", 3000, nil, events,
			BackoffConfig{Base: 15 * time.Millisecond, Max: 20 * time.Millisecond})
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunSupervised did not return after cancel")
	}
	close(events)
	<-drained

	mu.Lock()
	got := dones
	mu.Unlock()
	if got < 2 {
		t.Fatalf("expected >=2 restarts, got %d", got)
	}
}
