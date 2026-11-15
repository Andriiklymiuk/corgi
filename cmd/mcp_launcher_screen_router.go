package cmd

import (
	"context"
	"net"
	"strconv"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils/agent/portmap"
	"andriiklymiuk/corgi/utils/agent/screen"
)

const (
	routerLease    = 30 * time.Minute
	routerRenewAt  = 10 * time.Minute
	routerBackoff  = time.Hour
	routerMaxWait  = 1500 * time.Millisecond
	routerMapLimit = 4 * time.Second
)

var (
	mapRouterPort   = portmap.Map
	unmapRouterPort = portmap.Unmap
)

type routerState struct {
	mu          sync.Mutex
	port, fast  int
	quic, lane  portmap.Mapping
	until       time.Time
	failedUntil time.Time
	running     chan struct{}
}

var screenRouter = &routerState{}

func (r *routerState) candidateLocked(now time.Time) (screen.Candidate, bool) {
	if r.quic.External == nil || !now.Before(r.until) {
		return screen.Candidate{}, false
	}
	addr := net.JoinHostPort(r.quic.External.String(), strconv.Itoa(r.quic.Port))
	if r.lane.Port != 0 {
		addr += "/" + strconv.Itoa(r.lane.Port)
	}
	return screen.Candidate{Addr: addr, Kind: "router"}, true
}

func (r *routerState) Candidate(port, fast int, now time.Time) (screen.Candidate, bool) {
	r.mu.Lock()
	if r.port != port || r.fast != fast {
		r.quic, r.lane, r.until, r.failedUntil = portmap.Mapping{}, portmap.Mapping{}, time.Time{}, time.Time{}
		r.port, r.fast = port, fast
	}
	c, ok := r.candidateLocked(now)
	fresh := ok && r.until.Sub(now) > routerRenewAt
	if fresh || now.Before(r.failedUntil) {
		r.mu.Unlock()
		return c, ok
	}
	wait := r.running
	if wait == nil {
		wait = make(chan struct{})
		r.running = wait
		go r.refresh(port, fast, wait)
	}
	r.mu.Unlock()
	if ok {
		return c, true
	}
	select {
	case <-wait:
	case <-time.After(routerMaxWait):
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.candidateLocked(time.Now())
}

func (r *routerState) refresh(port, fast int, done chan struct{}) {
	ctx, cancel := context.WithTimeout(context.Background(), routerMapLimit)
	defer cancel()
	q, err := mapRouterPort(ctx, port, routerLease)
	var l portmap.Mapping
	if err == nil && portmap.Usable(q.External) && fast != 0 {
		l, _ = mapRouterPort(ctx, fast, routerLease)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running == done {
		r.running = nil
	}
	defer close(done)
	if r.port != port || r.fast != fast {
		return
	}
	if err != nil || !portmap.Usable(q.External) {
		r.failedUntil = time.Now().Add(routerBackoff)
		return
	}
	r.quic, r.lane, r.until = q, l, time.Now().Add(routerLease)
}

func (r *routerState) Release() {
	r.mu.Lock()
	q, l, port, fast := r.quic, r.lane, r.port, r.fast
	r.quic, r.lane, r.until, r.failedUntil = portmap.Mapping{}, portmap.Mapping{}, time.Time{}, time.Time{}
	r.mu.Unlock()
	if q.External == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), routerMapLimit)
		defer cancel()
		_ = unmapRouterPort(ctx, q, port)
		if l.Port != 0 {
			_ = unmapRouterPort(ctx, l, fast)
		}
	}()
}
