package cmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

	probeTunnelExposure(context.Background(), srv.URL)

	if mcpTunnelPrivate.Load() {
		t.Fatal("probing a public endpoint must not mark the tunnel private")
	}
}
