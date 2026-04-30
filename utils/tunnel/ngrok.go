package tunnel

import (
	"fmt"
	"os/exec"
	"regexp"
)

// Ngrok wraps the ngrok CLI in log=stdout mode so we can parse the
// "url=https://...ngrok-free.app" or "url=https://...ngrok.io" line emitted
// when the tunnel is established.
type Ngrok struct{}

func (Ngrok) Name() string { return "ngrok" }

func (Ngrok) Cmd(port int) []string {
	return []string{"ngrok", "http", "--log=stdout", fmt.Sprintf("%d", port)}
}

// ngrok writes structured-ish log lines, URLs appear inside addr=… url=…
// fields. Match any ngrok-flavored https URL.
var ngrokURLRe = regexp.MustCompile(`https://[a-z0-9-]+\.ngrok[a-z0-9.-]*`)

func (Ngrok) ExtractURL(line string) string { return ngrokURLRe.FindString(line) }

func (Ngrok) InstallHint() string { return "brew install ngrok/ngrok/ngrok" }

func (Ngrok) AcceptsStdin() bool { return false }

func (Ngrok) PreflightAuth() error {
	cmd := exec.Command("ngrok", "config", "check")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf(`ngrok authtoken not configured.

Get a free token from https://dashboard.ngrok.com/get-started/your-authtoken
then run:

    ngrok config add-authtoken <YOUR_TOKEN>

(Free tier is fine. No paid plan needed for local webhook testing.)`)
	}
	return nil
}

func (Ngrok) CmdNamed(port int, cfg NamedConfig) ([]string, error) {
	return []string{
		"ngrok", "http",
		"--log=stdout",
		"--domain=" + cfg.Hostname,
		fmt.Sprintf("%d", port),
	}, nil
}

func (Ngrok) PreflightNamedAuth(cfg NamedConfig) error {
	if err := (Ngrok{}).PreflightAuth(); err != nil {
		return err
	}
	// Domain claim verification needs ngrok API; defer to runtime.
	// If hostname unclaimed, ngrok prints a clear error in stdout.
	return nil
}
