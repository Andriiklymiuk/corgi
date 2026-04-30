package tunnel

import (
	"fmt"
	"regexp"
)

// Localtunnel wraps the npm `localtunnel` CLI (`lt --port <port>`).
// It emits exactly one line: "your url is: https://<sub>.loca.lt".
type Localtunnel struct{}

func (Localtunnel) Name() string { return "localtunnel" }

func (Localtunnel) Cmd(port int) []string {
	return []string{"lt", "--port", fmt.Sprintf("%d", port)}
}

var localtunnelURLRe = regexp.MustCompile(`https://[a-z0-9-]+\.loca\.lt`)

func (Localtunnel) ExtractURL(line string) string { return localtunnelURLRe.FindString(line) }

func (Localtunnel) InstallHint() string { return "npm install -g localtunnel" }

func (Localtunnel) AcceptsStdin() bool { return false }

func (Localtunnel) PreflightAuth() error { return nil }

func (Localtunnel) CmdNamed(port int, cfg NamedConfig) ([]string, error) {
	return nil, fmt.Errorf("localtunnel doesn't support custom hostnames; remove `tunnel.hostname` or switch provider to cloudflared/ngrok")
}

func (Localtunnel) PreflightNamedAuth(cfg NamedConfig) error {
	return fmt.Errorf("localtunnel doesn't support named/stable URLs; switch provider to cloudflared or ngrok")
}
