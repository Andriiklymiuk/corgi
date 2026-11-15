package wsock

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func echoServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Token") != "ok" {
			http.Error(w, "who are you", http.StatusUnauthorized)
			return
		}
		c, err := Upgrade(w, r)
		if err != nil {
			return
		}
		defer c.Close()
		for {
			op, msg, err := c.ReadMessage()
			if err != nil {
				return
			}
			if err := c.WriteMessage(op, append([]byte("echo:"), msg...)); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRoundTripAllSizes(t *testing.T) {
	srv := echoServer(t)
	c, err := Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/x?y=1", http.Header{"X-Token": {"ok"}}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, n := range []int{0, 1, 125, 126, 65535, 65536, 1 << 20} {
		msg := bytes.Repeat([]byte{byte(n)}, n)
		if err := c.WriteMessage(OpBinary, msg); err != nil {
			t.Fatal(err)
		}
		op, got, err := c.ReadMessage()
		if err != nil || op != OpBinary || !bytes.Equal(got, append([]byte("echo:"), msg...)) {
			t.Fatalf("size %d: op %d err %v len %d", n, op, err, len(got))
		}
	}
}

func TestRefusedHandshakeSaysWhy(t *testing.T) {
	srv := echoServer(t)
	_, err := Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil, 5*time.Second)
	he, ok := err.(*HandshakeError)
	if !ok || he.Status != http.StatusUnauthorized || !strings.Contains(he.Error(), "who are you") {
		t.Fatalf("got %v", err)
	}
}

func TestPlainRequestIsNotUpgraded(t *testing.T) {
	srv := echoServer(t)
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("X-Token", "ok")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", res.StatusCode)
	}
}

func TestCloseEndsTheReader(t *testing.T) {
	srv := echoServer(t)
	c, err := Dial("ws"+strings.TrimPrefix(srv.URL, "http"), http.Header{"X-Token": {"ok"}}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.WriteMessage(OpClose, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.ReadMessage(); err == nil {
		t.Fatal("read after close should fail")
	}
}

func TestBadSchemeAndURL(t *testing.T) {
	if _, err := Dial("ftp://x", nil, time.Second); err == nil {
		t.Fatal("ftp accepted")
	}
	if _, err := Dial("ws://127.0.0.1:1", nil, time.Second); err == nil {
		t.Fatal("dead port accepted")
	}
}
