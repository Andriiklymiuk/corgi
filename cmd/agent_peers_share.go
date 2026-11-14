package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/pairing"
	"andriiklymiuk/corgi/utils/agent/peers"
)

const shareWindowFor = 15 * time.Minute

type shareWindow struct {
	At      time.Time `json:"at"`
	Until   time.Time `json:"until"`
	Secrets bool      `json:"secrets,omitempty"`
}

func shareWindowPath(dir string) string { return filepath.Join(dir, "peers-share.json") }

func openShareWindow(dir string, secrets bool, now time.Time) error {
	raw, _ := json.Marshal(shareWindow{At: now.Add(-time.Second), Until: now.Add(shareWindowFor), Secrets: secrets})
	return os.WriteFile(shareWindowPath(dir), raw, 0o600)
}

func takeShareWindow(dir string, device pairing.Device, now time.Time) (shareWindow, error) {
	raw, err := os.ReadFile(shareWindowPath(dir))
	if err != nil {
		return shareWindow{}, errors.New("no setup window here: on that machine run corgi agent peers invite --setup")
	}
	var win shareWindow
	if json.Unmarshal(raw, &win) != nil || now.After(win.Until) {
		_ = os.Remove(shareWindowPath(dir))
		return shareWindow{}, errors.New("the setup window closed: corgi agent peers invite --setup there again")
	}
	if device.CreatedAt.Before(win.At) {
		return shareWindow{}, errors.New("this machine paired before the setup window opened; it is for the one that joins with that code")
	}
	_ = os.Remove(shareWindowPath(dir))
	return win, nil
}

func launchPeersPackHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, postOnlyMessage)
		return
	}
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	device, ok := authorizedDeviceFull(pairing.StorePath(dir), r.Header.Get("Authorization"))
	if !ok || !device.Peer() {
		writeLaunchError(w, http.StatusForbidden, "only a peer laptop takes the setup")
		return
	}
	win, err := takeShareWindow(dir, device, time.Now())
	if err != nil {
		writeLaunchError(w, http.StatusForbidden, err.Error())
		return
	}
	pack, err := buildCarryPack(dir, carryOptions{tokens: win.Secrets, env: win.Secrets, claude: win.Secrets})
	if err != nil {
		writeLaunchError(w, http.StatusConflict, err.Error())
		return
	}
	writeLaunchJSON(w, pack)
}

func fetchPeerPack(ctx context.Context, p peers.Peer) (peerPack, error) {
	client := peers.NewClient(p)
	client.MaxAnswer = carryTotalLimit * 2
	client.HTTP.Timeout = 2 * time.Minute
	var pack peerPack
	if err := client.Call(ctx, "/launch/peers/pack", map[string]any{}, &pack); err != nil {
		return pack, err
	}
	if len(pack.Workspaces) == 0 {
		return pack, errors.New(p.Name + " sent no workspace")
	}
	return pack, nil
}

func peerURL(dir string, ans pairAnswer) string {
	if ans.PublicURL != "" {
		return ans.PublicURL
	}
	raw, _ := os.ReadFile(filepath.Join(dir, mcpAddrName))
	addr := strings.TrimSpace(string(raw))
	if asksForNonLoopback(addr) {
		if _, port, err := net.SplitHostPort(addr); err == nil {
			if ip := outboundIP(); ip != "" {
				return utils.PlainHTTPURL(net.JoinHostPort(ip, port), "")
			}
		}
	}
	return ans.LocalURL
}
