package cmd

import (
	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/art"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var notificationsCmd = &cobra.Command{
	Use:   "notifications <on|off|test>",
	Short: "Enable, disable, or test desktop crash notifications",
	Long: `Toggle desktop notifications fired when a service crashes during corgi run.

Examples:
  corgi notifications              # show current setting
  corgi notifications on           # enable
  corgi notifications off          # disable
  corgi notifications test         # fire a one-shot test (bypasses opt-in)

State is persisted to ~/.corgi/config.yml.`,
	ValidArgs: []string{"on", "off", "test"},
	Args:      cobra.MaximumNArgs(1),
	Run:       runNotifications,
}

func init() {
	rootCmd.AddCommand(notificationsCmd)
}

func runNotifications(cmd *cobra.Command, args []string) {
	if len(args) == 0 {
		showNotificationsStatus()
		return
	}
	switch strings.ToLower(args[0]) {
	case "on":
		writeNotificationsPref(true)
	case "off":
		writeNotificationsPref(false)
	case "test":
		utils.NotifyRaw("corgi 🐶", "Test notification — if you see this, OS-level notifications work.")
		fmt.Println("Test notification dispatched. Nothing shown? Check OS notification permissions for your terminal app.")
	default:
		fmt.Fprintf(os.Stderr, "unknown action %q — use one of: on, off, test\n", args[0])
		os.Exit(2)
	}
}

func showNotificationsStatus() {
	cfg, err := utils.LoadUserConfig()
	if err != nil {
		fmt.Printf("%s❌ Failed to read user config: %v%s\n", art.RedColor, err, art.WhiteColor)
		os.Exit(1)
	}
	state := "off"
	if cfg.Notifications {
		state = "on"
	}
	fmt.Printf("notifications: %s\n", state)
	fmt.Println("Change with: corgi notifications on|off|test")
}

func writeNotificationsPref(enabled bool) {
	cfg, err := utils.LoadUserConfig()
	if err != nil {
		fmt.Printf("%s❌ Failed to read user config: %v%s\n", art.RedColor, err, art.WhiteColor)
		os.Exit(1)
	}
	cfg.Notifications = enabled
	if err := utils.SaveUserConfig(cfg); err != nil {
		fmt.Printf("%s❌ Failed to save user config: %v%s\n", art.RedColor, err, art.WhiteColor)
		os.Exit(1)
	}
	utils.ResetNotifyCache()

	state := "off"
	if enabled {
		state = "on"
	}
	dir, _ := utils.GetUserConfigDir()
	fmt.Printf("%s✅ Notifications %s%s\n", art.GreenColor, state, art.WhiteColor)
	fmt.Printf("   Saved to %s\n", filepath.Join(dir, "config.yml"))
}
