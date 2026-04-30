// Package tunnel exposes pluggable tunnel providers for `corgi tunnel`.
// Each provider knows how to spawn a CLI subprocess that opens a public
// HTTPS tunnel to a local port and prints (somewhere on stdout/stderr)
// a matching public URL line that can be parsed back out.
package tunnel

type NamedConfig struct {
	Hostname string
	Name     string
}

type Provider interface {
	Name() string
	Cmd(port int) []string
	CmdNamed(port int, cfg NamedConfig) ([]string, error)
	ExtractURL(line string) string
	InstallHint() string
	AcceptsStdin() bool
	PreflightAuth() error
	PreflightNamedAuth(cfg NamedConfig) error
}

// Providers is the registry consumed by the tunnel command.
var Providers = map[string]Provider{
	"cloudflared": Cloudflared{},
	"ngrok":       Ngrok{},
	"localtunnel": Localtunnel{},
}

// Names returns the registered provider keys for help text / validation.
func Names() []string {
	out := make([]string, 0, len(Providers))
	for k := range Providers {
		out = append(out, k)
	}
	return out
}
