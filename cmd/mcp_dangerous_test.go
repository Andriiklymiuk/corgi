package cmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/tunnel"
)

func TestDangerousToolGate_ClosedByDefault(t *testing.T) {
	t.Setenv("CORGI_MCP_ALLOW_DANGEROUS_TUNNEL", "")
	if dangerousTunnelToolsAllowed(true /* publicTunnel */) {
		t.Fatal("dangerous tools must be blocked over a public tunnel without the opt-in")
	}
	if !dangerousTunnelToolsAllowed(false /* no public tunnel */) {
		t.Fatal("dangerous tools must stay allowed when there is no public tunnel (non-breaking)")
	}
	t.Setenv("CORGI_MCP_ALLOW_DANGEROUS_TUNNEL", "1")
	if !dangerousTunnelToolsAllowed(true) {
		t.Fatal("explicit opt-in must allow dangerous tools over a tunnel")
	}
}

func TestStartMCPTunnel_NoPasteableTokenBlock(t *testing.T) {
	const token = "corgi_mcp_secrettoken"

	// The public-side block (token="") must NOT embed the bearer token.
	var pub bytes.Buffer
	printMCPClientConfig(&pub, "https://example.trycloudflare.com/mcp", "")
	if strings.Contains(pub.String(), token) || strings.Contains(pub.String(), "Authorization") {
		t.Fatalf("public client config must not include the bearer token: %s", pub.String())
	}

	// The local-side block (token set) still prints the Authorization header.
	var local bytes.Buffer
	printMCPClientConfig(&local, "http://127.0.0.1:8765/mcp", token)
	if !strings.Contains(local.String(), token) {
		t.Fatalf("local client config should include the token: %s", local.String())
	}
}

func TestDangerousToolGate_OpensForAVerifiedPrivateTunnel(t *testing.T) {
	// A tunnel behind an identity proxy is not open to the internet, so the
	// tools that make agent mode useful from a phone stay usable without
	// anyone setting a blanket "allow dangerous" variable and forgetting it.
	t.Setenv("CORGI_MCP_ALLOW_DANGEROUS_TUNNEL", "")
	mcpTunnelPrivate.Store(true)
	t.Cleanup(func() { mcpTunnelPrivate.Store(false) })

	if !dangerousTunnelToolsAllowed(true /* publicTunnel */) {
		t.Fatal("a tunnel verified to be behind an identity proxy must not block the tools")
	}
}

func TestExposureTiers(t *testing.T) {
	t.Cleanup(func() {
		mcpPublicTunnelActive.Store(false)
		mcpTunnelPrivate.Store(false)
	})

	mcpPublicTunnelActive.Store(false)
	mcpTunnelPrivate.Store(false)
	if got := mcpExposure(); got != tunnel.ExposureLocal {
		t.Errorf("no tunnel: exposure = %q, want %q", got, tunnel.ExposureLocal)
	}

	mcpPublicTunnelActive.Store(true)
	if got := mcpExposure(); got != tunnel.ExposurePublic {
		t.Errorf("unverified tunnel: exposure = %q, want %q", got, tunnel.ExposurePublic)
	}

	mcpTunnelPrivate.Store(true)
	if got := mcpExposure(); got != tunnel.ExposurePrivate {
		t.Errorf("verified tunnel: exposure = %q, want %q", got, tunnel.ExposurePrivate)
	}
}

func TestProbeTunnelExposureLeavesTheGateClosedOnAPublicEndpoint(t *testing.T) {
	// The probe is the only thing that may open the gate, so a public endpoint
	// running through it must leave the flag exactly as it found it.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	mcpTunnelPrivate.Store(false)
	t.Cleanup(func() { mcpTunnelPrivate.Store(false) })

	probeTunnelExposure(context.Background(), srv.URL, noWait)

	if mcpTunnelPrivate.Load() {
		t.Fatal("probing a public endpoint must not mark the tunnel private")
	}
}

func TestExposureIsMeasuredOnTheRouteTheToolsAreServedOn(t *testing.T) {
	// Making a non-browser MCP client work behind an identity proxy means
	// giving /mcp a service-token or bypass policy while / keeps redirecting to
	// the login page. Probing the root would see that redirect, call the whole
	// tunnel private, and re-enable corgi_exec on a route anyone with the URL
	// can reach.
	if got := mcpProbeTarget("https://kind-zebra-42.trycloudflare.com"); got != "https://kind-zebra-42.trycloudflare.com/mcp" {
		t.Errorf("mcpProbeTarget() = %q, want the /mcp route", got)
	}
	if got := mcpProbeTarget("https://corgi.example/"); got != "https://corgi.example/mcp" {
		t.Errorf("mcpProbeTarget() = %q, want no doubled slash", got)
	}
}

func TestExposureProbeUsesTheURLItIsGiven(t *testing.T) {
	var probed string
	mcpTunnelPrivate.Store(false)
	original := exposureProbe
	exposureProbe = func(_ context.Context, url string) tunnel.AccessResult {
		probed = url
		return tunnel.AccessResult{}
	}
	t.Cleanup(func() {
		exposureProbe = original
		mcpTunnelPrivate.Store(false)
	})

	probeTunnelExposure(context.Background(), mcpProbeTarget("https://corgi.example"), noWait)

	if probed != "https://corgi.example/mcp" {
		t.Errorf("probed %q, want the gated route", probed)
	}
	if mcpTunnelPrivate.Load() {
		t.Error("an unprotected result must leave the endpoint public")
	}
}

func TestAnUnauthenticatedRequestReachingMCPStaysPublic(t *testing.T) {
	// The bypassed-/mcp case, end to end: / redirects to the Access login but
	// /mcp answers, so the endpoint is reachable and must not be called private.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mcp" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, "/cdn-cgi/access/login", http.StatusFound)
	}))
	defer srv.Close()

	root := tunnel.ProbeAccessWith(context.Background(), srv.URL, srv.Client())
	if !root.Protected {
		t.Fatal("the root is protected in this setup; the test server is not set up as intended")
	}

	got := tunnel.ProbeAccessWith(context.Background(), mcpProbeTarget(srv.URL), srv.Client())
	if got.Protected {
		t.Error("an unauthenticated request that reaches /mcp must leave the endpoint public")
	}
}

func TestCorgisOwn401DoesNotMarkTheEndpointPrivate(t *testing.T) {
	// corgi's bearer check writes 401 with no WWW-Authenticate at all, which is
	// exactly what probing an unprotected /mcp sees.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()

	got := tunnel.ProbeAccessWith(context.Background(), mcpProbeTarget(srv.URL), srv.Client())

	if got.Protected {
		t.Errorf("corgi's own 401 must never count as an identity proxy (detail: %s)", got.Detail)
	}
}

// noWait skips the DNS-propagation delay: these tests point at a live server
// whose name already resolves.
func noWait(context.Context, time.Duration) bool { return true }
