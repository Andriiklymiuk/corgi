package utils

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func warmupServer(t *testing.T, h http.HandlerFunc) (port int, done func()) {
	t.Helper()
	srv := httptest.NewServer(h)
	addr := strings.TrimPrefix(srv.URL, "http://")
	p, err := strconv.Atoi(addr[strings.LastIndex(addr, ":")+1:])
	if err != nil {
		t.Fatal(err)
	}
	return p, srv.Close
}

// The whole point: warmup is performed once, however slow it is.
func TestWarmupRequestsOnce(t *testing.T) {
	var calls int32
	port, done := warmupServer(t, func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		time.Sleep(300 * time.Millisecond)
		w.Write([]byte(`<div id="root"></div>`))
	})
	defer done()

	err := RunWarmup(context.Background(), "web", port, &WarmupCheck{Path: "/", Expect: "root"})
	if err != nil {
		t.Fatalf("warmup: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("warmup must make exactly one request, made %d", got)
	}
}

func TestWarmupFailsWhenBodyLacksExpected(t *testing.T) {
	port, done := warmupServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("something else"))
	})
	defer done()

	err := RunWarmup(context.Background(), "web", port, &WarmupCheck{Expect: "root"})
	if err == nil || !strings.Contains(err.Error(), "did not contain") {
		t.Errorf("expected a substring failure, got %v", err)
	}
}

func TestWarmupFailsOnServerError(t *testing.T) {
	port, done := warmupServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	defer done()

	if err := RunWarmup(context.Background(), "web", port, &WarmupCheck{}); err == nil {
		t.Error("a 5xx must fail warmup")
	}
}

func TestWarmupHonoursItsOwnTimeout(t *testing.T) {
	port, done := warmupServer(t, func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
	})
	defer done()

	start := time.Now()
	err := RunWarmup(context.Background(), "web", port, &WarmupCheck{Timeout: 200 * time.Millisecond})
	if err == nil {
		t.Fatal("expected a timeout")
	}
	if time.Since(start) > time.Second {
		t.Errorf("timeout was not honoured, waited %s", time.Since(start))
	}
}

func TestWarmupIsANoOpWhenUnset(t *testing.T) {
	if err := RunWarmup(context.Background(), "web", 1234, nil); err != nil {
		t.Errorf("no warmup declared means nothing to do, got %v", err)
	}
	if err := RunWarmup(context.Background(), "web", 0, &WarmupCheck{}); err != nil {
		t.Errorf("no port means nothing to do, got %v", err)
	}
}

func TestWarmupDefaults(t *testing.T) {
	var w *WarmupCheck
	if w.path() != "/" || w.timeout() != DefaultWarmupTimeout {
		t.Error("nil must give the documented defaults")
	}
	set := &WarmupCheck{Path: "/ready", Timeout: time.Minute}
	if set.path() != "/ready" || set.timeout() != time.Minute {
		t.Error("explicit values must win")
	}
}
