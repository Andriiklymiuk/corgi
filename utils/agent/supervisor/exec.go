package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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

	bin, err := SanitizeBin(cfg.Bin)
	if err != nil {
		return nil, err
	}
	resolved, err := exec.LookPath(bin)
	if err != nil {
		return nil, fmt.Errorf("%s not found on PATH: %w", bin, err)
	}

	cmd := exec.Command(resolved, BuildArgs(cfg)...)
	cmd.Dir = cfg.Dir
	cmd.Env = BuildEnv(cfg, os.Environ())
	// Own process group so Stop can take down anything remote control spawned,
	// not just the parent.
	utils.SetProcessGroup(cmd)

	// The tail is held in memory for exit classification only, and is never
	// persisted: a session's output can contain env values and tokens.
	tail := newRingBuffer(outputTailBytes)
	var sink io.Writer = tail
	if cfg.MirrorOutput {
		// Only with --foreground, where a person is watching rather than a log
		// file collecting. stderr, so --json stdout stays pure JSON.
		sink = io.MultiWriter(tail, os.Stderr)
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
			// Kill the whole group: remote control may have spawned sessions.
			_ = utils.KillProcessGroup(pid)
		}
	})
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
