// Package tunnel exposes pluggable tunnel providers for `corgi tunnel`.
// Each provider knows how to spawn a CLI subprocess that opens a public
// HTTPS tunnel to a local port and prints (somewhere on stdout/stderr)
// a matching public URL line that can be parsed back out.
package tunnel

// Provider abstracts a tunnel CLI (cloudflared, ngrok, localtunnel, …).
// Keep impls thin — runner.go handles process lifecycle and URL streaming.
type Provider interface {
	// Name is the display label, e.g. "cloudflared".
	Name() string
	// Cmd returns argv for spawning the tunnel for the given local port.
	// First element is the binary name (resolved via PATH).
	Cmd(port int) []string
	// ExtractURL inspects one stdout/stderr line and returns the public URL
	// when one is announced, "" otherwise. Called for every line.
	ExtractURL(line string) string
	// InstallHint is shown when the binary is missing.
	InstallHint() string
	// AcceptsStdin reports whether the provider expects an interactive
	// stdin (e.g. for license prompts). Most return false.
	AcceptsStdin() bool
	// PreflightAuth returns nil if the provider can run without further
	// setup. Otherwise returns an error whose Error() string is a
	// user-facing message including the login command to run. Called once
	// before spawning any tunnels — if any target fails this, the whole
	// `corgi tunnel` invocation aborts.
	PreflightAuth() error
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
