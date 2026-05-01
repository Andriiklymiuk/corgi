package tunnel

import (
	"fmt"
	"regexp"
	"strings"
)

// Localtunnel wraps the npm `localtunnel` CLI (`lt --port <port>`).
// It emits exactly one line: "your url is: https://<sub>.localtunnel.me".
//
// Named mode: localtunnel supports `--subdomain <name>` to request a specific
// subdomain on the public server. Per the official README the request is
// best-effort — if the subdomain is in use, the server picks a different
// random one. Pass only the leading subdomain label as `tunnel.hostname`
// (e.g. `my-api`); the trailing `.localtunnel.me` is fixed.
type Localtunnel struct{}

func (Localtunnel) Name() string { return "localtunnel" }

func (Localtunnel) Cmd(port int) []string {
	return []string{"lt", "--port", fmt.Sprintf("%d", port)}
}

// Match URLs on either the canonical `localtunnel.me` host or the legacy
// short `loca.lt` mirror.
var localtunnelURLRe = regexp.MustCompile(`https://[a-z0-9-]+\.(?:localtunnel\.me|loca\.lt)`)

func (Localtunnel) ExtractURL(line string) string { return localtunnelURLRe.FindString(line) }

func (Localtunnel) InstallHint() string {
	return "npm install -g localtunnel  (or: brew install localtunnel)"
}

func (Localtunnel) AcceptsStdin() bool { return false }

func (Localtunnel) PreflightAuth() error { return nil }

// CmdNamed runs `lt --port <port> --subdomain <label>`. Hostname is
// expected as the bare subdomain label (no dots, no scheme). If a full
// `*.localtunnel.me` is passed, strip the suffix.
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

// PreflightNamedAuth: localtunnel needs no auth. Subdomain availability is
// best-effort — the server falls back to a random subdomain if requested
// label is taken. Return nil; the runner's URL extraction will print
// whatever subdomain was actually granted.
func (Localtunnel) PreflightNamedAuth(cfg NamedConfig) error { return nil }
