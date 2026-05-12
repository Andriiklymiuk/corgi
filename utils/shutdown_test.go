package utils

import (
	"testing"
	"time"
)

func TestShutdownCh_ClosedAfterRequest(t *testing.T) {
	ResetShutdownForTests()
	t.Cleanup(ResetShutdownForTests)

	if ShutdownRequested() {
		t.Fatal("expected shutdown not requested initially")
	}
	RequestShutdown()
	if !ShutdownRequested() {
		t.Error("expected ShutdownRequested() == true after RequestShutdown")
	}
	select {
	case <-ShutdownCh():
	default:
		t.Error("ShutdownCh must be closed after RequestShutdown")
	}
}

func TestRequestShutdown_Idempotent(t *testing.T) {
	ResetShutdownForTests()
	t.Cleanup(ResetShutdownForTests)

	// Must not panic on double-close — sync.Once guards it.
	RequestShutdown()
	RequestShutdown()
	RequestShutdown()
}

func TestInterruptibleSleep_TimeoutPath(t *testing.T) {
	ResetShutdownForTests()
	t.Cleanup(ResetShutdownForTests)

	start := time.Now()
	woke := InterruptibleSleep(50 * time.Millisecond)
	elapsed := time.Since(start)
	if woke {
		t.Error("expected timeout, got shutdown signal")
	}
	if elapsed < 40*time.Millisecond {
		t.Errorf("woke too early: %s", elapsed)
	}
}

func TestResetShutdown_RearmsChannel(t *testing.T) {
	ResetShutdown()
	t.Cleanup(ResetShutdown)

	RequestShutdown()
	if !ShutdownRequested() {
		t.Fatal("expected shutdown set")
	}
	ResetShutdown()
	if ShutdownRequested() {
		t.Error("ResetShutdown must clear shutdown state")
	}
	// After reset, RequestShutdown must work again (sync.Once re-armed).
	RequestShutdown()
	if !ShutdownRequested() {
		t.Error("RequestShutdown must work after reset")
	}
}

func TestInterruptibleSleep_AbortsOnShutdown(t *testing.T) {
	ResetShutdownForTests()

	done := make(chan struct{})
	go func() {
		defer close(done)
		time.Sleep(20 * time.Millisecond)
		RequestShutdown()
	}()
	t.Cleanup(func() {
		<-done
		ResetShutdownForTests()
	})

	start := time.Now()
	woke := InterruptibleSleep(5 * time.Second)
	elapsed := time.Since(start)
	if !woke {
		t.Error("expected shutdown wake, got timeout")
	}
	if elapsed > 1*time.Second {
		t.Errorf("did not abort promptly: %s", elapsed)
	}
}
