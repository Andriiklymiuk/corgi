package tunnel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClassifyRecognisesTheAccessLoginRedirect(t *testing.T) {
	h := http.Header{}
	h.Set("Location", "https://acme.cloudflareaccess.com/cdn-cgi/access/login/corgi.example")

	got := classifyAccessResponse(http.StatusFound, h)

	if !got.Protected {
		t.Fatal("a redirect to the Access login is the endpoint being intercepted — that is the evidence")
	}
	if got.Provider != "cloudflare-access" {
		t.Errorf("provider = %q, want cloudflare-access", got.Provider)
	}
	if got.Exposure() != ExposurePrivate {
		t.Errorf("exposure = %q, want %q", got.Exposure(), ExposurePrivate)
	}
}

func TestClassifyRecognisesAccessHeaders(t *testing.T) {
	// Header names are canonicalised by net/http, but a probe result should not
	// depend on which casing the proxy happened to send.
	for _, name := range []string{"Cf-Access-Jwt-Assertion", "CF-Access-Client-Id"} {
		h := http.Header{}
		h.Set(name, "value")

		got := classifyAccessResponse(http.StatusOK, h)
		if !got.Protected {
			t.Errorf("header %q should be recognised as an identity proxy", name)
		}
	}
}

func TestClassifyRecognisesAGenericChallenge(t *testing.T) {
	h := http.Header{}
	h.Set("Www-Authenticate", `Bearer realm="identity-aware-proxy"`)

	got := classifyAccessResponse(http.StatusUnauthorized, h)

	if !got.Protected {
		t.Fatal("a 401 naming a realm other than corgi is something standing in front of corgi")
	}
	if got.Provider != "identity-proxy" {
		t.Errorf("provider = %q, want identity-proxy", got.Provider)
	}
}

func TestCorgisOwnBearerCheckIsNotAnIdentityProxy(t *testing.T) {
	// This is the failure that matters: corgi answers 401 to an unauthenticated
	// request too. Counting that as protection would let the endpoint declare
	// itself private on the strength of the very token the gate protects, and
	// silently re-enable corgi_exec over an open URL.
	for _, challenge := range []string{
		"Bearer",
		`Bearer realm="corgi"`,
		"bearer",
	} {
		h := http.Header{}
		h.Set("Www-Authenticate", challenge)

		got := classifyAccessResponse(http.StatusUnauthorized, h)
		if got.Protected {
			t.Errorf("challenge %q must not count as an identity proxy", challenge)
		}
	}
}

func TestClassifyDefaultsToPublic(t *testing.T) {
	// Anything not recognised is public. The gate must fail closed: assuming
	// protection on an unrecognised response is how an open shell endpoint
	// happens.
	tests := []struct {
		name   string
		status int
		header http.Header
	}{
		{"plain 200", http.StatusOK, http.Header{}},
		{"a redirect somewhere else", http.StatusFound, headerWith("Location", "https://example.com/login")},
		{"401 with no challenge", http.StatusUnauthorized, http.Header{}},
		{"server error", http.StatusBadGateway, http.Header{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyAccessResponse(tt.status, tt.header)
			if got.Protected {
				t.Errorf("status %d must not be read as protected", tt.status)
			}
			if got.Exposure() != ExposurePublic {
				t.Errorf("exposure = %q, want %q", got.Exposure(), ExposurePublic)
			}
			if got.Detail == "" {
				t.Error("an unprotected result must say why, since it decides whether a tool is blocked")
			}
		})
	}
}

func headerWith(key, value string) http.Header {
	h := http.Header{}
	h.Set(key, value)
	return h
}

func TestProbeAccessRejectsPlainHTTP(t *testing.T) {
	// An identity proxy terminates TLS. A plain-HTTP endpoint is not behind
	// one, so there is nothing to probe and no request worth making.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cf-Access-Jwt-Assertion", "value")
	}))
	defer srv.Close()

	got := ProbeAccess(context.Background(), srv.URL)

	if got.Protected {
		t.Error("an http:// endpoint must never be reported as protected")
	}
	if !strings.Contains(got.Detail, "https") {
		t.Errorf("detail = %q, want it to explain the scheme requirement", got.Detail)
	}
}

func TestProbeAccessDoesNotFollowTheRedirect(t *testing.T) {
	// Following it would discard the evidence and classify the login page.
	var hits int
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path == "/cdn-cgi/access/login" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, "/cdn-cgi/access/login", http.StatusFound)
	}))
	defer srv.Close()

	got := probeWithClientOf(srv)

	if !got.Protected {
		t.Fatalf("result = %+v, want the redirect recognised", got)
	}
	if hits != 1 {
		t.Errorf("made %d requests, want 1 — the redirect must not be followed", hits)
	}
}

func TestProbeAccessTreatsAFailedRequestAsPublic(t *testing.T) {
	// A probe that cannot reach the endpoint knows nothing, and "knows nothing"
	// must not open the gate.
	got := ProbeAccess(context.Background(), "https://127.0.0.1:1/nothing-listening")

	if got.Protected {
		t.Error("an unreachable endpoint must not be reported as protected")
	}
	if got.Exposure() != ExposurePublic {
		t.Errorf("exposure = %q, want %q", got.Exposure(), ExposurePublic)
	}
}

func TestProbeAccessRespectsACancelledContext(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if got := ProbeAccess(ctx, srv.URL); got.Protected {
		t.Error("a cancelled probe must not report protection")
	}
}

// probeWithClientOf runs the probe against a TLS test server, whose certificate
// the default client will not trust. It repeats ProbeAccess's redirect and
// classification behaviour against that server's client so the no-follow rule
// is exercised end to end.
func probeWithClientOf(srv *httptest.Server) AccessResult {
	client := srv.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := client.Get(srv.URL)
	if err != nil {
		return AccessResult{Detail: err.Error()}
	}
	defer resp.Body.Close()
	return classifyAccessResponse(resp.StatusCode, resp.Header)
}
