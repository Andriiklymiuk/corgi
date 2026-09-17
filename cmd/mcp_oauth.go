package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
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
	statePath   string
	state       *oauthState
	bucket      rateBucket
	codes       map[string]authCode
	pending     map[string]*pendingAuth
	cimd        *cimdFetcher
	now         func() time.Time
}

// pendingAuth is a consent page waiting for approval. Filled in by the
// authorize endpoint.
type pendingAuth struct {
	client        oauthClient
	redirectURI   string
	codeChallenge string
	state         string
	expires       time.Time
	code          string
}

// newOAuthServer starts with the loopback issuer for listenAddr. deviceStore
// is where access tokens are recorded as devices; statePath is oauth.json.
// An unreadable oauth.json is reported and treated as empty, never fatal:
// the header path must keep serving.
func newOAuthServer(listenAddr string, hosts *oauthClientHosts, origins *originAllowlist, deviceStore, statePath string) *oauthServer {
	state, err := loadOAuthState(statePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "corgi mcp: cannot read %s (%v); starting with no OAuth clients\n", statePath, err)
		state = &oauthState{}
	}
	return &oauthServer{
		issuer:      "http://" + localURL(listenAddr), // NOSONAR — loopback issuer until the tunnel resolves; OAuth 2.1 allows plain http only there
		hosts:       hosts,
		origins:     origins,
		deviceStore: deviceStore,
		statePath:   statePath,
		state:       state,
		bucket:      rateBucket{capacity: oauthRequestsPerMinute, perSecond: float64(oauthRequestsPerMinute) / 60},
		codes:       map[string]authCode{},
		pending:     map[string]*pendingAuth{},
		cimd:        newCIMDFetcher(),
		now:         time.Now,
	}
}

// oauthRequestsPerMinute is one bucket for register, authorize and token
// together. Behind a tunnel every request comes from the tunnel agent, so a
// per-IP limit would be the same bucket with more code.
const oauthRequestsPerMinute = 60

// rateBucket is a token bucket; the caller holds oa.mu.
type rateBucket struct {
	capacity  float64
	perSecond float64
	tokens    float64
	last      time.Time
}

func (b *rateBucket) take(now time.Time) bool {
	if b.last.IsZero() {
		b.tokens = b.capacity
	} else if elapsed := now.Sub(b.last); elapsed > 0 { // a clock step back refills nothing
		b.tokens = min(b.capacity, b.tokens+elapsed.Seconds()*b.perSecond)
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// limited answers 429 when the shared bucket is empty. Called with oa.mu
// held by the handler that owns the request.
func (oa *oauthServer) limitedLocked(w http.ResponseWriter) bool {
	if oa.bucket.take(oa.now()) {
		return false
	}
	w.Header().Set("Retry-After", "5")
	oauthError(w, http.StatusTooManyRequests, "temporarily_unavailable", "too many requests; try again in a few seconds")
	return true
}

func (oa *oauthServer) saveLocked() error {
	return saveOAuthState(oa.statePath, oa.state)
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
	mux.Handle(oauthRegisterPath, oa.guard("POST, OPTIONS", http.HandlerFunc(oa.registerHandler)))
	mux.Handle(oauthTokenPath, oa.guard("POST, OPTIONS", http.HandlerFunc(oa.tokenHandler)))
	mux.Handle(oauthRevokePath, oa.guard("POST, OPTIONS", http.HandlerFunc(oa.revokeHandler)))
}

// maxOAuthBody bounds a registration or token request body.
const maxOAuthBody = 64 << 10

// registerRequest is the RFC 7591 metadata corgi reads; the rest is ignored.
type registerRequest struct {
	RedirectURIs            []string `json:"redirect_uris"`
	ClientName              string   `json:"client_name"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

// registerHandler is dynamic client registration: a public client with
// allowlisted redirect URIs gets an id, no secret.
func (oa *oauthServer) registerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	oa.mu.Lock()
	defer oa.mu.Unlock()
	if oa.limitedLocked(w) {
		return
	}
	if ct := r.Header.Get(headerContentType); !strings.HasPrefix(ct, "application/json") {
		oauthError(w, http.StatusBadRequest, "invalid_request", "registration wants application/json, got "+strconv.Quote(ct))
		return
	}
	var req registerRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxOAuthBody)).Decode(&req); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "body is not valid JSON")
		return
	}
	if len(req.RedirectURIs) == 0 {
		oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uris is required")
		return
	}
	for _, u := range req.RedirectURIs {
		if !oa.hosts.allowedRedirectURI(u) {
			oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect URI not allowed: "+u+" (loopback, or https to claude.ai / claude.com / an --oauth-client-host)")
			return
		}
	}
	for _, g := range req.GrantTypes {
		if g != "authorization_code" && g != "refresh_token" {
			oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "unsupported grant_type "+g)
			return
		}
	}
	for _, rt := range req.ResponseTypes {
		if rt != "code" {
			oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "unsupported response_type "+rt)
			return
		}
	}
	if m := req.TokenEndpointAuthMethod; m != "" && m != "none" {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "token_endpoint_auth_method must be none; corgi issues no client secrets")
		return
	}
	name := strings.TrimSpace(req.ClientName)
	if name == "" {
		name = "MCP client"
	}
	if len(name) > 64 {
		name = name[:64]
	}
	id, err := randomToken("corgi_client_")
	if err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "could not mint a client id")
		return
	}
	now := oa.now()
	oa.state.addClient(oauthClient{ID: id, Name: name, RedirectURIs: req.RedirectURIs, CreatedAt: now}, now)
	if err := oa.saveLocked(); err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "could not record the client: "+err.Error())
		return
	}
	w.Header().Set(headerContentType, mimeJSON)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusCreated)
	body, _ := json.Marshal(map[string]any{
		"client_id":                  id,
		"client_id_issued_at":        now.Unix(),
		"client_name":                name,
		"redirect_uris":              req.RedirectURIs,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
	_, _ = w.Write(body)
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
