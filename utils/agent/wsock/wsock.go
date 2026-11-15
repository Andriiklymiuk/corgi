package wsock

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	OpText   = 1
	OpBinary = 2
	OpClose  = 8
	OpPing   = 9
	OpPong   = 10

	MaxMessage = 4 << 20
	guid       = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
)

var ErrClosed = errors.New("the connection closed")

type Conn struct {
	conn     net.Conn
	r        *bufio.Reader
	client   bool
	writeMu  sync.Mutex
	closeMu  sync.Once
	closeErr error
}

func accept(key string) string {
	h := sha1.Sum([]byte(key + guid))
	return base64.StdEncoding.EncodeToString(h[:])
}

func headerHas(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for _, part := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

func Upgrade(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	if r.Method != http.MethodGet || !headerHas(r.Header, "Connection", "upgrade") || !headerHas(r.Header, "Upgrade", "websocket") {
		http.Error(w, "websocket only", http.StatusBadRequest)
		return nil, errors.New("not a websocket request")
	}
	if r.Header.Get("Sec-WebSocket-Version") != "13" {
		w.Header().Set("Sec-WebSocket-Version", "13")
		http.Error(w, "websocket version 13 only", http.StatusUpgradeRequired)
		return nil, errors.New("unsupported websocket version")
	}
	key := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key"))
	if raw, err := base64.StdEncoding.DecodeString(key); err != nil || len(raw) != 16 {
		http.Error(w, "bad websocket key", http.StatusBadRequest)
		return nil, errors.New("bad websocket key")
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "this server cannot upgrade", http.StatusInternalServerError)
		return nil, errors.New("response cannot be hijacked")
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + accept(key) + "\r\n\r\n"
	if _, err := rw.WriteString(resp); err != nil {
		conn.Close()
		return nil, err
	}
	if err := rw.Flush(); err != nil {
		conn.Close()
		return nil, err
	}
	return &Conn{conn: conn, r: rw.Reader}, nil
}

func Dial(rawURL string, header http.Header, timeout time.Duration) (*Conn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	host := u.Host
	var conn net.Conn
	d := &net.Dialer{Timeout: timeout}
	switch u.Scheme {
	case "ws", "http":
		if u.Port() == "" {
			host += ":80"
		}
		conn, err = d.Dial("tcp", host)
	case "wss", "https":
		if u.Port() == "" {
			host += ":443"
		}
		conn, err = dialTLS(d, host, u.Hostname())
	default:
		return nil, fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	if err != nil {
		return nil, err
	}
	keyRaw := make([]byte, 16)
	_, _ = rand.Read(keyRaw)
	key := base64.StdEncoding.EncodeToString(keyRaw)
	path := u.RequestURI()
	var b strings.Builder
	fmt.Fprintf(&b, "GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n", path, u.Host, key)
	for k, vs := range header {
		for _, v := range vs {
			fmt.Fprintf(&b, "%s: %s\r\n", k, v)
		}
	}
	b.WriteString("\r\n")
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := io.WriteString(conn, b.String()); err != nil {
		conn.Close()
		return nil, err
	}
	r := bufio.NewReader(conn)
	resp, err := http.ReadResponse(r, &http.Request{Method: http.MethodGet})
	if err != nil {
		conn.Close()
		return nil, err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		conn.Close()
		return nil, &HandshakeError{Status: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}
	if resp.Header.Get("Sec-WebSocket-Accept") != accept(key) {
		conn.Close()
		return nil, errors.New("the server answered with the wrong websocket accept key")
	}
	_ = conn.SetDeadline(time.Time{})
	return &Conn{conn: conn, r: r, client: true}, nil
}

type HandshakeError struct {
	Status int
	Body   string
}

func (e *HandshakeError) Error() string {
	if e.Body != "" {
		return fmt.Sprintf("refused (%d): %s", e.Status, e.Body)
	}
	return fmt.Sprintf("refused (%d)", e.Status)
}

func (c *Conn) WriteMessage(op byte, data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.writeFrame(op, data)
}

func (c *Conn) writeFrame(op byte, data []byte) error {
	header := make([]byte, 0, 14)
	header = append(header, 0x80|op)
	mask := byte(0)
	if c.client {
		mask = 0x80
	}
	n := len(data)
	switch {
	case n < 126:
		header = append(header, mask|byte(n))
	case n <= 0xFFFF:
		header = append(header, mask|126, byte(n>>8), byte(n))
	default:
		header = append(header, mask|127)
		header = binary.BigEndian.AppendUint64(header, uint64(n))
	}
	payload := data
	if c.client {
		var key [4]byte
		_, _ = rand.Read(key[:])
		header = append(header, key[:]...)
		payload = make([]byte, n)
		for i := range data {
			payload[i] = data[i] ^ key[i%4]
		}
	}
	if _, err := c.conn.Write(append(header, payload...)); err != nil {
		return err
	}
	return nil
}

func (c *Conn) readFrame() (fin bool, op byte, payload []byte, err error) {
	var h [2]byte
	if _, err = io.ReadFull(c.r, h[:]); err != nil {
		return
	}
	fin = h[0]&0x80 != 0
	if h[0]&0x70 != 0 {
		err = errors.New("reserved websocket bits set")
		return
	}
	op = h[0] & 0x0F
	masked := h[1]&0x80 != 0
	if masked == c.client {
		err = errors.New("websocket masking is the wrong way round")
		return
	}
	n := uint64(h[1] & 0x7F)
	switch n {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(c.r, ext[:]); err != nil {
			return
		}
		n = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(c.r, ext[:]); err != nil {
			return
		}
		n = binary.BigEndian.Uint64(ext[:])
	}
	if n > MaxMessage {
		err = fmt.Errorf("websocket frame of %d bytes is too large", n)
		return
	}
	if op >= OpClose && (n > 125 || !fin) {
		err = errors.New("bad websocket control frame")
		return
	}
	var key [4]byte
	if masked {
		if _, err = io.ReadFull(c.r, key[:]); err != nil {
			return
		}
	}
	payload = make([]byte, n)
	if _, err = io.ReadFull(c.r, payload); err != nil {
		return
	}
	if masked {
		for i := range payload {
			payload[i] ^= key[i%4]
		}
	}
	return
}

func (c *Conn) ReadMessage() (byte, []byte, error) {
	var msgOp byte
	var msg []byte
	for {
		fin, op, payload, err := c.readFrame()
		if err != nil {
			return 0, nil, err
		}
		switch op {
		case OpPing:
			if err := c.WriteMessage(OpPong, payload); err != nil {
				return 0, nil, err
			}
			continue
		case OpPong:
			continue
		case OpClose:
			_ = c.WriteMessage(OpClose, payload)
			c.conn.Close()
			return 0, nil, ErrClosed
		case 0:
			if msgOp == 0 {
				return 0, nil, errors.New("continuation without a start")
			}
		default:
			if msgOp != 0 {
				return 0, nil, errors.New("new message inside a fragmented one")
			}
			msgOp = op
		}
		if len(msg)+len(payload) > MaxMessage {
			return 0, nil, errors.New("websocket message too large")
		}
		msg = append(msg, payload...)
		if fin {
			return msgOp, msg, nil
		}
	}
}

func (c *Conn) Close() error {
	c.closeMu.Do(func() {
		_ = c.WriteMessage(OpClose, []byte{0x03, 0xE8})
		c.closeErr = c.conn.Close()
	})
	return c.closeErr
}

func (c *Conn) SetReadDeadline(t time.Time) error { return c.conn.SetReadDeadline(t) }
