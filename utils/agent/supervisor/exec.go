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

// outputTailBytes is how much of a process's output is kept for exit
// classification. Remote control can run for days, so the buffer is a ring:
// only the tail matters, and an unbounded one would be a slow leak.
const outputTailBytes = 8 << 10

// stopGrace is how long a process gets to exit after SIGTERM before the group
// is killed. Long enough for remote control to close its session cleanly.
const stopGrace = 5 * time.Second

// execProcess is a real `claude remote-control` process.
type execProcess struct {
	cmd      *exec.Cmd
	tail     *ringBuffer
	done     chan struct{}
	once     sync.Once
	finished sync.Once
}

// StartProcess launches remote control for a workspace. It is the production
// Starter; tests inject their own.
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
	// Own process group so Stop can take down anything remote control spawned,
	// not just the parent.
	utils.SetProcessGroup(cmd)

	// The tail is held in memory for exit classification only, and is never
	// persisted: a session's output can contain env values and tokens.
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
		// Only with --foreground, where a person is watching rather than a log
		// file collecting. stderr, so --json stdout stays pure JSON.
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

// stopWhenCancelled ties the process to the supervisor's context so a shutdown
// does not leave an orphan holding the workspace.
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

// Stop asks the process group to exit, escalating to a kill if it lingers.
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
		// Always sweep the group, not only after a timeout. Remote control
		// spawns sessions, and a parent that exits promptly on SIGINT would
		// otherwise leave them running — which is the whole reason the child
		// gets its own process group.
		_ = utils.KillProcessGroup(pid)
	})
}

// sessionURLPattern matches the claude.ai link remote control prints when a
// session opens. Best-effort: if the format drifts, the URL is simply absent
// and everything else still works.
var sessionURLPattern = regexp.MustCompile(`https://claude\.ai/\S+`)

// maxPartialLine bounds the scanner's memory; a URL will not span more.
const maxPartialLine = 16 << 10

// urlScanner watches process output and reports the first complete claude.ai
// URL. A match touching the end of the buffer is held back — the next write
// could extend it.
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

// sessionLinkPattern matches the per-session claude.ai links remote control
// prints as sessions spawn (…/code/session_<id>?from=cli, wrapped in OSC-8
// hyperlink escapes). Only the id is captured — the query string and escape
// bytes terminate the character class — and callers rebuild the canonical URL
// from it. This is the ONLY reliable source for a session's web link: the ids
// `claude agents --json` prints are local UUIDs the site does not resolve.
var sessionLinkPattern = regexp.MustCompile(`https://claude\.ai/code/(session_[A-Za-z0-9]+)`)

// sessionLinkScanner reports every DISTINCT per-session id seen in the process
// output, unlike urlScanner which reports one URL and stops. A match touching
// the end of the buffer is held back — the next write could extend the id.
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
			break // may extend on the next write; keep for then
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

// activityWriter reports that output happened, for the idle wake lock. It keeps
// no bytes — it is a signal, not a sink — so the callback must stay cheap.
type activityWriter struct{ report func() }

func (a activityWriter) Write(p []byte) (int, error) {
	if len(p) > 0 {
		a.report()
	}
	return len(p), nil
}

// ringBuffer keeps the last n bytes written to it.
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
