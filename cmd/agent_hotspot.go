package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/daemon"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var agentHotspotCmd = &cobra.Command{
	Use:   "hotspot [<wifi name>|off]",
	Short: "Hop onto your phone's hotspot when the internet is gone for a while",
	Long: `Hotel Wi-Fi drops, the laptop in the bag goes quiet, and the phone next to
it has data. After 90 seconds offline the daemon joins the hotspot named
here, tries again on a backoff, and tells the phone when the gap closes and
when Wi-Fi is back. A budget keeps a dependency download from eating the
phone's plan: past it, no new fix starts until Wi-Fi is back.

  corgi agent hotspot "Andrii's iPhone"             # an iPhone of yours: no password needed
  corgi agent hotspot "Travel router" --password    # asks for it, keeps it in the login keychain
  corgi agent hotspot "Andrii's iPhone" --budget 5GB
  corgi agent hotspot                               # what is set
  corgi agent hotspot off                           # forget it (it is learned again)

Nothing to set for an iPhone of yours: the first time this Mac is on its
hotspot, the daemon remembers the name and hops to it from then on.

An iPhone signed into the same Apple ID joins without a password when
System Settings › Wi-Fi › Ask to join hotspots is on; keep Bluetooth on.
The budget is 2 GB unless you say otherwise; --budget off lifts it.
Restart the daemon after a change (corgi agent restart).`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := agentDir()
		if err != nil {
			return err
		}
		path := agentUserConfigPath(dir)
		user, err := config.LoadUser(path)
		if err != nil {
			return err
		}
		if user == nil {
			user = &config.UserConfig{}
		}
		budget, _ := cmd.Flags().GetString("budget")
		if len(args) == 0 && budget == "" {
			printHotspot(user)
			return nil
		}
		if len(args) == 1 && strings.EqualFold(args[0], "off") {
			user.Hotspot, user.HotspotBudget = "", ""
			if err := writeUserConfig(path, user); err != nil {
				return err
			}
			utils.Info("✓ hotspot off - corgi agent restart")
			return nil
		}
		if len(args) == 1 {
			user.Hotspot = strings.TrimSpace(args[0])
		}
		if user.Hotspot == "" {
			return fmt.Errorf("name the hotspot first: corgi agent hotspot \"<wifi name>\"")
		}
		if budget != "" {
			if _, err := daemon.ParseSize(budget); err != nil {
				return err
			}
			user.HotspotBudget = budget
		}
		if ask, _ := cmd.Flags().GetBool("password"); ask {
			if err := storeHotspotPassword(user.Hotspot); err != nil {
				return err
			}
		}
		if err := writeUserConfig(path, user); err != nil {
			return err
		}
		utils.Infof("✓ hotspot %q joins after 90 s offline, %s - corgi agent restart\n", user.Hotspot, budgetWords(user.HotspotBudget))
		return nil
	},
}

func budgetWords(raw string) string {
	n := hotspotBudgetOf(raw)
	if n == 0 {
		return "no data budget"
	}
	return fmt.Sprintf("a %s data budget", daemon.SizeWords(n))
}

var hotspotLine = regexp.MustCompile(`(?m)^hotspot:.*$`)

func printHotspot(user *config.UserConfig) {
	if strings.TrimSpace(user.Hotspot) == "" {
		utils.Info("hotspot: not learned yet - the first time this Mac is on your phone's hotspot the daemon remembers its name; or name it: corgi agent hotspot \"<wifi name>\"")
		return
	}
	utils.Infof("hotspot: %s, %s\n", user.Hotspot, budgetWords(user.HotspotBudget))
}

func storeHotspotPassword(ssid string) error {
	fmt.Fprintf(os.Stderr, "password for %s: ", ssid)
	raw, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return fmt.Errorf("could not read the password: %v", err)
	}
	pw := strings.TrimSpace(string(raw))
	if pw == "" {
		return fmt.Errorf("an empty password - for an open network leave --password out")
	}
	out, err := exec.Command("security", "add-generic-password", "-U", "-s", "corgi-hotspot", "-a", ssid, "-w", pw).CombinedOutput()
	if err != nil {
		return fmt.Errorf("keychain: %v: %s", err, strings.TrimSpace(string(out)))
	}
	utils.Info("✓ password kept in the login keychain (corgi-hotspot)")
	return nil
}

func init() {
	agentHotspotCmd.Flags().Bool("password", false, "Ask for the hotspot password and keep it in the login keychain")
	agentHotspotCmd.Flags().String("budget", "", "Data the hotspot may use before new runs hold: 2GB, 500MB, off")
	agentCmd.AddCommand(agentHotspotCmd)
}
