package term

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/wsock"
)

var shared = bytes.Repeat([]byte{7}, 32)

func TestBoxBothWays(t *testing.T) {
	s, _ := NewBox(shared, "t1", true)
	c, _ := NewBox(shared, "t1", false)
	for i := 0; i < 3; i++ {
		got, err := c.Open(s.Seal([]byte("out")))
		if err != nil || string(got) != "out" {
			t.Fatal(err)
		}
		got, err = s.Open(c.Seal([]byte("in")))
		if err != nil || string(got) != "in" {
			t.Fatal(err)
		}
	}
}

func TestBoxRefusesTamperReplayAndOtherTickets(t *testing.T) {
	s, _ := NewBox(shared, "t1", true)
	c, _ := NewBox(shared, "t1", false)
	frame := s.Seal([]byte("ls"))
	bad := append([]byte(nil), frame...)
	bad[0] ^= 1
	if _, err := c.Open(bad); err == nil {
		t.Fatal("tampered frame opened")
	}
	if _, err := c.Open(frame); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Open(frame); err == nil {
		t.Fatal("replayed frame opened")
	}
	other, _ := NewBox(shared, "t2", false)
	if _, err := other.Open(s.Seal([]byte("x"))); err == nil {
		t.Fatal("another ticket's key opened it")
	}
	echo, _ := NewBox(shared, "t1", true)
	if _, err := echo.Open(echo.Seal([]byte("x"))); err == nil {
		t.Fatal("a side accepted its own direction")
	}
	if _, err := NewBox([]byte("short"), "t", true); err == nil {
		t.Fatal("short key accepted")
	}
}

func TestMessages(t *testing.T) {
	cols, rows, ok := ParseResize(Resize(120, 40))
	if !ok || cols != 120 || rows != 40 {
		t.Fatal("resize")
	}
	if code, ok := ParseExit(Exit(-3)); !ok || code != -3 {
		t.Fatal("exit")
	}
	if _, _, ok := ParseResize([]byte{1}); ok {
		t.Fatal("short resize")
	}
}

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func TestRealShellOverWebSocket(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := wsock.Upgrade(w, r)
		if err != nil {
			return
		}
		defer c.Close()
		box, _ := NewBox(shared, "ticket", true)
		_, _ = NewSession(box, c, time.Minute).Run(80, 24)
	}))
	defer srv.Close()
	conn, err := wsock.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	box, _ := NewBox(shared, "ticket", false)
	in, typer := io.Pipe()
	out := &lockedBuf{}
	sizes := make(chan [2]uint16, 1)
	sizes <- [2]uint16{100, 30}
	done := make(chan int, 1)
	go func() {
		code, _ := Attach(conn, box, in, out, sizes)
		done <- code
	}()
	_, _ = io.WriteString(typer, "echo hello-$((40+2))\n")
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), "hello-42") {
		if time.Now().After(deadline) {
			t.Fatalf("no output, got %q", out.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, _ = io.WriteString(typer, "exit 3\n")
	select {
	case code := <-done:
		if code != 3 {
			t.Fatalf("exit code %d", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the shell never exited")
	}
	close(sizes)
	typer.Close()
}
