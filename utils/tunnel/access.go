package tunnel

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Exposure string

const (
	ExposureLocal   Exposure = "local"
	ExposurePrivate Exposure = "private"
	ExposurePublic  Exposure = "public"
)

type AccessResult struct {
	Protected bool   `json:"protected"`
	Provider  string `json:"provider,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

func (r AccessResult) Exposure() Exposure {
	if r.Protected {
		return ExposurePrivate
	}
	return ExposurePublic
}

const accessProbeTimeout = 10 * time.Second

func ProbeAccess(ctx context.Context, rawURL string) AccessResult {
	return ProbeAccessWith(ctx, rawURL, &http.Client{})
}

func ProbeAccessWith(ctx context.Context, rawURL string, client *http.Client) AccessResult {
	if !strings.HasPrefix(rawURL, "https://") {
		return AccessResult{Detail: "not an https endpoint"}
	}

	ctx, cancel := context.WithTimeout(ctx, accessProbeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return AccessResult{Detail: err.Error()}
	}
	probe := *client
	probe.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := probe.Do(req)
	if err != nil {
		return AccessResult{Detail: fmt.Sprintf("probe failed: %v", err)}
	}
	defer resp.Body.Close()

	return classifyAccessResponse(resp.StatusCode, resp.Header)
}

func classifyAccessResponse(status int, header http.Header) AccessResult {
	location := header.Get("Location")

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

func isCorgiChallenge(challenge string) bool {
	lower := strings.ToLower(challenge)
	return strings.Contains(lower, "corgi") ||
		(strings.HasPrefix(lower, "bearer") && !strings.Contains(lower, "realm="))
}
