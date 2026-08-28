package cmd

import (
	"fmt"
	"os/exec"
	"strings"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

var agentTunnelCmd = &cobra.Command{
	Use:   "tunnel",
	Short: "Set up the permanent launcher URL",
}

var agentTunnelSetupCmd = &cobra.Command{
	Use:   "setup <hostname>",
	Short: "One-time setup for a launcher URL that survives restarts",
	Long: `Does the permanent-URL setup end to end and remembers it for later runs.

For cloudflared (the default) it logs you in if needed, creates the named
tunnel when it does not exist, routes the DNS name to it, and saves both flags
so a plain ` + "`corgi agent restart`" + ` keeps the same URL — and the phone
stays paired, because the origin never changes.

For ngrok it checks the authtoken and saves the domain; claiming the free
static domain itself is a dashboard step with no CLI equivalent.`,
	Args: cobra.ExactArgs(1),
	Run:  runAgentTunnelSetup,
}

// runner executes one setup step. Injected so the plan can be tested without
// touching cloudflared.
type tunnelRunner func(name string, args ...string) (string, error)

// binaryLookup reports whether a command exists. Injected so the setup plan can
// be tested on a machine (or a CI runner) without cloudflared installed.
type binaryLookup func(string) error

func lookPath(name string) error {
	_, err := exec.LookPath(name)
	return err
}

func execRunner(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return string(out), err
}

func runAgentTunnelSetup(cmd *cobra.Command, args []string) {
	host := strings.TrimSpace(args[0])
	provider, _ := cmd.Flags().GetString("provider")
	name, _ := cmd.Flags().GetString("name")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	if provider == "" {
		provider = "cloudflared"
	}
	if name == "" {
		name = "corgi-agent"
	}

	dir, err := agentDir()
	if err != nil {
		exitWithError("agent_data_dir", err, 1)
	}

	run := execRunner
	if dryRun {
		run = func(bin string, a ...string) (string, error) {
			utils.Infof("  would run: %s %s\n", bin, strings.Join(a, " "))
			return "", nil
		}
	}

	switch provider {
	case "cloudflared":
		if err := setupCloudflaredTunnel(run, lookPath, name, host, dryRun); err != nil {
			exitWithError("agent_tunnel_setup", err, 1)
		}
	case "ngrok":
		if err := setupNgrokTunnel(run, lookPath, host); err != nil {
			exitWithError("agent_tunnel_setup", err, 1)
		}
	default:
		exitWithError("agent_tunnel_setup",
			fmt.Errorf("tunnel setup covers cloudflared and ngrok; %q has no one-time setup — pass its flags to `corgi agent up` directly", provider), 2)
	}

	if dryRun {
		utils.Info("dry run — nothing was changed and no settings were saved")
		return
	}
	if err := saveUpSettings(dir, upSettings{Provider: provider, TunnelName: tunnelNameFor(provider, name), TunnelHostname: host}); err != nil {
		exitWithError("agent_tunnel_setup", err, 1)
	}
	utils.Infof("✓ saved: `corgi agent up` and `corgi agent restart` now serve the launcher at https://%s/app\n", host)
	utils.Info("next: `corgi agent restart`, then scan the QR once — the phone stays paired from then on")
}

// tunnelNameFor keeps the name only where it means something: ngrok selects a
// tunnel by its domain, and passing a name would be recorded but never used.
func tunnelNameFor(provider, name string) string {
	if provider == "ngrok" {
		return ""
	}
	return name
}

func setupCloudflaredTunnel(run tunnelRunner, have binaryLookup, name, host string, dryRun bool) error {
	if err := have("cloudflared"); err != nil {
		return fmt.Errorf("cloudflared is not installed — `brew install cloudflared` (or see developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads)")
	}

	utils.Info("checking cloudflared login…")
	list, listErr := run("cloudflared", "tunnel", "list")
	if listErr != nil && !dryRun {
		utils.Info("not logged in — opening the browser (pick the domain you want the launcher on)")
		if out, err := run("cloudflared", "tunnel", "login"); err != nil {
			return fmt.Errorf("cloudflared tunnel login failed: %w\n%s", err, strings.TrimSpace(out))
		}
		list, _ = run("cloudflared", "tunnel", "list")
	}

	if strings.Contains(list, name) {
		utils.Infof("tunnel %s already exists\n", name)
	} else {
		utils.Infof("creating tunnel %s…\n", name)
		if out, err := run("cloudflared", "tunnel", "create", name); err != nil {
			return fmt.Errorf("could not create tunnel %s: %w\n%s", name, err, strings.TrimSpace(out))
		}
	}

	utils.Infof("routing %s to %s…\n", host, name)
	if out, err := run("cloudflared", "tunnel", "route", "dns", name, host); err != nil {
		// An existing route is the normal case on a re-run, not a failure.
		if !strings.Contains(strings.ToLower(out), "already exists") {
			return fmt.Errorf("could not route %s: %w\n%s", host, err, strings.TrimSpace(out))
		}
		utils.Infof("%s already points at %s\n", host, name)
	}
	return nil
}

func setupNgrokTunnel(run tunnelRunner, have binaryLookup, host string) error {
	if err := have("ngrok"); err != nil {
		return fmt.Errorf("ngrok is not installed — `brew install ngrok`")
	}
	if _, err := run("ngrok", "config", "check"); err != nil {
		return fmt.Errorf(`ngrok has no authtoken configured. Get one from
https://dashboard.ngrok.com/get-started/your-authtoken then run:

    ngrok config add-authtoken <token>`)
	}
	utils.Infof("using ngrok domain %s\n", host)
	utils.Info("if that domain is not claimed yet: dashboard.ngrok.com → Domains → claim your free static domain")
	return nil
}

func init() {
	agentTunnelSetupCmd.Flags().String("provider", "cloudflared", "Tunnel provider (cloudflared|ngrok)")
	agentTunnelSetupCmd.Flags().String("name", "corgi-agent", "cloudflared tunnel name to create or reuse")
	agentTunnelSetupCmd.Flags().Bool("dry-run", false, "Print the commands without running them")
	agentTunnelCmd.AddCommand(agentTunnelSetupCmd)
	agentCmd.AddCommand(agentTunnelCmd)
}
