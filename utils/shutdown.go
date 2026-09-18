package utils

import (
	"sync"
	"sync/atomic"
	"time"
)

// atomic.Pointer so SIGHUP reset and SIGINT shutdown stay race-free.
type shutdownState struct {
	ch   chan struct{}
	once sync.Once
}

var state atomic.Pointer[shutdownState]

func init() {
	state.Store(&shutdownState{ch: make(chan struct{})})
}

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

func InterruptibleSleep(d time.Duration) bool {
	select {
	case <-time.After(d):
		return false
	case <-state.Load().ch:
		return true
	}
}

func ResetShutdown() {
	state.Store(&shutdownState{ch: make(chan struct{})})
}

func ResetShutdownForTests() { ResetShutdown() }
