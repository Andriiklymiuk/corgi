package tunnel

import "runtime"

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

var Providers = map[string]Provider{
	"cloudflared": Cloudflared{},
	"ngrok":       Ngrok{},
	"localtunnel": Localtunnel{},
}

func Names() []string {
	out := make([]string, 0, len(Providers))
	for k := range Providers {
		out = append(out, k)
	}
	return out
}

func installHint(goos, formula, linuxURL string) string {
	if goos == "linux" {
		return "download it from " + linuxURL + " (deb, rpm or a binary), or `brew install " + formula + "` with Linuxbrew"
	}
	return "brew install " + formula
}

func hostInstallHint(formula, linuxURL string) string {
	return installHint(runtime.GOOS, formula, linuxURL)
}
