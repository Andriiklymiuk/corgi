package term

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"os/user"
	"sync"
	"sync/atomic"
	"time"
)

type Wire interface {
	WriteMessage(op byte, data []byte) error
	ReadMessage() (byte, []byte, error)
	Close() error
}

const opBinary = 2

var live atomic.Int32

const MaxSessions = 4

var ErrBusy = errors.New("four terminals are open on this machine already")

func Shell() string {
	if s := os.Getenv("SHELL"); s != "" {
		if _, err := os.Stat(s); err == nil {
			return s
		}
	}
	for _, s := range []string{"/bin/zsh", "/bin/bash", "/bin/sh"} {
		if _, err := os.Stat(s); err == nil {
			return s
		}
	}
	return "/bin/sh"
}

func shellCmd(dir string) *exec.Cmd {
	cmd := exec.Command(Shell(), "-l")
	home, _ := os.UserHomeDir()
	if u, err := user.Current(); err == nil && home == "" {
		home = u.HomeDir
	}
	cmd.Dir = home
	if info, err := os.Stat(dir); dir != "" && err == nil && info.IsDir() {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor", "CORGI_TERMINAL=1")
	return cmd
}

type Session struct {
	Dir     string
	box     *Box
	wire    Wire
	idle    time.Duration
	writeMu sync.Mutex
}

func NewSession(box *Box, wire Wire, idle time.Duration) *Session {
	if idle <= 0 {
		idle = 30 * time.Minute
	}
	return &Session{box: box, wire: wire, idle: idle}
}

func (s *Session) send(plain []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.wire.WriteMessage(opBinary, s.box.Seal(plain))
}

func (s *Session) Run(cols, rows uint16) (int, error) {
	if live.Add(1) > MaxSessions {
		live.Add(-1)
		return -1, ErrBusy
	}
	defer live.Add(-1)
	tty, cmd, err := startPTY(shellCmd(s.Dir), cols, rows)
	if err != nil {
		return -1, err
	}
	defer tty.Close()
	activity := make(chan struct{}, 1)
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := tty.Read(buf)
			if n > 0 {
				if s.send(Data(buf[:n])) != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	go s.pump(tty, activity)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(s.idle)
	defer timer.Stop()
	for {
		select {
		case err := <-done:
			code := 0
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				code = ee.ExitCode()
			}
			_ = s.send(Exit(code))
			return code, nil
		case <-activity:
			if !timer.Stop() {
				<-timer.C
			}
			timer.Reset(s.idle)
		case <-timer.C:
			_ = cmd.Process.Kill()
		}
	}
}

func (s *Session) pump(tty io.ReadWriteCloser, activity chan<- struct{}) {
	defer tty.Close()
	for {
		_, ct, err := s.wire.ReadMessage()
		if err != nil {
			return
		}
		m, err := s.box.Open(ct)
		if err != nil || len(m) == 0 {
			return
		}
		select {
		case activity <- struct{}{}:
		default:
		}
		switch m[0] {
		case KindData:
			if _, err := tty.Write(m[1:]); err != nil {
				return
			}
		case KindResize:
			if cols, rows, ok := ParseResize(m); ok {
				resizePTY(tty, cols, rows)
			}
		}
	}
}
