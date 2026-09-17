package cmd

import (
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"unicode"
)

// cleanClientName makes a client's self-declared name safe to store as a
// device name and print in a terminal: graphic runes only (no escapes, no
// newlines), single spaces, at most 64 runes, cut on a rune boundary.
// Empty after cleaning → fallback.
func cleanClientName(raw, fallback string) string {
	var b strings.Builder
	space := false
	for _, r := range raw {
		if unicode.IsSpace(r) {
			space = true
			continue
		}
		if !unicode.IsGraphic(r) {
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	name := b.String()
	if runes := []rune(name); len(runes) > 64 {
		name = string(runes[:64])
	}
	if name == "" {
		return fallback
	}
	return name
}

// builtinOAuthClientHosts are the hosted callbacks corgi trusts out of the
// box: Claude's. A subdomain of either passes too.
var builtinOAuthClientHosts = []string{"claude.ai", "claude.com"}

// oauthClientHosts is the host allowlist for hosted redirect URIs and CIMD
// documents. Loopback needs no entry.
type oauthClientHosts struct {
	hosts []string
}

// newOAuthClientHosts builds the allowlist from the built-ins plus extras
// (the --oauth-client-host flag and CORGI_MCP_OAUTH_CLIENT_HOSTS, comma
// separated). A single-label entry such as "com" or "localhost" is dropped
// with a warning on warn, so a typo cannot open a whole TLD.
func newOAuthClientHosts(extra []string, warn io.Writer) *oauthClientHosts {
	a := &oauthClientHosts{}
	seen := map[string]bool{}
	add := func(raw string) {
		h := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(raw, ".")))
		if h == "" {
			return
		}
		if !strings.Contains(h, ".") {
			if warn != nil {
				fmt.Fprintf(warn, "corgi mcp: ignoring OAuth client host %q — a host needs at least one dot\n", raw)
			}
			return
		}
		if !seen[h] {
			seen[h] = true
			a.hosts = append(a.hosts, h)
		}
	}
	for _, h := range builtinOAuthClientHosts {
		add(h)
	}
	for _, h := range extra {
		for _, part := range strings.Split(h, ",") {
			add(part)
		}
	}
	for _, part := range strings.Split(os.Getenv("CORGI_MCP_OAUTH_CLIENT_HOSTS"), ",") {
		add(part)
	}
	return a
}

// allowsHost says whether host is an allowlisted host or a subdomain of one.
func (a *oauthClientHosts) allowsHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, h := range a.hosts {
		if host == h || strings.HasSuffix(host, "."+h) {
			return true
		}
	}
	return false
}

// isLoopbackHost is RFC 8252 §7.3's set: localhost and the two loopback
// literals. Nothing that merely resolves to loopback counts.
func isLoopbackHost(host string) bool {
	host = strings.ToLower(host)
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

// allowedRedirectURI is the one redirect rule DCR and CIMD share.
//
// Loopback: http or https to localhost / 127.0.0.1 / [::1] on any port —
// the port is a client's ephemeral choice and is ignored in comparisons.
// Hosted: https to an allowlisted host or a subdomain of one. Nothing else.
func (a *oauthClientHosts) allowedRedirectURI(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Fragment != "" || u.User != nil {
		return false
	}
	host := u.Hostname()
	if isLoopbackHost(host) {
		return u.Scheme == "http" || u.Scheme == "https"
	}
	return u.Scheme == "https" && a.allowsHost(host)
}

// sameRedirectURI compares a requested redirect URI with a registered one:
// byte for byte, except that a loopback URI may differ in its port.
func sameRedirectURI(registered, requested string) bool {
	if registered == requested {
		return true
	}
	a, errA := url.Parse(registered)
	b, errB := url.Parse(requested)
	if errA != nil || errB != nil {
		return false
	}
	if !isLoopbackHost(a.Hostname()) || !isLoopbackHost(b.Hostname()) {
		return false
	}
	return a.Scheme == b.Scheme && strings.EqualFold(a.Hostname(), b.Hostname()) &&
		a.Path == b.Path && a.RawQuery == b.RawQuery
}
