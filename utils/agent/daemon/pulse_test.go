package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestPulsePingsAndRecords(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("User-Agent") == "" {
			t.Error("no user agent")
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	d := &Daemon{Dir: dir, Version: "test", PulseURL: srv.URL, PulseEvery: 20 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	d.pulse(ctx)
	if hits.Load() < 2 {
		t.Fatalf("want at least 2 pings, got %d", hits.Load())
	}
	if st := ReadPulse(dir); st.At.IsZero() || st.Error != "" {
		t.Fatalf("state: %+v", st)
	}
}

func TestPulseRecordsFailure(t *testing.T) {
	dir := t.TempDir()
	d := &Daemon{Dir: dir, PulseURL: "http://127.0.0.1:1", PulseEvery: time.Hour}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	d.pulse(ctx)
	if st := ReadPulse(dir); st.Error == "" {
		t.Fatal("a refused connection must be recorded")
	}
}

func TestPulseWithoutURLReturns(t *testing.T) {
	d := &Daemon{Dir: t.TempDir()}
	done := make(chan struct{})
	go func() { d.pulse(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("no URL must return at once")
	}
}
