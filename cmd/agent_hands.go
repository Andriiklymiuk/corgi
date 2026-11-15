package cmd

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/peers"
	"andriiklymiuk/corgi/utils/agent/screen"
)

var handsHTTP = &http.Client{Timeout: 30 * time.Second}

func callHands(ctx context.Context, peer string, req handsRequest) (handsResult, error) {
	if err := req.validate(); err != nil {
		return handsResult{}, err
	}
	dir, err := agentDir()
	if err != nil {
		return handsResult{}, err
	}
	var out handsResult
	if peer != "" && peer != handsLocal {
		p, err := findPeer(dir, peer)
		if err != nil {
			return handsResult{}, err
		}
		c := peers.NewClient(p)
		c.MaxAnswer = 8 << 20
		if err := c.Call(ctx, "/launch/screen/hands", req, &out); err != nil {
			return handsResult{}, err
		}
		return out, resultErr(out)
	}
	base := strings.TrimSuffix(localLauncherURL(), "/app")
	if base == "" {
		return handsResult{}, errors.New("corgi agent up is not running on this Mac")
	}
	key, err := handsKey(dir)
	if err != nil {
		return handsResult{}, err
	}
	body, _ := json.Marshal(req)
	hr, _ := http.NewRequestWithContext(ctx, http.MethodPost, base+handsLocalPath, bytes.NewReader(body))
	hr.Header.Set(handsKeyHeader, key)
	hr.Header.Set("Content-Type", "application/json")
	res, err := handsHTTP.Do(hr)
	if err != nil {
		return handsResult{}, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if res.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		if e.Error == "" {
			e.Error = res.Status
		}
		return handsResult{}, errors.New(e.Error)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return handsResult{}, err
	}
	return out, resultErr(out)
}

func resultErr(r handsResult) error {
	if r.OK {
		return nil
	}
	if r.Error == "" {
		return errors.New("the Mac could not do that")
	}
	return errors.New(r.Error)
}

func parseHandsAction(line string) (handsAction, error) {
	f := strings.Fields(line)
	if len(f) == 0 {
		return handsAction{}, errors.New("empty action")
	}
	ints := func(n int) ([]int, error) {
		if len(f) != n+1 {
			return nil, fmt.Errorf("%s takes %d numbers", f[0], n)
		}
		out := make([]int, n)
		for i := range out {
			v, err := strconv.Atoi(f[i+1])
			if err != nil {
				return nil, fmt.Errorf("%s: %q is not a number", f[0], f[i+1])
			}
			out[i] = v
		}
		return out, nil
	}
	a := handsAction{Do: f[0]}
	switch f[0] {
	case "click", "double", "right", "move":
		v, err := ints(2)
		if err != nil {
			return a, err
		}
		a.X, a.Y = v[0], v[1]
	case "drag":
		v, err := ints(4)
		if err != nil {
			return a, err
		}
		a.X, a.Y, a.X2, a.Y2 = v[0], v[1], v[2], v[3]
	case "scroll":
		v, err := ints(2)
		if err != nil {
			return a, err
		}
		a.DX, a.DY = v[0], v[1]
	case "wait":
		v, err := ints(1)
		if err != nil {
			return a, err
		}
		a.MS = v[0]
	case "type":
		a.Text = strings.TrimSpace(strings.TrimPrefix(line, "type"))
	case "key":
		if len(f) != 2 {
			return a, errors.New("key takes one combination, like cmd+s or enter")
		}
		a.Keys = f[1]
	default:
		return a, fmt.Errorf("unknown action %q: click, double, right, move x y; drag x y x2 y2; scroll dx dy; type text; key cmd+s; wait ms", f[0])
	}
	return a, nil
}

var (
	handsOn     string
	handsWindow string
	handsMax    int
	handsOut    string
	handsLookTo bool
)

var agentHandsCmd = &cobra.Command{
	Use:   "hands",
	Short: "Let agents on a peer Mac (or this one) look at and use this Mac, and do it from here",
	Long: `Agents can see and use a Mac's screen through Corgi Bar: a screenshot, a list
of windows, clicks, typing, keys, opening an app. It is off until the Mac that
is used allows it, and that Mac shows a banner while an agent works.

On the Mac to be used:
  corgi agent hands allow "office-mac"   a peer's agents may use this Mac
  corgi agent hands allow here           agents on this Mac may use it
  corgi agent hands deny "office-mac"
  corgi agent hands status

From any machine (the same as the corgi_mac_* MCP tools):
  corgi agent hands windows --on studio
  corgi agent hands look --on studio --out shot.jpg
  corgi agent hands act --on studio "click 640 400" "type hello" "key cmd+s" --look
  corgi agent hands open --on studio Safari

Coordinates are pixels of the last screenshot.`,
}

func handsWho(dir, name string) (string, string, error) {
	if strings.EqualFold(name, handsLocal) {
		return handsLocal, "this Mac", nil
	}
	hash, n, err := findScreenDevice(dir, name)
	return hash, n, err
}

func setHands(on bool) func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		who, name, err := handsWho(dir, args[0])
		if err != nil {
			exitWithError("agent_hands", err, 2)
		}
		_, err = screen.Update(dir, func(s *screen.Settings) error {
			if who == handsLocal {
				s.LocalAgents = on
				return nil
			}
			g := s.Devices[who]
			if g == nil {
				g = &screen.Grant{Name: name, Role: screen.RoleAsk, Since: time.Now()}
				s.Devices[who] = g
			}
			g.Agents = on
			return nil
		})
		if err != nil {
			exitWithError("agent_hands", err, 1)
		}
		if on {
			utils.Infof("✓ agents from %s may use this Mac; Corgi Bar shows a banner while they do\n", name)
		} else {
			utils.Infof("✓ agents from %s may not use this Mac\n", name)
		}
	}
}

var agentHandsAllowCmd = &cobra.Command{Use: "allow <peer|here>", Short: "Let a peer's agents (or this Mac's) use this Mac", Args: cobra.ExactArgs(1), Run: setHands(true)}
var agentHandsDenyCmd = &cobra.Command{Use: "deny <peer|here>", Short: "Stop a peer's agents (or this Mac's) from using this Mac", Args: cobra.ExactArgs(1), Run: setHands(false)}

var agentHandsStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Who may use this Mac through agents",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		s, err := screen.Load(mustAgentDir())
		if err != nil {
			exitWithError("agent_hands", err, 1)
		}
		var who []string
		if s.LocalAgents {
			who = append(who, "this Mac's agents")
		}
		for _, g := range s.Devices {
			if g.Agents {
				who = append(who, g.Name)
			}
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"allowed": who, "barListening": hands.barListening(time.Now())})
			return
		}
		if len(who) == 0 {
			fmt.Println("No agent may use this Mac: corgi agent hands allow <peer|here>")
			return
		}
		fmt.Println("Agents that may use this Mac: " + strings.Join(who, ", "))
	},
}

func runHands(req handsRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	out, err := callHands(ctx, handsOn, req)
	if err != nil {
		exitWithError("agent_hands", err, 1)
	}
	if utils.JSONOutput {
		if handsOut != "" {
			out.Image = ""
		}
		utils.PrintJSON(out)
	}
	if out.Image != "" {
		path := handsOut
		if path == "" {
			path = "corgi-screen-" + time.Now().Format("150405") + ".jpg"
		}
		raw, err := base64.StdEncoding.DecodeString(out.Image)
		if err == nil {
			err = os.WriteFile(path, raw, 0o600)
		}
		if err != nil {
			exitWithError("agent_hands", err, 1)
		}
		utils.Infof("saved %s (%dx%d)\n", path, out.Width, out.Height)
	}
	if !utils.JSONOutput && out.Text != "" {
		fmt.Println(out.Text)
	}
}

var agentHandsLookCmd = &cobra.Command{
	Use: "look", Short: "A screenshot of the Mac (or one window), saved as JPEG", Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		runHands(handsRequest{Do: "look", Window: handsWindow, Max: handsMax})
	},
}

var agentHandsWindowsCmd = &cobra.Command{
	Use: "windows", Short: "The Mac's windows and apps as text (cheap: no picture)", Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) { runHands(handsRequest{Do: "windows"}) },
}

var agentHandsActCmd = &cobra.Command{
	Use: "act <action>...", Short: "Click, type, press keys, scroll or drag on the Mac", Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		req := handsRequest{Do: "act", Look: handsLookTo, Max: handsMax}
		for _, a := range args {
			act, err := parseHandsAction(a)
			if err != nil {
				exitWithError("agent_hands", err, 2)
			}
			req.Actions = append(req.Actions, act)
		}
		runHands(req)
	},
}

var agentHandsOpenCmd = &cobra.Command{
	Use: "open <app|url>", Short: "Open an app or a URL on the Mac", Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		req := handsRequest{Do: "open"}
		if strings.Contains(args[0], "://") {
			req.URL = args[0]
		} else {
			req.App = args[0]
		}
		runHands(req)
	},
}

func localHandsRequest(method, path string, body []byte, timeout time.Duration) (int, []byte, error) {
	dir, err := agentDir()
	if err != nil {
		return 0, nil, err
	}
	base := strings.TrimSuffix(localLauncherURL(), "/app")
	if base == "" {
		return 0, nil, errors.New("corgi agent up is not running on this Mac")
	}
	key, err := handsKey(dir)
	if err != nil {
		return 0, nil, err
	}
	req, _ := http.NewRequest(method, base+path, bytes.NewReader(body))
	req.Header.Set(handsKeyHeader, key)
	req.Header.Set("Content-Type", "application/json")
	res, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	return res.StatusCode, raw, err
}

var handsDoneFile string

var agentHandsBarNextCmd = &cobra.Command{
	Use:    "bar-next",
	Short:  "Corgi Bar waits for the next agent job",
	Hidden: true,
	Args:   cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		code, raw, err := localHandsRequest(http.MethodGet, handsBarPath, nil, handsWait+10*time.Second)
		if err != nil {
			exitWithError("agent_hands", err, 1)
		}
		if code == http.StatusOK {
			fmt.Print(string(raw))
		}
	},
}

var agentHandsBarDoneCmd = &cobra.Command{
	Use:    "bar-done",
	Short:  "Corgi Bar hands back a job's result",
	Hidden: true,
	Args:   cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		body, err := os.ReadFile(handsDoneFile)
		if err != nil {
			exitWithError("agent_hands", err, 2)
		}
		code, raw, err := localHandsRequest(http.MethodPost, handsBarPath, body, 20*time.Second)
		if err != nil {
			exitWithError("agent_hands", err, 1)
		}
		if code != http.StatusOK {
			exitWithError("agent_hands", errors.New(strings.TrimSpace(string(raw))), 1)
		}
		fmt.Print(string(raw))
	},
}

func init() {
	agentHandsBarDoneCmd.Flags().StringVar(&handsDoneFile, "file", "", "the result JSON")
	agentHandsCmd.AddCommand(agentHandsBarNextCmd, agentHandsBarDoneCmd)
	for _, c := range []*cobra.Command{agentHandsLookCmd, agentHandsWindowsCmd, agentHandsActCmd, agentHandsOpenCmd} {
		c.Flags().StringVar(&handsOn, "on", "", "the peer Mac to use (default: this Mac)")
	}
	agentHandsLookCmd.Flags().StringVar(&handsWindow, "window", "", "only this window (app name or title)")
	for _, c := range []*cobra.Command{agentHandsLookCmd, agentHandsActCmd} {
		c.Flags().IntVar(&handsMax, "max", 0, "longest side of the picture in pixels (default 1280)")
		c.Flags().StringVar(&handsOut, "out", "", "where to save the picture")
	}
	agentHandsActCmd.Flags().BoolVar(&handsLookTo, "look", false, "take a screenshot after the actions")
	agentHandsCmd.AddCommand(agentHandsAllowCmd, agentHandsDenyCmd, agentHandsStatusCmd, agentHandsLookCmd, agentHandsWindowsCmd, agentHandsActCmd, agentHandsOpenCmd)
	agentCmd.AddCommand(agentHandsCmd)
}
