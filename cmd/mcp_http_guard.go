package cmd

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
)

// originAllowlist decides which browser Origins may talk to /mcp. A request
// with no Origin is server-to-server (Claude's backend, curl, an MCP client
// binary) and passes; a request that carries one is a page in a browser and
// must be a known page, or it could be a DNS-rebinding attack on localhost.
type originAllowlist struct {
	mu    sync.RWMutex
	exact map[string]bool
}

// newOriginAllowlist allows every loopback origin on any port, the listen
// address itself, and each extra origin (exact scheme://host[:port]).
func newOriginAllowlist(listenAddr string, extra []string) *originAllowlist {
	a := &originAllowlist{exact: map[string]bool{}}
	if listenAddr != "" {
		a.add("http://" + localURL(listenAddr))
	}
	for _, o := range extra {
		a.add(o)
	}
	return a
}

// add records the origin of raw (a URL or a bare origin); a value that does
// not parse is ignored rather than opening the list to everything.
func (a *originAllowlist) add(raw string) {
	o := canonicalOrigin(raw)
	if o == "" {
		return
	}
	a.mu.Lock()
	a.exact[o] = true
	a.mu.Unlock()
}

func (a *originAllowlist) allows(origin string) bool {
	o := canonicalOrigin(origin)
	if o == "" {
		return false
	}
	if isLoopbackOrigin(o) {
		return true
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.exact[o]
}

// canonicalOrigin lowercases scheme and host and drops any path, so
// "https://APP.example.com/mcp" and "https://app.example.com" compare equal.
// Anything that is not scheme://host[:port] (including "null") is "".
func canonicalOrigin(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	return scheme + "://" + strings.ToLower(u.Host)
}

func isLoopbackOrigin(canonical string) bool {
	hostport := strings.TrimPrefix(strings.TrimPrefix(canonical, "http://"), "https://")
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

const (
	mcpProtocolVersionHeader = "MCP-Protocol-Version"
	mcpAllowedMethods        = "GET, POST, DELETE"
)

// mcpEndpointGuard wraps the MCP endpoint with the transport checks the
// Streamable HTTP spec puts on the server: only the methods the transport
// defines, a known Origin, and a protocol version it speaks. It runs before
// authentication so a rebinding page is refused before a token is ever
// compared.
func mcpEndpointGuard(allow *originAllowlist, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodGet, http.MethodDelete:
		default:
			w.Header().Set("Allow", mcpAllowedMethods)
			http.Error(w, "method not allowed on the MCP endpoint", http.StatusMethodNotAllowed)
			return
		}
		if origin, present := r.Header["Origin"]; present && (len(origin) != 1 || !allow.allows(origin[0])) {
			w.Header().Set(headerContentType, mimeJSON)
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"origin_forbidden"}`))
			return
		}
		if v := r.Header.Get(mcpProtocolVersionHeader); v != "" && !slices.Contains(mcp.ValidProtocolVersions, v) {
			w.Header().Set(headerContentType, mimeJSON)
			w.WriteHeader(http.StatusBadRequest)
			msg := fmt.Sprintf("unsupported %s %q; supported: %s", mcpProtocolVersionHeader, v, strings.Join(mcp.ValidProtocolVersions, ", "))
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":null,"error":{"code":%d,"message":%q}}`, mcp.INVALID_REQUEST, msg)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// resolveMCPListenAddr applies the loopback default: a bare port or an empty
// host binds 127.0.0.1, an explicit non-loopback host needs bindAll. The
// notice, when set, tells the operator what happened and how to change it.
func resolveMCPListenAddr(addr string, bindAll bool) (listen string, notice string, err error) {
	addr = strings.TrimSpace(addr)
	if addr != "" && !strings.Contains(addr, ":") {
		addr = ":" + addr
	}
	host, port, splitErr := net.SplitHostPort(addr)
	if splitErr != nil {
		return "", "", fmt.Errorf("--http wants host:port or a port, got %q", addr)
	}
	if n, err := strconv.Atoi(port); err != nil || n < 0 || n > 65535 {
		return "", "", fmt.Errorf("--http wants a port number, got %q", port)
	}
	if bindAll {
		return addr, "", nil
	}
	if host == "" {
		return net.JoinHostPort("127.0.0.1", port), "corgi mcp bound to 127.0.0.1:" + port + " (pass --bind-all to listen on every interface)", nil
	}
	if host == "localhost" {
		return addr, "", nil
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil || !ip.IsLoopback() {
		return "", "", fmt.Errorf("--http %s is not a loopback address; pass --bind-all to listen there (the MCP endpoint then needs --token or a paired device)", addr)
	}
	return addr, "", nil
}
