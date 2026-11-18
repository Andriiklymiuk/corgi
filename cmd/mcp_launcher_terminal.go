package cmd

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils/agent/pairing"
	"andriiklymiuk/corgi/utils/agent/screen"
	"andriiklymiuk/corgi/utils/agent/term"
	"andriiklymiuk/corgi/utils/agent/workspace"
	"andriiklymiuk/corgi/utils/agent/wsock"
)

const terminalWSPath = "/launch/terminal/ws"

func terminalDevice(d pairing.Device) bool {
	return !d.Viewer() && d.Family == "" && !screenMacBar(d)
}

type terminalOpen struct {
	Do   string `json:"do"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
	Key  string `json:"key"`
	TS   int64  `json:"ts"`
	Sig  string `json:"sig"`
}

func launchTerminalHandler(w http.ResponseWriter, r *http.Request) {
	dir, d, ok := screenDevice(w, r)
	if !ok {
		return
	}
	if !terminalDevice(d) {
		writeLaunchError(w, http.StatusForbidden, "this device cannot open a terminal here")
		return
	}
	s, err := screen.Load(dir)
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if r.Method == http.MethodGet {
		status := map[string]any{"enabled": s.Terminal, "allowed": s.TerminalFor(d.TokenHash), "machine": macName(), "shell": term.Shell()}
		if s.TerminalFor(d.TokenHash) {
			status["workspaces"] = terminalWorkspaces()
		}
		writeLaunchJSON(w, status)
		return
	}
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET or POST")
		return
	}
	var in terminalOpen
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
		writeLaunchError(w, http.StatusBadRequest, "could not read the request")
		return
	}
	now := time.Now()
	if !s.Terminal {
		writeLaunchError(w, http.StatusForbidden, "Terminals are off on "+macName()+". Turn Terminal on there: Corgi Bar › Settings › Remote screen, or corgi agent term on")
		return
	}
	if !s.TerminalFor(d.TokenHash) {
		writeLaunchError(w, http.StatusForbidden, d.Name+" may not open a terminal on "+macName()+". Turn Terminal on for it there: Corgi Bar › Settings › Remote screen, or corgi agent term allow \""+d.Name+"\"")
		return
	}
	if !d.Peer() {
		g := s.Devices[d.TokenHash]
		key := g.Key
		if key == "" {
			key = strings.TrimSpace(in.Key)
		}
		if key == "" || (g.Key != "" && in.Key != "" && in.Key != g.Key) {
			writeLaunchError(w, http.StatusForbidden, "open the screen once from this phone to set its Face ID key, or send the key that is on file")
			return
		}
		if err := screen.VerifySignature(key, d.TokenHash, in.TS, in.Sig, now); err != nil {
			writeLaunchError(w, http.StatusForbidden, err.Error())
			return
		}
	}
	ticket, err := screen.MintTicket(dir, d.TokenHash, d.Name, now)
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeLaunchJSON(w, map[string]any{"ticket": ticket, "path": terminalWSPath, "machine": macName(), "expires": now.Add(screen.TicketTTL)})
}

func launchTerminalWS(token, storePath string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setLaunchHeaders(w)
		if storePath == "" {
			writeLaunchError(w, http.StatusForbidden, "terminals need paired devices")
			return
		}
		d, ok := authorizedDeviceFull(storePath, r.Header.Get("Authorization"))
		if !ok || !terminalDevice(d) || !d.Encrypted() {
			writeLaunchError(w, http.StatusUnauthorized, "pair this device with a key first")
			return
		}
		dir, err := agentDir()
		if err != nil {
			writeLaunchError(w, http.StatusInternalServerError, err.Error())
			return
		}
		ticket := r.URL.Query().Get("ticket")
		device, name, err := screen.ConsumeTicket(dir, ticket, time.Now())
		if err != nil || device != d.TokenHash {
			writeLaunchError(w, http.StatusForbidden, "this terminal ticket is unknown, used or for another device")
			return
		}
		s, err := screen.Load(dir)
		if err != nil || !s.TerminalFor(device) {
			writeLaunchError(w, http.StatusForbidden, "terminals were turned off for this device")
			return
		}
		key, err := e2eKeyFor(storePath, d)
		if err != nil || key == nil {
			writeLaunchError(w, http.StatusInternalServerError, "the pairing key could not be read")
			return
		}
		box, err := term.NewBox(key, ticket, true)
		if err != nil {
			writeLaunchError(w, http.StatusInternalServerError, err.Error())
			return
		}
		conn, err := wsock.Upgrade(w, r)
		if err != nil {
			return
		}
		defer conn.Close()
		cols, rows := sizeArg(r.URL.Query().Get("cols"), 80), sizeArg(r.URL.Query().Get("rows"), 24)
		_ = screen.Log(dir, screen.Session{Device: device, Name: name, Role: "terminal", Event: "start", At: time.Now()})
		session := term.NewSession(box, conn, s.IdleStopDuration())
		session.Dir = terminalWorkspaceDir(r.URL.Query().Get("workspace"))
		code, err := session.Run(cols, rows)
		event := "end"
		if err != nil {
			event = "refused: " + err.Error()
		}
		_ = screen.Log(dir, screen.Session{Device: device, Name: name, Role: "terminal", Event: event, Addr: "exit " + strconv.Itoa(code), At: time.Now()})
	})
}

type terminalWorkspace struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

func terminalWorkspaces() []terminalWorkspace {
	registry, _, err := agentRegistry()
	if err != nil {
		return nil
	}
	home, _ := os.UserHomeDir()
	var out []terminalWorkspace
	for _, ws := range registry.Sorted() {
		if ws.Status != "" && ws.Status != workspace.StatusOK {
			continue
		}
		shown := ws.AbsPath
		if home != "" && strings.HasPrefix(shown, home+"/") {
			shown = "~" + strings.TrimPrefix(shown, home)
		}
		out = append(out, terminalWorkspace{ID: ws.ID, Path: shown})
	}
	return out
}

func terminalWorkspaceDir(id string) string {
	if id == "" {
		return ""
	}
	registry, _, err := agentRegistry()
	if err != nil {
		return ""
	}
	ws, ok := registry.Find(id)
	if !ok {
		return ""
	}
	return ws.AbsPath
}

func sizeArg(s string, def int) uint16 {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 1000 {
		return uint16(def)
	}
	return uint16(n)
}
