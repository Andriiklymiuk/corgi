package cmd

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type rewriteTransport struct {
	target string
	inner  http.RoundTripper
}

func (rt rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = "http"
	clone.URL.Host = rt.target
	return rt.inner.RoundTrip(clone)
}

func publicResolver(ctx context.Context, host string) ([]net.IPAddr, error) {
	return []net.IPAddr{{IP: net.ParseIP("160.79.104.10")}}, nil
}

const testCIMDID = "https://claude.ai/oauth/claude-code-client-metadata"

func cimdTestServer(t *testing.T, handler http.HandlerFunc) (*cimdFetcher, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	f := newCIMDFetcher()
	f.resolve = publicResolver
	f.newTransport = func(string) http.RoundTripper {
		return rewriteTransport{target: strings.TrimPrefix(srv.URL, "http://"), inner: http.DefaultTransport}
	}
	return f, srv
}

func goodCIMD(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"client_id":"` + testCIMDID + `","client_name":"Claude Code","redirect_uris":["http://localhost/callback","http://127.0.0.1/callback"]}`))
}

func TestCIMDFetchesAndCaches(t *testing.T) {
	t.Setenv("CORGI_MCP_OAUTH_CLIENT_HOSTS", "")
	hosts := newOAuthClientHosts(nil, nil)
	calls := 0
	f, _ := cimdTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Accept") != "application/json" {
			t.Error("Accept header missing")
		}
		goodCIMD(w, r)
	})
	c, err := f.client(context.Background(), testCIMDID, hosts)
	if err != nil {
		t.Fatal(err)
	}
	if c.ID != testCIMDID || c.Name != "Claude Code" || len(c.RedirectURIs) != 2 {
		t.Errorf("client = %+v", c)
	}
	if _, err := f.client(context.Background(), testCIMDID, hosts); err != nil || calls != 1 {
		t.Errorf("second call must hit the cache; calls = %d, err = %v", calls, err)
	}
}

func TestCIMDSSRFMatrix(t *testing.T) {
	t.Setenv("CORGI_MCP_OAUTH_CLIENT_HOSTS", "")
	hosts := newOAuthClientHosts(nil, nil)
	f, _ := cimdTestServer(t, goodCIMD)
	for _, ips := range [][]string{
		{"10.0.0.1"}, {"127.0.0.1"}, {"169.254.1.1"}, {"fc00::1"}, {"::ffff:10.0.0.1"},
		{"160.79.104.10", "192.168.1.1"}, {"0.0.0.0"}, {"::1"}, {"224.0.0.1"}, {"::"},
	} {
		f.resolve = func(context.Context, string) ([]net.IPAddr, error) {
			var out []net.IPAddr
			for _, s := range ips {
				out = append(out, net.IPAddr{IP: net.ParseIP(s)})
			}
			return out, nil
		}
		if _, err := f.client(context.Background(), testCIMDID, hosts); !errors.Is(err, errCIMDRefused) || !strings.Contains(err.Error(), "non-public") {
			t.Errorf("%v must be refused as non-public, got %v", ips, err)
		}
	}
	f.resolve = func(context.Context, string) ([]net.IPAddr, error) { return nil, errors.New("nxdomain") }
	if _, err := f.client(context.Background(), testCIMDID, hosts); !errors.Is(err, errCIMDRefused) {
		t.Errorf("unresolvable host = %v", err)
	}
}

func TestCIMDRefusals(t *testing.T) {
	t.Setenv("CORGI_MCP_OAUTH_CLIENT_HOSTS", "")
	hosts := newOAuthClientHosts(nil, nil)
	cases := map[string]struct {
		id      string
		handler http.HandlerFunc
		want    string
	}{
		"redirect": {testCIMDID, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://claude.ai/other", http.StatusFound)
		}, "redirect"},
		"too big": {testCIMDID, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"client_id":"` + testCIMDID + `","client_name":"` + strings.Repeat("x", 65*1024) + `","redirect_uris":["http://localhost/cb"]}`))
		}, "over"},
		"id mismatch": {testCIMDID, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"client_id":"https://claude.ai/other","client_name":"x","redirect_uris":["http://localhost/cb"]}`))
		}, "does not match"},
		"bad redirect uri": {testCIMDID, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"client_id":"` + testCIMDID + `","client_name":"x","redirect_uris":["https://evil.example/cb"]}`))
		}, "not allowed"},
		"private_key_jwt": {testCIMDID, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"client_id":"` + testCIMDID + `","client_name":"x","redirect_uris":["http://localhost/cb"],"token_endpoint_auth_method":"private_key_jwt"}`))
		}, "not supported"},
		"no name": {testCIMDID, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"client_id":"` + testCIMDID + `","redirect_uris":["http://localhost/cb"]}`))
		}, "client_name"},
		"not json":         {testCIMDID, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`<html>`)) }, "JSON"},
		"404":              {testCIMDID, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(404) }, "404"},
		"host not allowed": {"https://evil.example/client", goodCIMD, "not an allowed client host"},
		"http scheme":      {"http://claude.ai/client", goodCIMD, "https URL"},
		"no path":          {"https://claude.ai", goodCIMD, "https URL"},
		"odd port":         {"https://claude.ai:8443/client", goodCIMD, "port 443"},
	}
	for name, c := range cases {
		f, _ := cimdTestServer(t, c.handler)
		_, err := f.client(context.Background(), c.id, hosts)
		if !errors.Is(err, errCIMDRefused) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want containing %q", name, err, c.want)
		}
	}
}

func TestCIMDCacheControl(t *testing.T) {
	t.Setenv("CORGI_MCP_OAUTH_CLIENT_HOSTS", "")
	hosts := newOAuthClientHosts(nil, nil)
	header := "no-store"
	calls := 0
	f, _ := cimdTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Cache-Control", header)
		goodCIMD(w, r)
	})
	_, _ = f.client(context.Background(), testCIMDID, hosts)
	_, _ = f.client(context.Background(), testCIMDID, hosts)
	if calls != 2 {
		t.Errorf("no-store must not cache; calls = %d", calls)
	}
	header = "public, max-age=99999"
	if _, err := f.client(context.Background(), testCIMDID, hosts); err != nil {
		t.Fatal(err)
	}
	e := f.cache[testCIMDID]
	if ttl := e.expires.Sub(f.now()); ttl > cimdMaxTTL+time.Second || ttl < cimdMaxTTL-time.Minute {
		t.Errorf("max-age must be capped at a day, got %v", ttl)
	}
	if ttl, ok := cimdTTL(""); !ok || ttl != cimdDefaultTTL {
		t.Errorf("absent header = %v %v", ttl, ok)
	}
	if ttl, ok := cimdTTL("max-age=120"); !ok || ttl != 2*time.Minute {
		t.Errorf("max-age=120 = %v %v", ttl, ok)
	}
}

func TestCIMDFetchBucket(t *testing.T) {
	t.Setenv("CORGI_MCP_OAUTH_CLIENT_HOSTS", "")
	hosts := newOAuthClientHosts(nil, nil)
	f, _ := cimdTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		goodCIMD(w, r)
	})
	now := time.Now()
	f.now = func() time.Time { return now }
	for i := 0; i < cimdFetchesPerMin; i++ {
		if _, err := f.client(context.Background(), testCIMDID, hosts); err != nil {
			t.Fatalf("fetch %d: %v", i, err)
		}
	}
	if _, err := f.client(context.Background(), testCIMDID, hosts); err == nil || !strings.Contains(err.Error(), "too many") {
		t.Errorf("11th fetch in a minute = %v", err)
	}
}
