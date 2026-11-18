package cmd

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	xterm "golang.org/x/term"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/pairing"
	"andriiklymiuk/corgi/utils/agent/peers"
	"andriiklymiuk/corgi/utils/agent/screen"
	"andriiklymiuk/corgi/utils/agent/term"
	"andriiklymiuk/corgi/utils/agent/wsock"
)

var agentTermCmd = &cobra.Command{
	Use:   "term [peer|on|off|status|allow|deny]",
	Short: "A shell on another corgi machine - a Linux server, a peer laptop - with no ssh keys or open ports",
	Long: `Opens a shell on a peer over the sealed pairing link: the tunnel or the LAN
carries it, end-to-end encrypted, and the other machine decides who may.
From there ssh, tmux and the logs are one line away.

On the machine that is to be opened (off until turned on):
  corgi agent term on
  corgi agent term allow "office-mac"     a peer or a paired phone may open one
  corgi agent term deny "office-mac"
  corgi agent term status                 who may, and the last sessions

From another machine:
  corgi agent term build-server           a shell there (exit or Ctrl-D ends it)`,
	Args: cobra.MaximumNArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		word, rest := firstWordOr("status", args)
		if err := runAgentTerm(dir, word, rest); err != nil {
			exitWithError("agent_term", err, 1)
		}
	},
}

func init() {
	agentCmd.AddCommand(agentTermCmd)
}

func runAgentTerm(dir, word string, rest []string) error {
	switch word {
	case "on", "off":
		on := word == "on"
		if _, err := screen.Update(dir, func(s *screen.Settings) error {
			s.Terminal = on
			return nil
		}); err != nil {
			return err
		}
		if on {
			utils.Info("✓ Terminals on. Only devices you allow may open one: corgi agent term allow \"<name>\"")
		} else {
			utils.Info("✓ Terminals off. Open ones end when the shell exits; no new ones start.")
		}
		return nil
	case "allow", "deny":
		if len(rest) != 1 {
			return fmt.Errorf(`%s takes a device or peer name: corgi agent term %s "office-mac"`, word, word)
		}
		return setTerminalGrant(dir, rest[0], word == "allow")
	case "status":
		return printTerminalStatus(dir)
	}
	return openPeerTerminal(dir, word)
}

func findTerminalDevice(dir, name string) (pairing.Device, error) {
	store, err := pairing.Load(pairing.StorePath(dir))
	if err != nil {
		return pairing.Device{}, err
	}
	var hits []pairing.Device
	for _, d := range store.Devices {
		if !terminalDevice(d) {
			continue
		}
		if strings.EqualFold(d.Name, name) {
			return d, nil
		}
		if strings.Contains(strings.ToLower(d.Name), strings.ToLower(name)) {
			hits = append(hits, d)
		}
	}
	if len(hits) == 1 {
		return hits[0], nil
	}
	if len(hits) > 1 {
		return pairing.Device{}, fmt.Errorf("%q matches %d devices: use the full name", name, len(hits))
	}
	return pairing.Device{}, fmt.Errorf("no paired peer or phone named %q (corgi mcp devices)", name)
}

func setTerminalGrant(dir, name string, allow bool) error {
	d, err := findTerminalDevice(dir, name)
	if err != nil {
		return err
	}
	if _, err := screen.Update(dir, func(s *screen.Settings) error {
		g := s.Devices[d.TokenHash]
		if g == nil {
			g = &screen.Grant{Name: d.Name, Since: time.Now()}
			s.Devices[d.TokenHash] = g
		}
		g.Terminal = allow
		return nil
	}); err != nil {
		return err
	}
	if allow {
		utils.Infof("✓ %s may open a terminal here\n", d.Name)
	} else {
		utils.Infof("✓ %s may not open a terminal here\n", d.Name)
	}
	return nil
}

func printTerminalStatus(dir string) error {
	s, err := screen.Load(dir)
	if err != nil {
		return err
	}
	var allowed []string
	for _, g := range s.Devices {
		if g.Terminal {
			allowed = append(allowed, g.Name)
		}
	}
	if utils.JSONOutput {
		utils.PrintJSON(map[string]any{"enabled": s.Terminal, "allowed": allowed})
		return nil
	}
	state := "off"
	if s.Terminal {
		state = "on"
	}
	fmt.Printf("Terminals here: %s\n", state)
	if len(allowed) == 0 {
		fmt.Println("Nobody may open one yet: corgi agent term allow \"<peer or phone>\"")
	}
	for _, n := range allowed {
		fmt.Printf("  %s\n", n)
	}
	return nil
}

func peerWebSocketURL(base, path string, q url.Values) (string, error) {
	u, err := url.Parse(strings.TrimRight(base, "/") + path)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return "", fmt.Errorf("the peer's address %q is neither http nor https", base)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func dialPeerTerminal(p peers.Peer, key []byte, cols, rows int) (*wsock.Conn, *term.Box, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var opened struct {
		Ticket  string `json:"ticket"`
		Path    string `json:"path"`
		Machine string `json:"machine"`
	}
	if err := peers.NewClient(p).Call(ctx, "/launch/terminal", map[string]any{"do": "open", "cols": cols, "rows": rows}, &opened); err != nil {
		return nil, nil, "", err
	}
	q := url.Values{"ticket": {opened.Ticket}, "cols": {strconv.Itoa(cols)}, "rows": {strconv.Itoa(rows)}}
	wsURL, err := peerWebSocketURL(p.URL, opened.Path, q)
	if err != nil {
		return nil, nil, "", err
	}
	conn, err := wsock.Dial(wsURL, http.Header{"Authorization": {"Bearer " + p.Token}, pairing.E2EHeader: {"1"}, "Ngrok-Skip-Browser-Warning": {"1"}}, 15*time.Second)
	if err != nil {
		return nil, nil, "", err
	}
	box, err := term.NewBox(key, opened.Ticket, false)
	if err != nil {
		conn.Close()
		return nil, nil, "", err
	}
	return conn, box, opened.Machine, nil
}

func openPeerTerminal(dir, name string) error {
	store, err := peers.Load(peers.Path(dir))
	if err != nil {
		return err
	}
	p, ok := store.Find(name)
	if !ok {
		return fmt.Errorf("no peer named %q (corgi agent peers)", name)
	}
	key, err := base64.StdEncoding.DecodeString(p.Key)
	if err != nil || len(key) != 32 {
		return fmt.Errorf("peer %s has no usable key; pair again", p.Name)
	}
	cols, rows := 80, 24
	if w, h, err := xterm.GetSize(int(os.Stdin.Fd())); err == nil {
		cols, rows = w, h
	}
	conn, box, machine, err := dialPeerTerminal(p, key, cols, rows)
	if err != nil {
		return err
	}
	defer conn.Close()
	utils.Infof("corgi: shell on %s (exit or Ctrl-D ends it)\n", firstNonBlank(machine, p.Name))
	fd := int(os.Stdin.Fd())
	if old, err := xterm.MakeRaw(fd); err == nil {
		defer func() { _ = xterm.Restore(fd, old) }()
	}
	sizes := make(chan [2]uint16, 1)
	stop := watchTerminalSize(fd, sizes)
	defer stop()
	code, err := term.Attach(conn, box, os.Stdin, os.Stdout, sizes)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("the shell on %s exited with %d", p.Name, code)
	}
	return nil
}

func firstNonBlank(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func firstWordOr(fallback string, args []string) (string, []string) {
	if len(args) == 0 {
		return fallback, nil
	}
	return args[0], args[1:]
}
