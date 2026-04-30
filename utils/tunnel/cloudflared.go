package tunnel

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Cloudflared uses Cloudflare Quick Tunnels (no signup, free).
// `cloudflared tunnel --url http://localhost:<port>` writes lines like:
//
//	2026-04-30T12:34:56Z INF |  https://kind-zebra-42.trycloudflare.com  |
//
// to stderr. We grep the line for any *.trycloudflare.com URL.
type Cloudflared struct{}

func (Cloudflared) Name() string { return "cloudflared" }

func (Cloudflared) Cmd(port int) []string {
	return []string{"cloudflared", "tunnel", "--url", fmt.Sprintf("http://localhost:%d", port)}
}

var cloudflaredURLRe = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)

func (Cloudflared) ExtractURL(line string) string {
	return cloudflaredURLRe.FindString(line)
}

func (Cloudflared) InstallHint() string { return "brew install cloudflared" }

func (Cloudflared) AcceptsStdin() bool { return false }

func (Cloudflared) PreflightAuth() error { return nil }

func (Cloudflared) CmdNamed(port int, cfg NamedConfig) ([]string, error) {
	if cfg.Name == "" {
		return nil, fmt.Errorf("cloudflared named tunnel requires `tunnel.name` (must exist via `cloudflared tunnel create <name>`)")
	}
	return []string{
		"cloudflared", "tunnel",
		"--url", fmt.Sprintf("http://localhost:%d", port),
		"run", cfg.Name,
	}, nil
}

func (Cloudflared) PreflightNamedAuth(cfg NamedConfig) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("can't resolve home dir: %w", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".cloudflared", "cert.pem")); err != nil {
		return fmt.Errorf(`cloudflared not logged in (no ~/.cloudflared/cert.pem).

Run once:

    cloudflared tunnel login`)
	}
	out, err := exec.Command("cloudflared", "tunnel", "list").Output()
	if err != nil {
		return fmt.Errorf("`cloudflared tunnel list` failed: %w", err)
	}
	if !strings.Contains(string(out), cfg.Name) {
		return fmt.Errorf(`cloudflared tunnel %q not found.

Create it once + route DNS:

    cloudflared tunnel create %s
    cloudflared tunnel route dns %s %s`, cfg.Name, cfg.Name, cfg.Name, cfg.Hostname)
	}
	return nil
}
