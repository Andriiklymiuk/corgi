package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/pairing"
	"andriiklymiuk/corgi/utils/agent/screen"
)

var (
	screenHostPort        int
	screenHostFastPort    int
	screenHostFingerprint string
	screenHostClear       bool
	screenVerifyTicket    string
	screenVerifyAddr      string
	screenLogEvent        string
	screenLogDevice       string
)

var agentScreenCmd = &cobra.Command{
	Use:   "screen [on|off|status|devices|grant|forget|reach|log]",
	Short: "Your Mac's screen on your iPhone, iPad or another Mac: turn it on, grant devices, end sessions",
	Long: `Remote screen lets a paired phone, iPad or Mac see and drive this Mac through
Corgi Bar. It is off until you turn it on, a new device is asked about on the
Mac the first time, and Face ID on the phone guards every session.

  corgi agent screen                 what it is now
  corgi agent screen on              allow it (Corgi Bar shares the screen)
  corgi agent screen off             stop every session and close the port
  corgi agent screen devices         paired devices and what each may do
  corgi agent screen grant "iPad" control|watch|off
  corgi agent screen forget "iPad"   drop its grant and Face ID key
  corgi agent screen reach nearby|away
  corgi agent screen log             the last sessions`,
	Args: cobra.MaximumNArgs(3),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		word := "status"
		if len(args) > 0 {
			word = strings.ToLower(args[0])
		}
		if err := runAgentScreen(dir, word, args[1:]); err != nil {
			exitWithError("agent_screen", err, 1)
		}
	},
}

var agentScreenHostCmd = &cobra.Command{
	Use:    "host",
	Short:  "Corgi Bar registers the screen it shares (port, fast port, certificate fingerprint)",
	Hidden: true,
	Args:   cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		if screenHostClear {
			_ = screen.ClearHost(dir)
			utils.PrintJSON(map[string]any{"ok": true})
			return
		}
		s, err := screen.Load(dir)
		if err != nil {
			exitWithError("agent_screen_host", err, 1)
		}
		if !s.Enabled {
			_ = screen.ClearHost(dir)
			utils.PrintJSON(map[string]any{"enabled": false})
			return
		}
		h := screen.Host{Port: screenHostPort, FastPort: screenHostFastPort, Fingerprint: screenHostFingerprint}
		if err := screen.RegisterHost(dir, h, time.Now()); err != nil {
			exitWithError("agent_screen_host", err, 2)
		}
		utils.PrintJSON(map[string]any{"enabled": true, "settings": s})
	},
}

var agentScreenVerifyCmd = &cobra.Command{
	Use:    "verify",
	Short:  "Corgi Bar checks a viewer's one-time ticket",
	Hidden: true,
	Args:   cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		utils.PrintJSON(verifyScreenTicket(mustAgentDir(), screenVerifyTicket, screenVerifyAddr, time.Now()))
	},
}

var screenTicketDevice string

var agentScreenTicketCmd = &cobra.Command{
	Use:    "ticket",
	Short:  "A one-time ticket for a granted device, for diagnostics on this Mac",
	Hidden: true,
	Args:   cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		s, err := screen.Load(dir)
		if err != nil {
			exitWithError("agent_screen_ticket", err, 1)
		}
		if !s.Enabled {
			exitWithError("agent_screen_ticket", fmt.Errorf("remote screen is off: corgi agent screen on"), 2)
		}
		g := s.Devices[screenTicketDevice]
		if g == nil || g.Role == "" || g.Role == screen.RoleOff {
			exitWithError("agent_screen_ticket", fmt.Errorf("%q has no grant: corgi agent screen grant", screenTicketDevice), 2)
		}
		h, _ := screen.CurrentHost(dir, time.Now())
		if h == nil {
			exitWithError("agent_screen_ticket", fmt.Errorf("Corgi Bar is not sharing: open it on this Mac"), 2)
		}
		t, err := screen.MintTicket(dir, screenTicketDevice, g.Name, time.Now())
		if err != nil {
			exitWithError("agent_screen_ticket", err, 1)
		}
		cands := []string{fmt.Sprintf("127.0.0.1:%d", h.Port)}
		for _, c := range screen.Candidates(h.Port, s.Reach) {
			cands = append(cands, c.Addr)
		}
		utils.PrintJSON(map[string]any{"ticket": t, "fingerprint": h.Fingerprint, "port": h.Port, "fastPort": h.FastPort, "candidates": cands, "role": g.Role})
	},
}

var agentScreenPeerCmd = &cobra.Command{
	Use:    "peer <name> [open-json]",
	Short:  "Corgi Bar opens a peer's screen: the challenge to sign, then the ticket",
	Hidden: true,
	Args:   cobra.RangeArgs(1, 2),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		if len(args) == 1 {
			out, err := peerScreenChallenge(dir, args[0])
			if err != nil {
				exitWithError("agent_screen_peer", err, 2)
			}
			utils.PrintJSON(out)
			return
		}
		var in peerScreenIn
		if err := json.Unmarshal([]byte(args[1]), &in); err != nil {
			exitWithError("agent_screen_peer", err, 2)
		}
		in.Peer = args[0]
		out, err := peerScreenOpen(dir, in)
		if err != nil {
			exitWithError("agent_screen_peer", err, 1)
		}
		utils.PrintJSON(out)
	},
}

var agentScreenSettingsCmd = &cobra.Command{
	Use:    "settings [json]",
	Short:  "Corgi Bar reads or writes the remote screen settings",
	Hidden: true,
	Args:   cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		if len(args) == 1 {
			var in screenSettingsIn
			if err := json.Unmarshal([]byte(args[0]), &in); err != nil {
				exitWithError("agent_screen_settings", err, 2)
			}
			if err := applyScreenChange(dir, in, time.Now()); err != nil {
				exitWithError("agent_screen_settings", err, 2)
			}
		}
		utils.PrintJSON(screenOverview(dir))
	},
}

func init() {
	agentScreenHostCmd.Flags().IntVar(&screenHostPort, "port", 0, "QUIC port")
	agentScreenHostCmd.Flags().IntVar(&screenHostFastPort, "fast-port", 0, "fast lane UDP port")
	agentScreenHostCmd.Flags().StringVar(&screenHostFingerprint, "fingerprint", "", "SHA-256 of the session certificate")
	agentScreenHostCmd.Flags().BoolVar(&screenHostClear, "clear", false, "the bar stopped sharing")
	agentScreenVerifyCmd.Flags().StringVar(&screenVerifyTicket, "ticket", "", "one-time ticket from the viewer")
	agentScreenVerifyCmd.Flags().StringVar(&screenVerifyAddr, "addr", "", "the viewer's address")
	agentScreenCmd.Flags().StringVar(&screenLogEvent, "event", "", "")
	agentScreenCmd.Flags().StringVar(&screenLogDevice, "device", "", "")
	_ = agentScreenCmd.Flags().MarkHidden("event")
	_ = agentScreenCmd.Flags().MarkHidden("device")
	agentScreenTicketCmd.Flags().StringVar(&screenTicketDevice, "device", "", "device id from corgi agent screen devices --json")
	agentScreenCmd.AddCommand(agentScreenHostCmd, agentScreenVerifyCmd, agentScreenSettingsCmd, agentScreenTicketCmd, agentScreenPeerCmd)
	agentCmd.AddCommand(agentScreenCmd)
}

func findScreenDevice(dir, name string) (string, string, error) {
	store, err := pairing.Load(pairing.StorePath(dir))
	if err != nil {
		return "", "", err
	}
	var hits []pairing.Device
	for _, d := range store.Devices {
		if !screenViewer(d) {
			continue
		}
		if strings.EqualFold(d.Name, name) || strings.HasPrefix(d.TokenHash, name) {
			return d.TokenHash, d.Name, nil
		}
		if strings.Contains(strings.ToLower(d.Name), strings.ToLower(name)) {
			hits = append(hits, d)
		}
	}
	if len(hits) == 1 {
		return hits[0].TokenHash, hits[0].Name, nil
	}
	if len(hits) > 1 {
		return "", "", fmt.Errorf("%q matches %d devices: use the full name from corgi agent screen devices", name, len(hits))
	}
	return "", "", fmt.Errorf("no paired phone, iPad or Mac named %q (corgi agent screen devices)", name)
}

var screenWords = map[string]func(dir string, rest []string) error{
	"status":  func(dir string, _ []string) error { return printScreenStatus(dir) },
	"devices": func(dir string, _ []string) error { return printScreenDevices(dir) },
	"on":      func(dir string, _ []string) error { return setScreenEnabled(dir, true) },
	"off":     func(dir string, _ []string) error { return setScreenEnabled(dir, false) },
	"grant":   grantScreen,
	"forget":  forgetScreen,
	"reach":   setScreenReach,
	"log":     printScreenLog,
}

func runAgentScreen(dir, word string, rest []string) error {
	if run, ok := screenWords[word]; ok {
		return run(dir, rest)
	}
	return fmt.Errorf("unknown: %s (on, off, status, devices, grant, forget, reach, log)", word)
}

func setScreenEnabled(dir string, on bool) error {
	if _, err := screen.Update(dir, func(s *screen.Settings) error {
		s.Enabled = on
		return nil
	}); err != nil {
		return err
	}
	if !on {
		_ = screen.ClearHost(dir)
	}
	switch {
	case utils.JSONOutput:
		utils.PrintJSON(map[string]any{"enabled": on})
	case on:
		utils.Info("✓ Remote screen on. Corgi Bar shares the screen; a new device is asked about on the Mac first.")
	default:
		utils.Info("✓ Remote screen off. Sessions end within seconds and nothing listens.")
	}
	return nil
}

func grantScreen(dir string, rest []string) error {
	if len(rest) != 2 {
		return fmt.Errorf(`grant takes a device and a role: corgi agent screen grant "iPad" control`)
	}
	role, err := screen.ParseRole(rest[1])
	if err != nil {
		return err
	}
	id, name, err := findScreenDevice(dir, rest[0])
	if err != nil {
		return err
	}
	if err := applyScreenChange(dir, screenSettingsIn{Do: "grant", Device: id, Role: role}, time.Now()); err != nil {
		return err
	}
	utils.Infof("✓ %s: %s\n", name, role)
	return nil
}

func forgetScreen(dir string, rest []string) error {
	if len(rest) != 1 {
		return fmt.Errorf(`forget takes a device: corgi agent screen forget "iPad"`)
	}
	id, name, err := findScreenDevice(dir, rest[0])
	if err != nil {
		return err
	}
	if err := applyScreenChange(dir, screenSettingsIn{Do: "forget", Device: id}, time.Now()); err != nil {
		return err
	}
	utils.Infof("✓ %s forgotten: it is asked about again next time\n", name)
	return nil
}

func setScreenReach(dir string, rest []string) error {
	if len(rest) != 1 || (rest[0] != screen.ReachNearby && rest[0] != screen.ReachAway) {
		return fmt.Errorf("reach is nearby (same network only) or away (anywhere the phone can reach)")
	}
	_, err := screen.Update(dir, func(s *screen.Settings) error {
		s.Reach = rest[0]
		return nil
	})
	return err
}

func printScreenLog(dir string, _ []string) error {
	if screenLogEvent != "" {
		return applyScreenChange(dir, screenSettingsIn{Do: "log", Device: screenLogDevice, Event: screenLogEvent}, time.Now())
	}
	recent, err := screen.Recent(dir, 20)
	if err != nil {
		return err
	}
	if utils.JSONOutput {
		utils.PrintJSON(recent)
		return nil
	}
	if len(recent) == 0 {
		utils.Info("No sessions yet.")
	}
	for _, s := range recent {
		fmt.Printf("%s  %-5s  %-20s %-7s %s\n", s.At.Local().Format("Jan 2 15:04"), s.Event, s.Name, s.Role, s.Addr)
	}
	return nil
}

func printScreenStatus(dir string) error {
	o := screenOverview(dir)
	if utils.JSONOutput {
		utils.PrintJSON(o)
		return nil
	}
	s := o["settings"].(*screen.Settings)
	state := "off"
	if s.Enabled {
		state = "on"
	}
	fmt.Printf("Remote screen: %s · reach %s · idle stop %s\n", state, s.Reach, s.IdleStop)
	if h, _ := o["host"].(*screen.Host); h != nil {
		fmt.Printf("Corgi Bar is sharing (QUIC %d, fast lane %d)\n", h.Port, h.FastPort)
	} else if s.Enabled {
		fmt.Println("Corgi Bar is not sharing yet: open it on this Mac")
	}
	return printScreenDevices(dir)
}

func printScreenDevices(dir string) error {
	o := screenOverview(dir)
	rows, _ := o["devices"].([]screenDeviceRow)
	if utils.JSONOutput {
		utils.PrintJSON(rows)
		return nil
	}
	if len(rows) == 0 {
		fmt.Println("No paired phones yet: corgi agent pair")
		return nil
	}
	for _, r := range rows {
		key := ""
		if r.Key {
			key = " · Face ID set"
		}
		fmt.Printf("  %-28s %-8s%s\n", r.Name, r.Role, key)
	}
	return nil
}
