package utils

import (
	"sync"
	"sync/atomic"
	"time"
)

// Shutdown channel lets blocking init/poll loops abort on Ctrl+C
// instead of running until handleRunSignal reaches os.Exit.
//
// State is wrapped in a struct held by atomic.Pointer so SIGHUP can
// reset (publish a new state) concurrently with SIGINT requesting
// shutdown without data races.

type shutdownState struct {
	ch   chan struct{}
	once sync.Once
}

var state atomic.Pointer[shutdownState]

func init() {
	state.Store(&shutdownState{ch: make(chan struct{})})
}

// RequestShutdown closes the shutdown channel. Idempotent.
func RequestShutdown() {
	s := state.Load()
	s.once.Do(func() { close(s.ch) })
}

func ShutdownCh() <-chan struct{} { return state.Load().ch }

func ShutdownRequested() bool {
	select {
	case <-state.Load().ch:
		return true
	default:
		return false
	}
}

// InterruptibleSleep returns true if shutdown woke it, false on timeout.
func InterruptibleSleep(d time.Duration) bool {
	select {
	case <-time.After(d):
		return false
	case <-state.Load().ch:
		return true
	}
}

// ResetShutdown re-arms shutdown signaling. Called by the SIGHUP reload
// path and by tests.
func ResetShutdown() {
	state.Store(&shutdownState{ch: make(chan struct{})})
}

func ResetShutdownForTests() { ResetShutdown() }
