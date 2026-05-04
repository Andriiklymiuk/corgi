package utils

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func listenTempPort(t *testing.T) (int, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	return port, func() { _ = ln.Close() }
}

func freeTempPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

func TestIsPortListening_WhenListening(t *testing.T) {
	port, stop := listenTempPort(t)
	defer stop()
	if !IsPortListening(port) {
		t.Fatalf("expected port %d to be reported as listening", port)
	}
}

func TestIsPortListening_WhenFree(t *testing.T) {
	port := freeTempPort(t)
	if IsPortListening(port) {
		t.Fatalf("expected port %d to be reported as free", port)
	}
}

func TestPortOwner_EmptyWhenFree(t *testing.T) {
	port := freeTempPort(t)
	if got := PortOwner(port); got != "" {
		t.Fatalf("expected empty owner for free port, got %q", got)
	}
}

func TestPortOwner_NonEmptyWhenListening(t *testing.T) {
	port, stop := listenTempPort(t)
	defer stop()
	got := PortOwner(port)
	if got == "" {
		t.Skip("lsof may not be available on this platform; skipping")
	}
}

func TestIsHTTPHealthy_2xxIsHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	ok, code, reason := IsHTTPHealthy(srv.URL, 2*time.Second)
	if !ok || code != http.StatusOK || reason != "" {
		t.Fatalf("expected healthy 200, got ok=%v code=%d reason=%q", ok, code, reason)
	}
}

func TestIsHTTPHealthy_4xxStillHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	ok, code, reason := IsHTTPHealthy(srv.URL, 2*time.Second)
	if !ok {
		t.Fatalf("expected 4xx to be considered healthy (service responded), got unhealthy")
	}
	if code != http.StatusNotFound {
		t.Fatalf("expected code 404, got %d", code)
	}
	if reason != "" {
		t.Fatalf("expected empty reason on HTTP response, got %q", reason)
	}
}

func TestIsHTTPHealthy_5xxIsUnhealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	ok, code, reason := IsHTTPHealthy(srv.URL, 2*time.Second)
	if ok {
		t.Fatalf("expected 5xx unhealthy, got healthy")
	}
	if code != http.StatusInternalServerError {
		t.Fatalf("expected code 500, got %d", code)
	}
	if reason != "" {
		t.Fatalf("expected empty reason on HTTP response, got %q", reason)
	}
}

func TestIsHTTPHealthy_ConnRefusedIsUnhealthy(t *testing.T) {
	port := freeTempPort(t)
	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	ok, code, reason := IsHTTPHealthy(url, 500*time.Millisecond)
	if ok {
		t.Fatalf("expected unreachable URL to be unhealthy")
	}
	if code != 0 {
		t.Fatalf("expected code 0 on transport error, got %d", code)
	}
	if reason != "connection refused" {
		t.Fatalf("expected reason %q, got %q", "connection refused", reason)
	}
}

func TestIsHTTPHealthy_TimeoutReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	ok, code, reason := IsHTTPHealthy(srv.URL, 100*time.Millisecond)
	if ok {
		t.Fatalf("expected timeout to be unhealthy")
	}
	if code != 0 {
		t.Fatalf("expected code 0 on timeout, got %d", code)
	}
	if reason != "timeout" {
		t.Fatalf("expected reason %q, got %q", "timeout", reason)
	}
}
