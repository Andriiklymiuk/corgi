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

func send(ctx context.Context, events chan<- Event, ev Event) bool {
	select {
	case events <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}

type Event struct {
	Service string
	Port    int
	URL     string
	Err     error
	Done    bool
}

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
		if named.Hostname == "" {
			send(ctx, events, Event{Service: service, Port: port, Done: true,
				Err: fmt.Errorf("named tunnel %q has no hostname - pass the DNS name routed to it (cloudflared tunnel route dns %s <host>)", named.Name, named.Name)})
			return
		}
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

type BackoffConfig struct {
	Base time.Duration
	Max  time.Duration
}

func RunSupervised(ctx context.Context, provider Provider, service string, port int, named *NamedConfig, events chan<- Event, cfg BackoffConfig) {
	base, limit := cfg.Base, cfg.Max
	if base <= 0 {
		base = 500 * time.Millisecond
	}
	if limit <= 0 {
		limit = 30 * time.Second
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
		if delay < limit {
			delay *= 2
			if delay > limit {
				delay = limit
			}
		}
	}
}
