package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestAllowedRedirectURI(t *testing.T) {
	t.Setenv("CORGI_MCP_OAUTH_CLIENT_HOSTS", "")
	hosts := newOAuthClientHosts(nil, nil)
	cases := map[string]bool{
		"http://localhost/callback":               true,
		"http://localhost:53421/callback":         true,
		"http://127.0.0.1:8080/cb":                true,
		"https://127.0.0.1/cb":                    true,
		"http://[::1]:9/cb":                       true,
		"https://claude.ai/api/mcp/auth_callback": true,
		"https://x.claude.com/cb":                 true,
		"https://CLAUDE.AI/cb":                    true,
		"https://claude.ai.evil.example/cb":       false,
		"http://claude.ai/cb":                     false,
		"https://evil.example/cb":                 false,
		"https://localhost.evil.example/cb":       false,
		"https://claude.ai/cb#frag":               false,
		"https://user@claude.ai/cb":               false,
		"claude.ai/cb":                            false,
		"":                                        false,
		"http://10.0.0.1/cb":                      false,
		"https://notclaude.ai/cb":                 false,
	}
	for raw, want := range cases {
		if got := hosts.allowedRedirectURI(raw); got != want {
			t.Errorf("allowedRedirectURI(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestOAuthClientHostsExtras(t *testing.T) {
	t.Setenv("CORGI_MCP_OAUTH_CLIENT_HOSTS", "chatgpt.com, com")
	var warn bytes.Buffer
	hosts := newOAuthClientHosts([]string{"Example.Org.", "localhost"}, &warn)
	if !hosts.allowedRedirectURI("https://a.chatgpt.com/cb") || !hosts.allowedRedirectURI("https://example.org/cb") {
		t.Error("extras from flag and env must pass")
	}
	if hosts.allowedRedirectURI("https://anything.com/cb") {
		t.Error("the single-label 'com' must have been dropped")
	}
	if strings.Count(warn.String(), "ignoring OAuth client host") != 2 {
		t.Errorf("warnings = %q", warn.String())
	}
}

func TestSameRedirectURI(t *testing.T) {
	cases := []struct {
		reg, req string
		want     bool
	}{
		{"http://localhost/callback", "http://localhost:51234/callback", true},
		{"http://127.0.0.1/callback", "http://127.0.0.1:9/callback", true},
		{"http://localhost/callback", "http://localhost/other", false},
		{"http://localhost/callback", "https://localhost/callback", false},
		{"https://claude.ai/cb", "https://claude.ai/cb", true},
		{"https://claude.ai/cb", "https://claude.ai:444/cb", false},
		{"https://claude.ai/cb", "https://claude.ai/cb?x=1", false},
	}
	for _, c := range cases {
		if got := sameRedirectURI(c.reg, c.req); got != c.want {
			t.Errorf("sameRedirectURI(%q, %q) = %v", c.reg, c.req, got)
		}
	}
}
