package cmd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"andriiklymiuk/corgi/utils/agent/pairing"
	"andriiklymiuk/corgi/utils/agent/peers"
	"andriiklymiuk/corgi/utils/agent/term"
	"andriiklymiuk/corgi/utils/agent/wake"
	"andriiklymiuk/corgi/utils/agent/wsock"
)

func findPeer(dir, name string) (peers.Peer, error) {
	store, err := peers.Load(peers.Path(dir))
	if err != nil {
		return peers.Peer{}, err
	}
	if p, ok := store.Find(name); ok {
		return p, nil
	}
	return peers.Peer{}, errors.New("no peer named " + name + " (corgi agent peers)")
}

func peerKey(p peers.Peer) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(p.Key)
	if err != nil || len(key) != 32 {
		return nil, errors.New("peer " + p.Name + " has no usable key; pair again")
	}
	return key, nil
}

type peerScreenIn struct {
	Peer   string `json:"peer"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Scale  int    `json:"scale"`
	FPS    int    `json:"fps"`
	Key    string `json:"key"`
	TS     int64  `json:"ts"`
	Sig    string `json:"sig"`
}

func peerScreenChallenge(dir, name string) (map[string]any, error) {
	p, err := findPeer(dir, name)
	if err != nil {
		return nil, err
	}
	return map[string]any{"peer": p.Name, "device": pairing.HashToken(p.Token), "ts": time.Now().Unix()}, nil
}

func peerScreenOpen(dir string, in peerScreenIn) (map[string]any, error) {
	p, err := findPeer(dir, in.Peer)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	var out map[string]any
	body := map[string]any{"do": "start", "width": in.Width, "height": in.Height, "scale": in.Scale, "fps": in.FPS, "key": in.Key, "ts": in.TS, "sig": in.Sig}
	if err := peers.NewClient(p).Call(ctx, "/launch/screen", body, &out); err != nil {
		return nil, err
	}
	out["peer"] = p.Name
	if list, ok := out["candidates"].([]any); ok {
		for _, c := range list {
			if m, ok := c.(map[string]any); ok {
				if a, ok := m["addr"].(string); ok {
					m["addr"] = absoluteRelay(p.URL, a)
				}
			}
		}
	}
	return out, nil
}

func launchScreenPeerHandler(w http.ResponseWriter, r *http.Request) {
	dir, ok := screenMac(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodGet {
		out, err := peerScreenChallenge(dir, r.URL.Query().Get("peer"))
		if err != nil {
			writeLaunchError(w, http.StatusNotFound, err.Error())
			return
		}
		writeLaunchJSON(w, out)
		return
	}
	var in peerScreenIn
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
		writeLaunchError(w, http.StatusBadRequest, "could not read the request")
		return
	}
	out, err := peerScreenOpen(dir, in)
	if err != nil {
		writeLaunchError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeLaunchJSON(w, out)
}

func launchPeersWakeHandler(w http.ResponseWriter, r *http.Request) {
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
	var in struct {
		Peer string `json:"peer"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil {
		writeLaunchError(w, http.StatusBadRequest, "POST {peer}")
		return
	}
	if err := wakePeer(dir, in.Peer); err != nil {
		writeLaunchError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeLaunchJSON(w, map[string]any{"ok": true, "peer": in.Peer})
}

var sendWake = wake.Send

func wakePeer(dir, name string) error {
	p, err := findPeer(dir, name)
	if err != nil {
		return err
	}
	if p.MAC == "" {
		return errors.New(p.Name + " has not said how to wake it yet: it needs corgi 2.51 and one pulse while awake")
	}
	return sendWake(wake.Card{MAC: p.MAC, Broadcast: p.Broadcast})
}

func launchTerminalPeerWS(token, storePath string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setLaunchHeaders(w)
		d, ok := authorizedDeviceFull(storePath, r.Header.Get("Authorization"))
		if !ok || !screenMacBar(d) || !fromThisMachine(r) {
			writeLaunchError(w, http.StatusForbidden, "only Corgi Bar on this Mac may do that")
			return
		}
		dir, err := agentDir()
		if err != nil {
			writeLaunchError(w, http.StatusInternalServerError, err.Error())
			return
		}
		p, err := findPeer(dir, r.URL.Query().Get("peer"))
		if err != nil {
			writeLaunchError(w, http.StatusNotFound, err.Error())
			return
		}
		key, err := peerKey(p)
		if err != nil {
			writeLaunchError(w, http.StatusBadRequest, err.Error())
			return
		}
		cols, rows := int(sizeArg(r.URL.Query().Get("cols"), 80)), int(sizeArg(r.URL.Query().Get("rows"), 24))
		remote, box, _, err := dialPeerTerminal(p, key, cols, rows)
		if err != nil {
			writeLaunchError(w, http.StatusBadGateway, err.Error())
			return
		}
		defer remote.Close()
		local, err := wsock.Upgrade(w, r)
		if err != nil {
			return
		}
		defer local.Close()
		relayTerminal(local, remote, box)
	})
}

func relayTerminal(local *wsock.Conn, remote *wsock.Conn, box *term.Box) {
	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			_, ct, err := remote.ReadMessage()
			if err != nil {
				return
			}
			m, err := box.Open(ct)
			if err != nil || local.WriteMessage(wsock.OpBinary, m) != nil {
				return
			}
			if len(m) > 0 && m[0] == term.KindExit {
				return
			}
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			_, m, err := local.ReadMessage()
			if err != nil || len(m) == 0 {
				return
			}
			if m[0] != term.KindData && m[0] != term.KindResize && m[0] != term.KindPing {
				continue
			}
			if remote.WriteMessage(wsock.OpBinary, box.Seal(m)) != nil {
				return
			}
		}
	}()
	<-done
}
