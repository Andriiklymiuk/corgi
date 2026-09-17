package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Paths corgi serves as resource server and authorization server for its own
// /mcp. RFC 9728 (protected resource) and RFC 8414 (AS metadata), both at
// the root and under the /mcp path, since Claude probes the path form first.
const (
	oauthPRMPath        = "/.well-known/oauth-protected-resource"
	oauthASMetadataPath = "/.well-known/oauth-authorization-server"
	oauthRegisterPath   = "/oauth/register"
	oauthAuthorizePath  = "/oauth/authorize"
	oauthTokenPath      = "/oauth/token"
	oauthRevokePath     = "/oauth/revoke"
	oauthApprovePath    = "/oauth/approve"
	oauthPendingPath    = "/oauth/pending/"
	oauthDocsURL        = "https://github.com/Andriiklymiuk/corgi/blob/main/docs/mcp.md"
)

// oauthServer is corgi as OAuth 2.1 authorization server for the connector.
// One instance per `corgi mcp --http`; every /oauth and /.well-known route
// hangs off it.
type oauthServer struct {
	mu sync.Mutex
	// issuer is the public origin, set once the tunnel resolves; before that
	// the loopback address. Never the request's Host header.
	issuer      string
	hosts       *oauthClientHosts
	origins     *originAllowlist
	deviceStore string
	now         func() time.Time
}

// newOAuthServer starts with the loopback issuer for listenAddr.
func newOAuthServer(listenAddr string, hosts *oauthClientHosts, origins *originAllowlist, deviceStore string) *oauthServer {
	return &oauthServer{
		issuer:      "http://" + localURL(listenAddr), // NOSONAR — loopback issuer until the tunnel resolves; OAuth 2.1 allows plain http only there
		hosts:       hosts,
		origins:     origins,
		deviceStore: deviceStore,
		now:         time.Now,
	}
}

// setIssuer records the public origin the tunnel gave us.
func (oa *oauthServer) setIssuer(raw string) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return
	}
	oa.mu.Lock()
	oa.issuer = raw
	oa.mu.Unlock()
}

func (oa *oauthServer) issuerURL() string {
	oa.mu.Lock()
	defer oa.mu.Unlock()
	return oa.issuer
}

// resourceMetadataURL is what the 401 challenge points at.
func (oa *oauthServer) resourceMetadataURL() string {
	return oa.issuerURL() + oauthPRMPath + "/mcp"
}

func (oa *oauthServer) protectedResourceMetadata() map[string]any {
	issuer := oa.issuerURL()
	return map[string]any{
		"resource":                 issuer + "/mcp",
		"authorization_servers":    []string{issuer},
		"bearer_methods_supported": []string{"header"},
		"resource_name":            "corgi",
		"resource_documentation":   oauthDocsURL,
	}
}

func (oa *oauthServer) authorizationServerMetadata() map[string]any {
	issuer := oa.issuerURL()
	return map[string]any{
		"issuer":                                issuer,
		"authorization_endpoint":                issuer + oauthAuthorizePath,
		"token_endpoint":                        issuer + oauthTokenPath,
		"registration_endpoint":                 issuer + oauthRegisterPath,
		"revocation_endpoint":                   issuer + oauthRevokePath,
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"client_id_metadata_document_supported": true,
	}
}

// metadataHandler serves one JSON document, cacheable for five minutes.
func (oa *oauthServer) metadataHandler(doc func() map[string]any) http.Handler {
	return oa.guard("GET, HEAD, OPTIONS", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(headerContentType, mimeJSON)
		w.Header().Set("Cache-Control", "max-age=300")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		body, _ := json.Marshal(doc())
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = w.Write(body)
	}))
}

// guard is the /mcp endpoint's method and Origin discipline for the OAuth
// routes: unlisted methods 405, a foreign Origin 403.
func (oa *oauthServer) guard(allow string, next http.Handler) http.Handler {
	methods := map[string]bool{}
	for _, m := range strings.Split(allow, ",") {
		methods[strings.TrimSpace(m)] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !methods[r.Method] {
			w.Header().Set("Allow", allow)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if origin, present := r.Header["Origin"]; present && (len(origin) != 1 || !oa.origins.allows(origin[0])) {
			w.Header().Set(headerContentType, mimeJSON)
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"origin_forbidden"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// mount registers every OAuth route on mux.
func (oa *oauthServer) mount(mux *http.ServeMux) {
	prm := oa.metadataHandler(oa.protectedResourceMetadata)
	as := oa.metadataHandler(oa.authorizationServerMetadata)
	mux.Handle(oauthPRMPath, prm)
	mux.Handle(oauthPRMPath+"/mcp", prm)
	mux.Handle(oauthASMetadataPath, as)
	mux.Handle(oauthASMetadataPath+"/mcp", as)
}

// oauthError writes an RFC 6749 §5.2 error body.
func oauthError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set(headerContentType, mimeJSON)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(status)
	body, _ := json.Marshal(map[string]string{"error": code, "error_description": description})
	_, _ = w.Write(body)
}

// authorizeAccessToken reports whether header carries a live access token
// corgi issued. Filled in with the token store; false until then.
func (oa *oauthServer) authorizeAccessToken(header string) bool {
	_ = header
	return false
}
