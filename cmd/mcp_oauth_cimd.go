package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	cimdMaxBody       = 64 << 10
	cimdTimeout       = 5 * time.Second
	cimdDefaultTTL    = time.Hour
	cimdMaxTTL        = 24 * time.Hour
	cimdMaxEntries    = 50
	cimdFetchesPerMin = 10
)

var errCIMDRefused = errors.New("client metadata document refused")

type cimdEntry struct {
	client  oauthClient
	expires time.Time
}

type cimdFetcher struct {
	mu           sync.Mutex
	cache        map[string]cimdEntry
	bucket       rateBucket
	now          func() time.Time
	resolve      func(ctx context.Context, host string) ([]net.IPAddr, error)
	newTransport func(pinned string) http.RoundTripper
}

func newCIMDFetcher() *cimdFetcher {
	return &cimdFetcher{
		cache:   map[string]cimdEntry{},
		bucket:  rateBucket{capacity: cimdFetchesPerMin, perSecond: float64(cimdFetchesPerMin) / 60},
		now:     time.Now,
		resolve: net.DefaultResolver.LookupIPAddr,
		newTransport: func(pinned string) http.RoundTripper {
			dialer := &net.Dialer{Timeout: cimdTimeout}
			return &http.Transport{
				DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
					return dialer.DialContext(ctx, network, pinned)
				},
				DisableKeepAlives: true,
			}
		},
	}
}

func isCIMDClientID(clientID string) bool {
	u, err := url.Parse(clientID)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.Path != "" && u.Path != "/"
}

func publicIP(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}
	switch {
	case ip.IsLoopback(), ip.IsPrivate(), ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast(),
		ip.IsInterfaceLocalMulticast(), ip.IsMulticast(), ip.IsUnspecified():
		return false
	}
	for _, cidr := range reservedV4 {
		if cidr.Contains(ip) {
			return false
		}
	}
	return true
}

var reservedV4 = func() []*net.IPNet {
	var out []*net.IPNet
	for _, c := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15", "240.0.0.0/4"} {
		_, n, _ := net.ParseCIDR(c)
		out = append(out, n)
	}
	return out
}()

func (f *cimdFetcher) client(ctx context.Context, clientID string, hosts *oauthClientHosts) (oauthClient, error) {
	u, err := url.Parse(clientID)
	if err != nil || !isCIMDClientID(clientID) {
		return oauthClient{}, fmt.Errorf("%w: client_id is not an https URL with a path", errCIMDRefused)
	}
	if port := u.Port(); port != "" && port != "443" {
		return oauthClient{}, fmt.Errorf("%w: only port 443", errCIMDRefused)
	}
	if u.User != nil || u.Fragment != "" {
		return oauthClient{}, fmt.Errorf("%w: no credentials or fragment in client_id", errCIMDRefused)
	}
	host := u.Hostname()
	if !hosts.allowsHost(host) {
		return oauthClient{}, fmt.Errorf("%w: %s is not an allowed client host", errCIMDRefused, host)
	}

	f.mu.Lock()
	now := f.now()
	if e, ok := f.cache[clientID]; ok && now.Before(e.expires) {
		f.mu.Unlock()
		return e.client, nil
	}
	if !f.bucket.take(now) {
		f.mu.Unlock()
		return oauthClient{}, fmt.Errorf("%w: too many metadata fetches; try again shortly", errCIMDRefused)
	}
	f.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, cimdTimeout)
	defer cancel()
	addrs, err := f.resolve(ctx, host)
	if err != nil || len(addrs) == 0 {
		return oauthClient{}, fmt.Errorf("%w: %s does not resolve", errCIMDRefused, host)
	}
	for _, a := range addrs {
		if !publicIP(a.IP) {
			return oauthClient{}, fmt.Errorf("%w: %s resolves to a non-public address", errCIMDRefused, host)
		}
	}
	pinned := net.JoinHostPort(addrs[0].IP.String(), "443")
	httpClient := &http.Client{
		Transport: f.newTransport(pinned),
		Timeout:   cimdTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return fmt.Errorf("%w: redirects are not followed", errCIMDRefused)
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clientID, nil)
	if err != nil {
		return oauthClient{}, fmt.Errorf("%w: %v", errCIMDRefused, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "corgi-mcp")
	resp, err := httpClient.Do(req)
	if err != nil {
		return oauthClient{}, fmt.Errorf("%w: fetch failed: %v", errCIMDRefused, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return oauthClient{}, fmt.Errorf("%w: document returned %d", errCIMDRefused, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, cimdMaxBody+1))
	if err != nil {
		return oauthClient{}, fmt.Errorf("%w: %v", errCIMDRefused, err)
	}
	if len(body) > cimdMaxBody {
		return oauthClient{}, fmt.Errorf("%w: document over %d bytes", errCIMDRefused, cimdMaxBody)
	}
	c, err := parseCIMD(body, clientID, hosts)
	if err != nil {
		return oauthClient{}, err
	}
	c.CreatedAt = now
	if ttl, ok := cimdTTL(resp.Header.Get("Cache-Control")); ok {
		f.mu.Lock()
		if len(f.cache) >= cimdMaxEntries {
			f.evictOneLocked()
		}
		f.cache[clientID] = cimdEntry{client: c, expires: now.Add(ttl)}
		f.mu.Unlock()
	}
	return c, nil
}

func (f *cimdFetcher) evictOneLocked() {
	var victim string
	var soonest time.Time
	for k, e := range f.cache {
		if victim == "" || e.expires.Before(soonest) {
			victim, soonest = k, e.expires
		}
	}
	delete(f.cache, victim)
}

func cimdTTL(header string) (time.Duration, bool) {
	ttl := cimdDefaultTTL
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(strings.ToLower(part))
		if part == "no-store" || part == "no-cache" {
			return 0, false
		}
		if v, ok := strings.CutPrefix(part, "max-age="); ok {
			if n, err := strconv.Atoi(v); err == nil {
				if n <= 0 {
					return 0, false
				}
				ttl = time.Duration(n) * time.Second
			}
		}
	}
	return min(ttl, cimdMaxTTL), true
}

type cimdDocument struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

func parseCIMD(body []byte, clientID string, hosts *oauthClientHosts) (oauthClient, error) {
	var doc cimdDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return oauthClient{}, fmt.Errorf("%w: document is not a JSON object", errCIMDRefused)
	}
	if doc.ClientID != clientID {
		return oauthClient{}, fmt.Errorf("%w: client_id in the document does not match its URL", errCIMDRefused)
	}
	name := cleanClientName(doc.ClientName, "")
	if name == "" {
		return oauthClient{}, fmt.Errorf("%w: client_name is required", errCIMDRefused)
	}
	if len(doc.RedirectURIs) == 0 {
		return oauthClient{}, fmt.Errorf("%w: redirect_uris is required", errCIMDRefused)
	}
	for _, u := range doc.RedirectURIs {
		if !hosts.allowedRedirectURI(u) {
			return oauthClient{}, fmt.Errorf("%w: redirect URI not allowed: %s", errCIMDRefused, u)
		}
	}
	if m := doc.TokenEndpointAuthMethod; m != "" && m != "none" {
		return oauthClient{}, fmt.Errorf("%w: token_endpoint_auth_method %s is not supported (only none)", errCIMDRefused, m)
	}
	return oauthClient{ID: clientID, Name: name, RedirectURIs: doc.RedirectURIs}, nil
}
