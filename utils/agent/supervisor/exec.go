package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils"
)

const outputTailBytes = 8 << 10

const stopGrace = 5 * time.Second

type execProcess struct {
	cmd      *exec.Cmd
	tail     *ringBuffer
	done     chan struct{}
	once     sync.Once
	finished sync.Once
}

func StartProcess(ctx context.Context, cfg SpawnConfig) (Process, error) {
	if err := ValidateSpawnConfig(cfg); err != nil {
		return nil, err
	}

	bin, err := ResolveBin(cfg)
	if err != nil {
		return nil, err
	}
	resolved, err := exec.LookPath(bin)
	if err != nil {
		return nil, fmt.Errorf("%s not found on PATH: %w", bin, err)
	}

	args, err := BuildArgs(cfg)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(resolved, args...)
	cmd.Dir = cfg.Dir
	cmd.Env = BuildEnv(cfg, os.Environ())
	utils.SetProcessGroup(cmd)

	tail := newRingBuffer(outputTailBytes)
	writers := []io.Writer{tail}
	if cfg.OnSessionURL != nil {
		writers = append(writers, newURLScanner(cfg.OnSessionURL))
	}
	if cfg.OnSessionLink != nil {
		writers = append(writers, newSessionLinkScanner(cfg.OnSessionLink))
	}
	if cfg.OnActivity != nil {
		writers = append(writers, activityWriter{cfg.OnActivity})
	}
	var sink io.Writer = tail
	if len(writers) > 1 {
		sink = io.MultiWriter(writers...)
	}
	if cfg.MirrorOutput {
		sink = io.MultiWriter(sink, os.Stderr)
	}
	cmd.Stdout = sink
	cmd.Stderr = sink

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("could not start %s: %w", bin, err)
	}

	p := &execProcess{cmd: cmd, tail: tail, done: make(chan struct{})}
	go p.stopWhenCancelled(ctx)
	return p, nil
}

func (p *execProcess) stopWhenCancelled(ctx context.Context) {
	select {
	case <-ctx.Done():
		p.Stop()
	case <-p.done:
	}
}

func (p *execProcess) Pid() int {
	if p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

func (p *execProcess) Wait() (int, string) {
	err := p.cmd.Wait()
	p.finished.Do(func() { close(p.done) })

	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else {
			code = -1
		}
	}
	return code, p.tail.String()
}

func (p *execProcess) Stop() {
	p.once.Do(func() {
		if p.cmd.Process == nil {
			return
		}
		pid := p.cmd.Process.Pid
		_ = p.cmd.Process.Signal(os.Interrupt)

		select {
		case <-p.done:
		case <-time.After(stopGrace):
		}
		_ = utils.KillProcessGroup(pid)
	})
}

var sessionURLPattern = regexp.MustCompile(`https://claude\.ai/\S+`)

const maxPartialLine = 16 << 10

type urlScanner struct {
	mu      sync.Mutex
	partial []byte
	report  func(string)
	done    bool
}

func newURLScanner(report func(string)) *urlScanner { return &urlScanner{report: report} }

func (u *urlScanner) Write(p []byte) (int, error) {
	u.mu.Lock()
	if u.done {
		u.mu.Unlock()
		return len(p), nil
	}
	u.partial = append(u.partial, p...)
	var hit string
	if m := sessionURLPattern.FindIndex(u.partial); m != nil && m[1] < len(u.partial) {
		hit = strings.TrimRight(string(u.partial[m[0]:m[1]]), `.,;:)]}'"`)
		u.done = true
		u.partial = nil
	} else if len(u.partial) > maxPartialLine {
		u.partial = u.partial[len(u.partial)-maxPartialLine:]
	}
	u.mu.Unlock()
	if hit != "" {
		u.report(hit)
	}
	return len(p), nil
}

var sessionLinkPattern = regexp.MustCompile(`https://claude\.ai/code/(session_[A-Za-z0-9]+)`)

type sessionLinkScanner struct {
	mu      sync.Mutex
	partial []byte
	seen    map[string]bool
	report  func(id string)
}

func newSessionLinkScanner(report func(string)) *sessionLinkScanner {
	return &sessionLinkScanner{seen: map[string]bool{}, report: report}
}

func (s *sessionLinkScanner) Write(p []byte) (int, error) {
	s.mu.Lock()
	s.partial = append(s.partial, p...)
	var hits []string
	cut := 0
	for _, m := range sessionLinkPattern.FindAllSubmatchIndex(s.partial, -1) {
		if m[1] >= len(s.partial) {
			break
		}
		if id := string(s.partial[m[2]:m[3]]); !s.seen[id] {
			s.seen[id] = true
			hits = append(hits, id)
		}
		cut = m[1]
	}
	if cut > 0 {
		s.partial = s.partial[cut:]
	}
	if len(s.partial) > maxPartialLine {
		s.partial = s.partial[len(s.partial)-maxPartialLine:]
	}
	s.mu.Unlock()
	for _, id := range hits {
		s.report(id)
	}
	return len(p), nil
}

type activityWriter struct{ report func() }

func (a activityWriter) Write(p []byte) (int, error) {
	if len(p) > 0 {
		a.report()
	}
	return len(p), nil
}

type ringBuffer struct {
	mu   sync.Mutex
	buf  []byte
	size int
}

func newRingBuffer(size int) *ringBuffer {
	return &ringBuffer{size: size}
}

func (r *ringBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, p...)
	if len(r.buf) > r.size {
		r.buf = r.buf[len(r.buf)-r.size:]
	}
	return len(p), nil
}

func (r *ringBuffer) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.buf)
}
