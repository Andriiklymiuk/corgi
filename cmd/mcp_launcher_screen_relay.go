package cmd

import (
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils/agent/screen"
	"andriiklymiuk/corgi/utils/agent/wsock"
)

const (
	screenRelayPath   = "/launch/screen/relay"
	screenRelayPrefix = "relay:"
	screenRelayIdle   = 2 * time.Minute
	screenRelayMax    = 2048
)

func mintScreenRelay(dir, deviceHash, name string, now time.Time) (screen.Candidate, bool) {
	t, err := screen.MintTicket(dir, screenRelayPrefix+deviceHash, name, now)
	if err != nil {
		return screen.Candidate{}, false
	}
	return screen.Candidate{Addr: screenRelayPath + "?t=" + url.QueryEscape(t), Kind: "relay"}, true
}

func absoluteRelay(base, addr string) string {
	if !strings.HasPrefix(addr, "/") {
		return addr
	}
	b := strings.TrimRight(base, "/")
	switch {
	case strings.HasPrefix(b, "https://"):
		b = "wss://" + strings.TrimPrefix(b, "https://")
	case strings.HasPrefix(b, "http://"):
		b = "ws://" + strings.TrimPrefix(b, "http://")
	default:
		return addr
	}
	return b + addr
}

func launchScreenRelay() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setLaunchHeaders(w)
		dir, err := agentDir()
		if err != nil {
			writeLaunchError(w, http.StatusInternalServerError, err.Error())
			return
		}
		now := time.Now()
		device, name, err := screen.ConsumeTicket(dir, r.URL.Query().Get("t"), now)
		if err != nil || !strings.HasPrefix(device, screenRelayPrefix) {
			writeLaunchError(w, http.StatusForbidden, "this relay ticket is unknown or used")
			return
		}
		s, err := screen.Load(dir)
		if err != nil || !s.Enabled || s.Reach == screen.ReachNearby {
			writeLaunchError(w, http.StatusForbidden, "remote screen is off or nearby only")
			return
		}
		host, _ := screen.CurrentHost(dir, now)
		if host == nil {
			writeLaunchError(w, http.StatusServiceUnavailable, "Corgi Bar is not sharing")
			return
		}
		udp, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: host.Port})
		if err != nil {
			writeLaunchError(w, http.StatusBadGateway, err.Error())
			return
		}
		defer udp.Close()
		conn, err := wsock.Upgrade(w, r)
		if err != nil {
			return
		}
		defer conn.Close()
		hash := strings.TrimPrefix(device, screenRelayPrefix)
		_ = screen.Log(dir, screen.Session{Device: hash, Name: name, Role: "tunnel", Event: "start", At: now})
		up, down := relayDatagrams(conn, udp, screenRelayIdle)
		_ = screen.Log(dir, screen.Session{Device: hash, Name: name, Role: "tunnel", Event: "end", Addr: humanBytes(up) + " up, " + humanBytes(down) + " down", At: time.Now()})
	})
}

func relayDatagrams(conn *wsock.Conn, udp *net.UDPConn, idle time.Duration) (up, down int64) {
	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		buf := make([]byte, screenRelayMax)
		for {
			_ = udp.SetReadDeadline(time.Now().Add(idle))
			n, err := udp.Read(buf)
			if err != nil {
				return
			}
			down += int64(n)
			if conn.WriteMessage(wsock.OpBinary, buf[:n]) != nil {
				return
			}
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			_ = conn.SetReadDeadline(time.Now().Add(idle))
			op, m, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if op != wsock.OpBinary || len(m) == 0 || len(m) > screenRelayMax {
				continue
			}
			up += int64(len(m))
			if _, err := udp.Write(m); err != nil {
				return
			}
		}
	}()
	<-done
	_ = conn.Close()
	_ = udp.Close()
	<-done
	return up, down
}

func humanBytes(n int64) string {
	units := []string{"B", "KB", "MB", "GB"}
	v, u := float64(n), 0
	for v >= 1024 && u < len(units)-1 {
		v /= 1024
		u++
	}
	if u == 0 {
		return strconv.FormatInt(n, 10) + " B"
	}
	return strconv.FormatFloat(v, 'f', 1, 64) + " " + units[u]
}
