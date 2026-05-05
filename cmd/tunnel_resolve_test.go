package cmd

import (
	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/tunnel"
	"strings"
	"testing"
)

func TestResolveTunnelLiteralHostname(t *testing.T) {
	s := utils.Service{
		ServiceName: "api",
		Tunnel: &utils.TunnelConfig{
			Hostname: "api.example.com",
			Provider: "cloudflared",
		},
	}
	named, p, err := resolveTunnel(s, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if named.Hostname != "api.example.com" {
		t.Errorf("got %q", named.Hostname)
	}
	if p.Name() != "cloudflared" {
		t.Errorf("got %q", p.Name())
	}
}

func TestResolveTunnelDefaultProvider(t *testing.T) {
	s := utils.Service{
		ServiceName: "api",
		Tunnel: &utils.TunnelConfig{
			Hostname: "api.example.com",
		},
	}
	_, p, err := resolveTunnel(s, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "cloudflared" {
		t.Errorf("default should be cloudflared, got %q", p.Name())
	}
}

func TestResolveTunnelMissingEnv(t *testing.T) {
	s := utils.Service{
		ServiceName: "api",
		Tunnel: &utils.TunnelConfig{
			Hostname: "${MISSING_VAR_ZZZ}",
		},
	}
	_, _, err := resolveTunnel(s, nil, false)
	if err == nil || !strings.Contains(err.Error(), "MISSING_VAR_ZZZ") {
		t.Errorf("expected missing env error, got %v", err)
	}
}

func TestResolveTunnelFlagOverride(t *testing.T) {
	s := utils.Service{
		ServiceName: "api",
		Tunnel: &utils.TunnelConfig{
			Hostname: "api.example.com",
			Provider: "cloudflared",
		},
	}
	flagP := tunnel.Ngrok{}
	_, p, err := resolveTunnel(s, flagP, true)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "ngrok" {
		t.Errorf("flag should win, got %q", p.Name())
	}
}

func TestResolveTunnelUnknownProvider(t *testing.T) {
	s := utils.Service{
		ServiceName: "api",
		Tunnel: &utils.TunnelConfig{
			Hostname: "api.example.com",
			Provider: "noprovider",
		},
	}
	_, _, err := resolveTunnel(s, nil, false)
	if err == nil || !strings.Contains(err.Error(), "unknown tunnel.provider") {
		t.Errorf("got %v", err)
	}
}

func TestResolveTunnelEnvFromShell(t *testing.T) {
	t.Setenv("MY_HOST", "api.dev.example.com")
	s := utils.Service{
		ServiceName: "api",
		Tunnel: &utils.TunnelConfig{
			Hostname: "${MY_HOST}",
		},
	}
	named, _, err := resolveTunnel(s, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if named.Hostname != "api.dev.example.com" {
		t.Errorf("got %q", named.Hostname)
	}
}
