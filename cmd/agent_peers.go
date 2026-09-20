package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/pairing"
	"andriiklymiuk/corgi/utils/agent/peers"

	"github.com/spf13/cobra"
)

// Two laptops on the same trackers: they pair with each other the way a
// phone pairs with one, pulse each other every minute, and for every
// tracker both watch, one leads — it fixes and rings, the other stays quiet
// until the leader goes silent. The phone introduces them on its own the
// moment it holds both; these commands do it by hand.

var agentPeersCmd = &cobra.Command{
	Use:   "peers",
	Short: "The other laptops on your trackers: who leads, who is awake",
	Long: `Two laptops that watch the same tracker would each start a fix and each
ring your phone. Paired as peers, one of them leads per tracker and the other
stays quiet, taking over when the leader sleeps.

  corgi agent peers                 who is paired, when each last pulsed, who leads
  corgi agent peers invite          a code for the other laptop (10 minutes, one use)
  corgi agent peers join <url> <code>   pair with the laptop that printed the code
  corgi agent peers lead [off]      this laptop leads every tracker it shares
  corgi agent peers rm <name>       forget a laptop

The phone does the invite and join for you when it is paired with both.`,
	Run: func(cmd *cobra.Command, _ []string) {
		dir, err := agentDir()
		if err != nil {
			exitWithError("agent_peers", err, 1)
		}
		store, err := peers.Load(peers.Path(dir))
		if err != nil {
			exitWithError("agent_peers", err, 1)
		}
		now := time.Now()
		me := peers.Me()
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"me": me, "lead": store.Lead, "peers": store.Peers})
			return
		}
		if len(store.Peers) == 0 {
			fmt.Println("No peer laptops. Pair your phone with both and it introduces them, or:")
			fmt.Println("  here:   corgi agent peers invite")
			fmt.Println("  there:  corgi agent peers join <url> <code>")
			return
		}
		fmt.Printf("%s%s\n", me, leadWord(store.Lead))
		for _, p := range store.Peers {
			state := "silent"
			if p.Alive(now) {
				state = "awake · " + ago(now.Sub(p.SeenAt))
			} else if !p.SeenAt.IsZero() {
				state = "silent since " + ago(now.Sub(p.SeenAt))
			}
			fmt.Printf("  %s%s  %s  %s\n", p.Name, leadWord(p.Lead), state, p.URL)
			if p.Unwell != "" {
				fmt.Printf("    ⚠ cannot run fixes: %s\n", p.Unwell)
			}
			if p.Budget > 0 {
				fmt.Printf("    %d%% of its five-hour window free\n", p.Budget)
			}
			if len(p.Watches) > 0 {
				fmt.Printf("    watches %s\n", strings.Join(p.Watches, ", "))
			}
			working := 0
			for _, s := range p.Sessions {
				if s.Status == "working" || s.Status == "needs_input" {
					working++
					fmt.Printf("    %s %s%s\n", s.Status, firstNonEmpty(s.Display, s.Label), ticketWord(s.Ticket))
				}
			}
			for _, r := range p.Runs {
				if r.State == "failed" || r.State == "blocked" {
					fmt.Printf("    %s %s: %s\n", r.State, r.Ref, r.Reason)
				}
			}
		}
	},
}

func ticketWord(t string) string {
	if t == "" {
		return ""
	}
	return " · " + t
}

func leadWord(lead bool) string {
	if lead {
		return " (leads)"
	}
	return ""
}

func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
}

var agentPeersInviteCmd = &cobra.Command{
	Use:   "invite",
	Short: "A one-time code the other laptop joins with",
	Run: func(cmd *cobra.Command, _ []string) {
		dir, err := agentDir()
		if err != nil {
			exitWithError("agent_peers", err, 1)
		}
		ans, err := requestPairWindowFor(dir, pairing.RolePeer, 6*time.Second)
		if err != nil {
			exitWithError("agent_peers", err, 1)
		}
		base := firstNonEmpty(ans.PublicURL, ans.LocalURL)
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"url": base, "code": ans.Code, "expiresAt": ans.ExpiresAt})
			return
		}
		fmt.Println()
		fmt.Println("  on the other laptop, within 10 minutes:")
		fmt.Printf("    corgi agent peers join %s %s\n", base, ans.Code)
		fmt.Println()
	},
}

var agentPeersJoinCmd = &cobra.Command{
	Use:   "join <url> <code>",
	Short: "Pair with the laptop that printed the code; it pairs back",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		dir, err := agentDir()
		if err != nil {
			exitWithError("agent_peers", err, 1)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		p, err := joinPeerBothWays(ctx, dir, args[0], args[1])
		if err != nil {
			exitWithError("agent_peers", err, 1)
		}
		nudgeDaemon(dir)
		if utils.JSONOutput {
			utils.PrintJSON(p)
			return
		}
		fmt.Printf("Paired with %s (%s). Both daemons now pulse each other; one leads per shared tracker.\n", p.Name, p.URL)
	},
}

var agentPeersLeadCmd = &cobra.Command{
	Use:   "lead [off]",
	Short: "This laptop leads every tracker it shares with a peer",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir, err := agentDir()
		if err != nil {
			exitWithError("agent_peers", err, 1)
		}
		lead := len(args) == 0 || !strings.EqualFold(args[0], "off")
		if err := peers.Update(peers.Path(dir), func(s *peers.Store) { s.Lead = lead }); err != nil {
			exitWithError("agent_peers", err, 1)
		}
		nudgeDaemon(dir)
		if lead {
			fmt.Println("This laptop leads. Peers hear it on the next pulse and go quiet on the trackers you share.")
		} else {
			fmt.Println("No longer asking to lead; the first laptop by name leads.")
		}
	},
}

var agentPeersRmCmd = &cobra.Command{
	Use:   "rm <name>",
	Short: "Forget a peer laptop (and revoke its token here)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir, err := agentDir()
		if err != nil {
			exitWithError("agent_peers", err, 1)
		}
		removed := false
		_ = peers.Update(peers.Path(dir), func(s *peers.Store) { removed = s.Remove(args[0]) })
		if store, err := pairing.Load(pairing.StorePath(dir)); err == nil && store.Revoke(args[0]) {
			_ = pairing.Save(pairing.StorePath(dir), store)
		}
		if !removed {
			exitWithError("agent_peers", fmt.Errorf("no peer named %s", args[0]), 1)
		}
		nudgeDaemon(dir)
		fmt.Printf("Forgot %s. Run the same there: corgi agent peers rm %s\n", args[0], peers.Me())
	},
}

// joinPeerBothWays pairs this laptop with the one at url, then opens a
// window of its own and asks that laptop to pair back through it, so both
// hold a token for the other.
func joinPeerBothWays(ctx context.Context, dir, rawURL, code string) (peers.Peer, error) {
	p, err := peers.Join(ctx, dir, rawURL, code)
	if err != nil {
		return p, err
	}
	mine, err := requestPairWindowFor(dir, pairing.RolePeer, 6*time.Second)
	if err != nil {
		return p, fmt.Errorf("paired one way; the MCP server here would not open a window for %s to pair back: %w", p.Name, err)
	}
	back := map[string]any{"url": firstNonEmpty(mine.PublicURL, mine.LocalURL), "code": mine.Code, "reciprocal": true}
	if err := peers.NewClient(p).Call(ctx, "/launch/peers/join", back, nil); err != nil {
		return p, fmt.Errorf("paired one way; %s could not pair back: %w", p.Name, err)
	}
	return p, nil
}

func init() {
	agentPeersCmd.AddCommand(agentPeersInviteCmd, agentPeersJoinCmd, agentPeersLeadCmd, agentPeersRmCmd)
	agentCmd.AddCommand(agentPeersCmd)
}

// ---- the phone's and the peers' side --------------------------------------

// GET /launch/peers lists them; POST /launch/peers/invite mints a code for
// another laptop; POST /launch/peers/join {url, code} pairs with that
// laptop (and asks it to pair back unless reciprocal); POST
// /launch/peers/pulse is what a peer daemon sends once a minute.

func launchPeersHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	switch r.Method {
	case http.MethodGet:
		store, err := peers.Load(peers.Path(dir))
		if err != nil {
			writeLaunchError(w, http.StatusInternalServerError, err.Error())
			return
		}
		now := time.Now()
		list := make([]map[string]any, 0, len(store.Peers))
		for _, p := range store.Peers {
			list = append(list, map[string]any{"name": p.Name, "url": p.URL, "alive": p.Alive(now), "seenAt": p.SeenAt, "lead": p.Lead, "watches": p.Watches, "since": p.Since})
		}
		writeLaunchJSON(w, map[string]any{"me": peers.Me(), "lead": store.Lead, "peers": list})
	case http.MethodDelete:
		name := launchNameArg(r)
		removed := false
		_ = peers.Update(peers.Path(dir), func(s *peers.Store) { removed = s.Remove(name) })
		if store, err := pairing.Load(pairing.StorePath(dir)); err == nil && store.Revoke(name) {
			_ = pairing.Save(pairing.StorePath(dir), store)
		}
		if !removed {
			writeLaunchError(w, http.StatusNotFound, "no peer named "+name)
			return
		}
		nudgeDaemon(dir)
		writeLaunchJSON(w, map[string]any{"removed": name})
	default:
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET the peers, DELETE ?name= to forget one")
	}
}

func launchPeersInviteHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ans, err := requestPairWindowFor(dir, pairing.RolePeer, 6*time.Second)
	if err != nil {
		writeLaunchError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeLaunchJSON(w, map[string]any{"url": firstNonEmpty(ans.PublicURL, ans.LocalURL), "code": ans.Code, "expiresAt": ans.ExpiresAt, "name": peers.Me()})
}

func launchPeersJoinHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var req struct {
		URL        string `json:"url"`
		Code       string `json:"code"`
		Reciprocal bool   `json:"reciprocal"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeLaunchError(w, http.StatusBadRequest, "could not read the request")
		return
	}
	// A peer laptop may only pair back through the window this laptop
	// opened for it; asking this laptop to go and join a third one is the
	// phone's to do.
	if device, ok := authorizedDeviceFull(pairing.StorePath(dir), r.Header.Get("Authorization")); ok && device.Peer() && !req.Reciprocal {
		writeLaunchError(w, http.StatusForbidden, "a peer laptop only pairs back")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	var p peers.Peer
	if req.Reciprocal {
		p, err = peers.Join(ctx, dir, req.URL, req.Code)
	} else {
		p, err = joinPeerBothWays(ctx, dir, req.URL, req.Code)
	}
	if err != nil {
		writeLaunchError(w, http.StatusBadGateway, err.Error())
		return
	}
	nudgeDaemon(dir)
	writeLaunchJSON(w, map[string]any{"name": p.Name, "url": p.URL, "since": p.Since})
}

func launchPeersPulseHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Only the laptop that owns the token may speak for it: the pulse's
	// name is what the token was paired under, whatever the body says.
	device, ok := authorizedDeviceFull(pairing.StorePath(dir), r.Header.Get("Authorization"))
	if !ok || !device.Peer() {
		writeLaunchError(w, http.StatusForbidden, "only a peer laptop pulses")
		return
	}
	var in peers.Pulse
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
		writeLaunchError(w, http.StatusBadRequest, "could not read the pulse")
		return
	}
	if len(in.Watches) > 200 {
		in.Watches = in.Watches[:200]
	}
	now := time.Now()
	err = peers.Update(peers.Path(dir), func(s *peers.Store) {
		for i := range s.Peers {
			if strings.EqualFold(s.Peers[i].Name, device.Name) {
				s.Peers[i].Absorb(in, now)
			}
		}
	})
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The daemon reads the files it keeps; so does this answer, so a peer
	// hears the board even between daemon ticks.
	writeLaunchJSON(w, daemon.LocalPulse(dir, APP_VERSION))
	nudgeDaemon(dir)
}
