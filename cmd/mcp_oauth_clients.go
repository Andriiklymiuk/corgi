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

var builtinOAuthClientHosts = []string{"claude.ai", "claude.com"}

type oauthClientHosts struct {
	hosts []string
}

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
				fmt.Fprintf(warn, "corgi mcp: ignoring OAuth client host %q - a host needs at least one dot\n", raw)
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

func (a *oauthClientHosts) allowsHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, h := range a.hosts {
		if host == h || strings.HasSuffix(host, "."+h) {
			return true
		}
	}
	return false
}

func isLoopbackHost(host string) bool {
	host = strings.ToLower(host)
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

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
