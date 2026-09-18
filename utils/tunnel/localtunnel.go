package tunnel

import (
	"fmt"
	"regexp"
	"strings"
)

type Localtunnel struct{}

func (Localtunnel) Name() string { return "localtunnel" }

func (Localtunnel) Cmd(port int) []string {
	return []string{"lt", "--port", fmt.Sprintf("%d", port)}
}

var localtunnelURLRe = regexp.MustCompile(`https://[a-z0-9-]+\.(?:localtunnel\.me|loca\.lt)`)

func (Localtunnel) ExtractURL(line string) string { return localtunnelURLRe.FindString(line) }

func (Localtunnel) InstallHint() string {
	return "npm install -g localtunnel  (or: brew install localtunnel)"
}

func (Localtunnel) AcceptsStdin() bool { return false }

func (Localtunnel) PreflightAuth() error { return nil }

func (Localtunnel) CmdNamed(port int, cfg NamedConfig) ([]string, error) {
	sub := cfg.Hostname
	for _, suffix := range []string{".localtunnel.me", ".loca.lt"} {
		sub = strings.TrimSuffix(sub, suffix)
	}
	if sub == "" || strings.ContainsAny(sub, "./:") {
		return nil, fmt.Errorf("localtunnel hostname must be a bare subdomain label (got %q); example: my-api", cfg.Hostname)
	}
	return []string{"lt", "--port", fmt.Sprintf("%d", port), "--subdomain", sub}, nil
}

func (Localtunnel) PreflightNamedAuth(cfg NamedConfig) error { return nil }
