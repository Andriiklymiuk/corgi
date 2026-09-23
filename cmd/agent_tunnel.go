package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/tunnel"

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
so a plain ` + "`corgi agent restart`" + ` keeps the same URL - and the phone
stays paired, because the origin never changes.

For ngrok it checks the authtoken and saves the domain. Every free account
already has one static ` + "`*.ngrok-free.dev`" + ` dev domain - copy it from
dashboard.ngrok.com/domains; its name cannot be chosen on the free tier.`,
	Args: cobra.ExactArgs(1),
	Run:  runAgentTunnelSetup,
}

type tunnelRunner func(name string, args ...string) (string, error)

type binaryLookup func(string) error

func lookPath(name string) error {
	_, err := exec.LookPath(name)
	return err
}

const setupStepTimeout = 2 * time.Minute

func execRunner(name string, args ...string) (string, error) {
	timeout := setupStepTimeout
	if len(args) > 1 && args[1] == "login" {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if ctx.Err() != nil {
		return string(out), fmt.Errorf("%s %s timed out after %s", name, strings.Join(args, " "), timeout)
	}
	return string(out), err
}

var (
	tunnelExec   tunnelRunner = execRunner
	tunnelLookup binaryLookup = lookPath
)

func runAgentTunnelSetup(cmd *cobra.Command, args []string) {
	host := strings.TrimSpace(args[0])
	provider, _ := cmd.Flags().GetString("provider")
	name, _ := cmd.Flags().GetString("name")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	if provider == "" {
		provider = "cloudflared"
	}

	dir, err := agentDir()
	if err != nil {
		exitWithError("agent_data_dir", err, 1)
	}
	if name == "" {
		name = defaultTunnelName(loadUpSettings(dir).TunnelName)
	}

	run := tunnelExec
	if dryRun {
		run = func(bin string, a ...string) (string, error) {
			utils.Infof("  would run: %s %s\n", bin, strings.Join(a, " "))
			return "", nil
		}
	}

	switch provider {
	case "cloudflared":
		if err := setupCloudflaredTunnel(run, tunnelLookup, name, host, dryRun); err != nil {
			exitWithError("agent_tunnel_setup", err, 1)
		}
	case "ngrok":
		if err := setupNgrokTunnel(run, tunnelLookup, host); err != nil {
			exitWithError("agent_tunnel_setup", err, 1)
		}
	default:
		exitWithError("agent_tunnel_setup",
			fmt.Errorf("tunnel setup covers cloudflared and ngrok; %q has no one-time setup - pass its flags to `corgi agent up` directly", provider), 2)
	}

	if dryRun {
		utils.Info("dry run - nothing was changed and no settings were saved")
		return
	}
	if err := saveUpSettings(dir, upSettings{Provider: provider, TunnelName: tunnelNameFor(provider, name), TunnelHostname: host}); err != nil {
		exitWithError("agent_tunnel_setup", err, 1)
	}
	utils.Infof("✓ saved: `corgi agent up` and `corgi agent restart` now serve the launcher at https://%s/app\n", host)
	utils.Info("next: `corgi agent restart`, then scan the QR once - the phone stays paired from then on")
}

func tunnelNameFor(provider, name string) string {
	if provider == "ngrok" {
		return ""
	}
	return name
}

func setupCloudflaredTunnel(run tunnelRunner, have binaryLookup, name, host string, dryRun bool) error {
	if err := have("cloudflared"); err != nil {
		return fmt.Errorf("cloudflared is not installed - %s", tunnel.Cloudflared{}.InstallHint())
	}

	utils.Info("checking cloudflared login…")
	list, listErr := run("cloudflared", "tunnel", "list")
	if listErr != nil && !dryRun {
		utils.Info("not logged in - opening the browser (pick the domain you want the launcher on)")
		if out, err := run("cloudflared", "tunnel", "login"); err != nil {
			return fmt.Errorf("cloudflared tunnel login failed: %w\n%s", err, strings.TrimSpace(out))
		}
		list, _ = run("cloudflared", "tunnel", "list")
	}

	if id := tunnelIDIn(list, name); id != "" {
		if !dryRun && !tunnelCredentialsExist(id) {
			return fmt.Errorf("tunnel %s exists in this Cloudflare account, but its credentials are not on this laptop - it was created on another one.\n"+
				"Every laptop needs a tunnel of its own on a hostname of its own: corgi agent tunnel setup %s --name %s", name, host, defaultTunnelName(""))
		}
		utils.Infof("tunnel %s already exists\n", name)
	} else {
		utils.Infof("creating tunnel %s…\n", name)
		if out, err := run("cloudflared", "tunnel", "create", name); err != nil {
			return fmt.Errorf("could not create tunnel %s: %w\n%s", name, err, strings.TrimSpace(out))
		}
	}

	utils.Infof("routing %s to %s…\n", host, name)
	if out, err := run("cloudflared", "tunnel", "route", "dns", name, host); err != nil {
		if !strings.Contains(strings.ToLower(out), "already exists") {
			return fmt.Errorf("could not route %s: %w\n%s", host, err, strings.TrimSpace(out))
		}
		utils.Infof("%s already has a DNS record - if it was made for this tunnel, all is well; if another laptop made it, pick a hostname of your own (one per laptop: home.%s, work.%s)\n", host, domainOf(host), domainOf(host))
	}
	return nil
}

// defaultTunnelName is the saved name when there is one, else one that
// tells two laptops on the same Cloudflare account apart.
func defaultTunnelName(saved string) string {
	if saved = strings.TrimSpace(saved); saved != "" {
		return saved
	}
	host, _ := os.Hostname()
	short := strings.ToLower(strings.SplitN(strings.TrimSpace(host), ".", 2)[0])
	short = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			return r
		}
		return '-'
	}, short)
	short = strings.Trim(short, "-")
	if short == "" {
		return "corgi-agent"
	}
	return "corgi-" + short
}

// tunnelIDIn reads `cloudflared tunnel list`: the id is the first column
// of the row whose name is exactly the one asked for.
func tunnelIDIn(list, name string) string {
	for _, line := range strings.Split(list, "\n") {
		fields := strings.Fields(line)
		named := false
		for _, f := range fields {
			named = named || f == name
		}
		if !named {
			continue
		}
		for _, f := range fields {
			if f != name && tunnelIDShape.MatchString(f) {
				return f
			}
		}
		return "?"
	}
	return ""
}

var tunnelIDShape = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

var tunnelCredentialsExist = func(id string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return true
	}
	_, err = os.Stat(filepath.Join(home, ".cloudflared", id+".json"))
	return err == nil
}

func domainOf(host string) string {
	parts := strings.Split(host, ".")
	if len(parts) > 2 {
		return strings.Join(parts[len(parts)-2:], ".")
	}
	return host
}

func setupNgrokTunnel(run tunnelRunner, have binaryLookup, host string) error {
	if err := have("ngrok"); err != nil {
		return utils.NotInstalledError("ngrok")
	}
	if _, err := run("ngrok", "config", "check"); err != nil {
		return fmt.Errorf(`ngrok has no authtoken configured. Get one from
https://dashboard.ngrok.com/get-started/your-authtoken then run:

    ngrok config add-authtoken <token>`)
	}
	utils.Infof("using ngrok domain %s\n", host)
	utils.Info("free tier: this is the `dev domain` row on dashboard.ngrok.com/domains - its name is assigned, not chosen")
	return nil
}

func init() {
	agentTunnelSetupCmd.Flags().String("provider", "cloudflared", "Tunnel provider (cloudflared|ngrok)")
	agentTunnelSetupCmd.Flags().String("name", "", "cloudflared tunnel name to create or reuse (default: the saved one, else corgi-<this laptop>)")
	agentTunnelSetupCmd.Flags().Bool("dry-run", false, "Print the commands without running them")
	agentTunnelCmd.AddCommand(agentTunnelSetupCmd)
	agentCmd.AddCommand(agentTunnelCmd)
}
