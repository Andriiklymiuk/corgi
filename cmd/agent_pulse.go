package cmd

import (
	"fmt"
	"net/url"
	"strings"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/daemon"

	"github.com/spf13/cobra"
)

var agentPulseCmd = &cobra.Command{
	Use:   "pulse [url|off]",
	Short: "Ping a URL every five minutes so a service alarms you when this machine goes quiet",
	Long: `A dead-man switch. The daemon GETs the URL every five minutes; a
healthchecks.io, Uptime Kuma push or cronitor check that stops hearing from
it emails or messages you. Corgi appends nothing to the URL.

  corgi agent pulse https://hc-ping.com/<uuid>   # set, then corgi agent restart
  corgi agent pulse                              # show
  corgi agent pulse off                          # clear`,
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
		if len(args) == 0 {
			printPulse(dir, user.PulseUrl)
			return nil
		}
		if strings.EqualFold(args[0], "off") {
			user.PulseUrl = ""
			if err := writeUserConfig(path, user); err != nil {
				return err
			}
			utils.Info("✓ pulse off — corgi agent restart")
			return nil
		}
		u, err := url.Parse(args[0])
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return fmt.Errorf("a pulse is an http(s) URL")
		}
		user.PulseUrl = args[0]
		if err := writeUserConfig(path, user); err != nil {
			return err
		}
		utils.Infof("✓ pulse → %s every %s — corgi agent restart\n", maskPulseURL(args[0]), daemon.DefaultPulseEvery)
		return nil
	},
}

func printPulse(dir, raw string) {
	if raw == "" {
		fmt.Println("no pulse — corgi agent pulse <url>")
		return
	}
	st := daemon.ReadPulse(dir)
	fmt.Println(maskPulseURL(raw))
	switch {
	case st.Error != "":
		fmt.Printf("last ping failed: %s\n", st.Error)
	case !st.At.IsZero():
		fmt.Printf("last ping %s\n", st.At.Local().Format("15:04:05"))
	default:
		fmt.Println("no ping yet — corgi agent restart")
	}
}

func maskPulseURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "***"
	}
	p := u.Path
	if len(p) > 9 {
		p = p[:5] + "…" + p[len(p)-4:]
	}
	return u.Scheme + "://" + u.Host + p
}

func init() {
	agentCmd.AddCommand(agentPulseCmd)
}
