package tunnel

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

// send delivers ev unless ctx is done first. Returns false if abandoned,
// so callers can stop early instead of blocking on a dead consumer.
func send(ctx context.Context, events chan<- Event, ev Event) bool {
	select {
	case events <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}

// Event is a single state change for one tunnel target. Consumers drain a
// channel of these and re-render the table or log lines accordingly.
type Event struct {
	Service string // logical name (compose service name or "port-3030")
	Port    int    // local port being tunneled
	URL     string // public URL when established; "" otherwise
	Err     error  // non-nil on subprocess failure
	Done    bool   // true when the subprocess has exited
}

// Run spawns the provider's tunnel CLI for one (service, port) target and
// streams Events on `events`. If named is non-nil, runs in named mode with
// the configured hostname (URL emitted immediately). Cancel `ctx` to
// terminate the subprocess. Run returns when the subprocess exits.
func Run(ctx context.Context, provider Provider, service string, port int, named *NamedConfig, events chan<- Event) {
	var argv []string
	if named != nil {
		var err error
		argv, err = provider.CmdNamed(port, *named)
		if err != nil {
			send(ctx, events, Event{Service: service, Port: port, Err: err, Done: true})
			return
		}
	} else {
		argv = provider.Cmd(port)
	}
	if len(argv) == 0 {
		send(ctx, events, Event{Service: service, Port: port, Err: fmt.Errorf("provider %s returned empty command", provider.Name()), Done: true})
		return
	}
	if _, err := exec.LookPath(argv[0]); err != nil {
		send(ctx, events, Event{
			Service: service,
			Port:    port,
			Err:     fmt.Errorf("%s not found on PATH. Install: %s", argv[0], provider.InstallHint()),
			Done:    true,
		})
		return
	}

	if named != nil {
		send(ctx, events, Event{Service: service, Port: port, URL: "https://" + named.Hostname})
	}

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		send(ctx, events, Event{Service: service, Port: port, Err: err, Done: true})
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		send(ctx, events, Event{Service: service, Port: port, Err: err, Done: true})
		return
	}

	if err := cmd.Start(); err != nil {
		send(ctx, events, Event{Service: service, Port: port, Err: err, Done: true})
		return
	}

	// cloudflared writes to stderr, ngrok to stdout, localtunnel to stdout.
	// Read both unconditionally — extra empty lines are cheap.
	var wg sync.WaitGroup
	wg.Add(2)
	go scan(ctx, &wg, stdout, provider, service, port, events)
	go scan(ctx, &wg, stderr, provider, service, port, events)
	wg.Wait()

	_ = cmd.Wait()
	send(ctx, events, Event{Service: service, Port: port, Done: true})
}

func scan(ctx context.Context, wg *sync.WaitGroup, r io.Reader, p Provider, service string, port int, events chan<- Event) {
	defer wg.Done()
	scanner := bufio.NewScanner(r)
	// Bump max line size — cloudflared sometimes prints long banner lines.
	scanner.Buffer(make([]byte, 0, 1024*64), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if url := p.ExtractURL(line); url != "" {
			if !send(ctx, events, Event{Service: service, Port: port, URL: url}) {
				return
			}
		}
	}
}

// BackoffConfig bounds the restart backoff. Zero values fall back to defaults.
type BackoffConfig struct {
	Base time.Duration // first delay; default 500ms
	Max  time.Duration // ceiling; default 30s
}

// RunSupervised keeps a tunnel target alive across subprocess crashes,
// restarting with capped exponential backoff until ctx is cancelled. Each
// attempt delegates to Run. Bound entirely to ctx — returns when ctx is done.
func RunSupervised(ctx context.Context, provider Provider, service string, port int, named *NamedConfig, events chan<- Event, cfg BackoffConfig) {
	base, max := cfg.Base, cfg.Max
	if base <= 0 {
		base = 500 * time.Millisecond
	}
	if max <= 0 {
		max = 30 * time.Second
	}
	delay := base
	for {
		if ctx.Err() != nil {
			return
		}
		Run(ctx, provider, service, port, named, events)
		if ctx.Err() != nil {
			return
		}
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		}
		if delay < max {
			delay *= 2
			if delay > max {
				delay = max
			}
		}
	}
}
