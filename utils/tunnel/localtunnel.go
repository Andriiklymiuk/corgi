package tunnel

import (
	"fmt"
	"regexp"
)

// Localtunnel wraps the npm `localtunnel` CLI (`lt --port <port>`).
// It emits exactly one line: "your url is: https://<sub>.loca.lt".
type Localtunnel struct{}

func (Localtunnel) Name() string { return "localtunnel" }

func (Localtunnel) Cmd(port int) []string {
	return []string{"lt", "--port", fmt.Sprintf("%d", port)}
}

var localtunnelURLRe = regexp.MustCompile(`https://[a-z0-9-]+\.loca\.lt`)

func (Localtunnel) ExtractURL(line string) string { return localtunnelURLRe.FindString(line) }

func (Localtunnel) InstallHint() string { return "npm install -g localtunnel" }

func (Localtunnel) AcceptsStdin() bool { return false }

// PreflightAuth — localtunnel needs no auth ever. Anonymous + free.
func (Localtunnel) PreflightAuth() error { return nil }
