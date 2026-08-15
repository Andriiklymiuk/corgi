package tunnel

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Exposure is how reachable an endpoint is, which is a different question from
// whether it has a stable URL.
//
// corgi already treats "is there a tunnel" as a yes/no, and gates the mutating
// tools on it. That is too blunt: a named tunnel behind an identity proxy is
// not open to the internet, and a quick tunnel is open to anyone who guesses
// the hostname. Those deserve different answers.
type Exposure string

const (
	// ExposureLocal is a loopback or LAN listener with no tunnel at all.
	ExposureLocal Exposure = "local"
	// ExposurePrivate is a tunnel an identity proxy stands in front of, so an
	// unauthenticated request never reaches corgi.
	ExposurePrivate Exposure = "private"
	// ExposurePublic is a tunnel anyone holding the URL can reach.
	ExposurePublic Exposure = "public"
)

// AccessResult is what probing an endpoint learned.
type AccessResult struct {
	// Protected is true only when an identity proxy was actually observed
	// intercepting the request. Anything else — an error, a timeout, an
	// unrecognised response — leaves it false.
	Protected bool `json:"protected"`
	// Provider names what answered, for the line corgi prints.
	Provider string `json:"provider,omitempty"`
	// Detail says how it was recognised, or why it was not.
	Detail string `json:"detail,omitempty"`
}

// Exposure maps a probe result onto the tier a tunnelled endpoint sits in.
func (r AccessResult) Exposure() Exposure {
	if r.Protected {
		return ExposurePrivate
	}
	return ExposurePublic
}

// accessProbeTimeout bounds the probe. It runs while a tunnel is coming up and
// nothing waits on the result, but an unbounded request would keep a goroutine
// alive for the life of the process.
const accessProbeTimeout = 10 * time.Second

// ProbeAccess asks whether an identity proxy stands in front of a URL.
//
// The check is deliberately positive-only: corgi downgrades an endpoint to
// "private" solely on evidence that an unauthenticated request was intercepted.
// Guessing the other way — assuming protection because a probe failed, or
// because the config said so — would relax a security gate on a hunch, and the
// failure mode is an open shell endpoint.
//
// It follows no redirects, because the redirect itself is the evidence.
func ProbeAccess(ctx context.Context, rawURL string) AccessResult {
	if !strings.HasPrefix(rawURL, "https://") {
		// An identity proxy terminates TLS. Anything on plain HTTP is not
		// behind one, whatever it answers.
		return AccessResult{Detail: "not an https endpoint"}
	}

	ctx, cancel := context.WithTimeout(ctx, accessProbeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return AccessResult{Detail: err.Error()}
	}
	client := &http.Client{
		// The interception is what is being measured, so following it would
		// discard the answer and report on the login page instead.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return AccessResult{Detail: fmt.Sprintf("probe failed: %v", err)}
	}
	defer resp.Body.Close()

	return classifyAccessResponse(resp.StatusCode, resp.Header)
}

// classifyAccessResponse recognises an identity proxy from the response alone.
// Split out from the request so the recognition rules are testable without a
// network.
func classifyAccessResponse(status int, header http.Header) AccessResult {
	location := header.Get("Location")

	// Cloudflare Access redirects an unauthenticated request to its own login
	// path, and stamps the response on the way through.
	if strings.Contains(location, "/cdn-cgi/access/login") {
		return AccessResult{
			Protected: true,
			Provider:  "cloudflare-access",
			Detail:    "unauthenticated request redirected to the Access login",
		}
	}
	for name := range header {
		if strings.HasPrefix(strings.ToLower(name), "cf-access-") {
			return AccessResult{
				Protected: true,
				Provider:  "cloudflare-access",
				Detail:    "response carries a " + name + " header",
			}
		}
	}
	// A generic identity proxy in front of an API endpoint answers 401 with a
	// challenge naming itself. corgi's own bearer auth also answers 401, so the
	// challenge has to name something other than corgi to count.
	if status == http.StatusUnauthorized {
		if challenge := header.Get("Www-Authenticate"); challenge != "" && !isCorgiChallenge(challenge) {
			return AccessResult{
				Protected: true,
				Provider:  "identity-proxy",
				Detail:    "unauthenticated request challenged: " + challenge,
			}
		}
	}

	return AccessResult{Detail: fmt.Sprintf("no identity proxy observed (status %d)", status)}
}

// isCorgiChallenge reports whether a 401 came from corgi's own bearer check
// rather than something standing in front of it. Treating corgi's own auth as
// an identity proxy would let the endpoint declare itself private on the
// strength of the very token the gate exists to protect.
func isCorgiChallenge(challenge string) bool {
	lower := strings.ToLower(challenge)
	return strings.Contains(lower, "corgi") ||
		(strings.HasPrefix(lower, "bearer") && !strings.Contains(lower, "realm="))
}
