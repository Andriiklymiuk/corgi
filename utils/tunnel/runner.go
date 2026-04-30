package tunnel

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

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
			events <- Event{Service: service, Port: port, Err: err, Done: true}
			return
		}
	} else {
		argv = provider.Cmd(port)
	}
	if len(argv) == 0 {
		events <- Event{Service: service, Port: port, Err: fmt.Errorf("provider %s returned empty command", provider.Name()), Done: true}
		return
	}
	if _, err := exec.LookPath(argv[0]); err != nil {
		events <- Event{
			Service: service,
			Port:    port,
			Err:     fmt.Errorf("%s not found on PATH. Install: %s", argv[0], provider.InstallHint()),
			Done:    true,
		}
		return
	}

	if named != nil {
		events <- Event{Service: service, Port: port, URL: "https://" + named.Hostname}
	}

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		events <- Event{Service: service, Port: port, Err: err, Done: true}
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		events <- Event{Service: service, Port: port, Err: err, Done: true}
		return
	}

	if err := cmd.Start(); err != nil {
		events <- Event{Service: service, Port: port, Err: err, Done: true}
		return
	}

	// cloudflared writes to stderr, ngrok to stdout, localtunnel to stdout.
	// Read both unconditionally — extra empty lines are cheap.
	var wg sync.WaitGroup
	wg.Add(2)
	go scan(&wg, stdout, provider, service, port, events)
	go scan(&wg, stderr, provider, service, port, events)
	wg.Wait()

	_ = cmd.Wait()
	events <- Event{Service: service, Port: port, Done: true}
}

func scan(wg *sync.WaitGroup, r io.Reader, p Provider, service string, port int, events chan<- Event) {
	defer wg.Done()
	scanner := bufio.NewScanner(r)
	// Bump max line size — cloudflared sometimes prints long banner lines.
	scanner.Buffer(make([]byte, 0, 1024*64), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if url := p.ExtractURL(line); url != "" {
			events <- Event{Service: service, Port: port, URL: url}
		}
	}
}
