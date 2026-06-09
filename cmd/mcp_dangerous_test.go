package cmd

import (
	"bytes"
	"strings"
	"testing"
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
