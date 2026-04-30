package tunnel

import (
	"fmt"
	"regexp"
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

// PreflightAuth — Quick Tunnels need no auth. Cloudflared CLI prints a
// random *.trycloudflare.com URL anonymously. Return nil unconditionally.
// (When/if we add a --named flag for stable URLs, switch this to check
// `~/.cloudflared/cert.pem` and prompt for `cloudflared tunnel login`.)
func (Cloudflared) PreflightAuth() error { return nil }
