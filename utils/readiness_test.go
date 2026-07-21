package utils

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// httpReadyPort starts an httptest server that serves 200 at the given path and
// returns its port. The caller closes the server.
func httpReadyPort(t *testing.T, path string) (*httptest.Server, int) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	u := strings.TrimPrefix(srv.URL, "http://")
	_, portStr, err := net.SplitHostPort(u)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("atoi: %v", err)
	}
	return srv, port
}

func TestReadiness_ServiceHTTPHealthCheckReady(t *testing.T) {
	srv, port := httpReadyPort(t, "/healthz")
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := WaitForServiceReady(ctx, Service{ServiceName: "svc", Port: port, HealthCheck: "/healthz"})
	if err != nil {
		t.Fatalf("expected HTTP healthcheck ready, got %v", err)
	}
}

func TestReadiness_DBHTTPHealthCheckReady(t *testing.T) {
	srv, port := httpReadyPort(t, "/ready")
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := WaitForDBReady(ctx, DatabaseService{ServiceName: "db", Port: port, HealthCheck: "/ready"})
	if err != nil {
		t.Fatalf("expected HTTP healthcheck ready, got %v", err)
	}
}

// listenerPort opens a TCP listener on an ephemeral port and returns it plus
// the chosen port. The caller closes the listener.
func listenerPort(t *testing.T) (net.Listener, int) {
	t.Helper()
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	return ln, ln.Addr().(*net.TCPAddr).Port
}

func TestReadiness_ServiceListeningReturnsNil(t *testing.T) {
	ln, port := listenerPort(t)
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := WaitForServiceReady(ctx, Service{ServiceName: "svc", Port: port}); err != nil {
		t.Fatalf("expected ready, got %v", err)
	}
}

func TestReadiness_ServiceClosedPortTimesOut(t *testing.T) {
	// Grab a port then close it so nothing is listening.
	ln, port := listenerPort(t)
	ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()

	err := WaitForServiceReady(ctx, Service{ServiceName: "svc", Port: port})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), ErrReadinessTimeout) {
		t.Fatalf("expected error wrapping %s, got %v", ErrReadinessTimeout, err)
	}
}

func TestReadiness_ServiceNoPortReturnsNilImmediately(t *testing.T) {
	// No port => "started"-style path: returns nil without waiting.
	ctx := context.Background()
	if err := WaitForServiceReady(ctx, Service{ServiceName: "svc"}); err != nil {
		t.Fatalf("expected nil for no-port service, got %v", err)
	}
}

func TestReadiness_DBNoPortFallbackReturnsNil(t *testing.T) {
	ctx := context.Background()
	if err := WaitForDBReady(ctx, DatabaseService{ServiceName: "db"}); err != nil {
		t.Fatalf("expected nil for port-0 db, got %v", err)
	}
}

func TestReadiness_DBListeningReturnsNil(t *testing.T) {
	ln, port := listenerPort(t)
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := WaitForDBReady(ctx, DatabaseService{ServiceName: "db", Port: port}); err != nil {
		t.Fatalf("expected ready, got %v", err)
	}
}

// A dev server doing work on its first request must not be reported as down.
func TestReadinessProbeToleratesASlowFirstResponse(t *testing.T) {
	var mu sync.Mutex
	first := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		slow := first
		first = false
		mu.Unlock()
		if slow {
			// Longer than the poll interval, well inside the probe timeout.
			time.Sleep(2 * time.Second)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	healthy, _, reason := IsHTTPHealthy(srv.URL, ReadinessProbeTimeout)
	if !healthy {
		t.Errorf("a 2s first response must still count as healthy, got %q", reason)
	}
}

func TestReadinessProbeTimeoutExceedsPollInterval(t *testing.T) {
	if ReadinessProbeTimeout <= readinessPollInterval {
		t.Errorf("probe timeout %s must exceed the poll interval %s, or a server slower than one poll is reported down",
			ReadinessProbeTimeout, readinessPollInterval)
	}
}
