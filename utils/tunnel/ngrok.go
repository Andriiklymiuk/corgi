package tunnel

import (
	"fmt"
	"os/exec"
	"regexp"
)

type Ngrok struct{}

func (Ngrok) Name() string { return "ngrok" }

func (Ngrok) Cmd(port int) []string {
	return []string{"ngrok", "http", "--log=stdout", fmt.Sprintf("%d", port)}
}

var ngrokURLRe = regexp.MustCompile(`https://[a-z0-9-]+\.ngrok[a-z0-9.-]*`)

func (Ngrok) ExtractURL(line string) string { return ngrokURLRe.FindString(line) }

func (Ngrok) InstallHint() string { return hostInstallHint("ngrok", "ngrok.com/download") }

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
	return nil
}
