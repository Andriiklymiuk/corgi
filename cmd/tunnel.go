/*
Copyright © 2026 ANDRII KLYMIUK
*/
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/tunnel"

	"github.com/spf13/cobra"
)

var (
	tunnelProvider string
	tunnelPort     int
)

var tunnelCmd = &cobra.Command{
	Use:   "tunnel [service-names]",
	Short: "Open public HTTPS tunnels for declared services",
	Long: `Spawns one tunnel subprocess per selected service and prints public URLs.
Default provider: cloudflared (free, no signup, Quick Tunnels).

By default tunnels every services.<name> in corgi-compose.yml that has a
` + "`port:`" + ` field set and is not manualRun. Pass service names (csv) to
narrow the set, or --port to tunnel a raw local port without compose lookup.`,
	Example: `corgi tunnel
corgi tunnel api
corgi tunnel api,api-2
corgi tunnel --port 3030
corgi tunnel --provider ngrok api`,
	Run: runTunnelCmd,
}

type tunnelTarget struct {
	service     string
	port        int
	providerOvr tunnel.Provider // per-target override (compose `tunnel.provider`); nil = use --provider flag
	named       *tunnel.NamedConfig
}

func (t tunnelTarget) effectiveProvider(fallback tunnel.Provider) tunnel.Provider {
	if t.providerOvr != nil {
		return t.providerOvr
	}
	return fallback
}

func parseRequestedServices(args []string) map[string]bool {
	if len(args) == 0 {
		return nil
	}
	requested := map[string]bool{}
	for _, a := range args {
		for _, name := range strings.Split(a, ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				requested[name] = true
			}
		}
	}
	return requested
}

func buildTargetsFromCompose(cmd *cobra.Command, args []string, flagProvider tunnel.Provider, flagSet bool) ([]tunnelTarget, error) {
	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		return nil, fmt.Errorf("couldn't load corgi-compose.yml: %w", err)
	}

	requested := parseRequestedServices(args)
	var targets []tunnelTarget

	for _, s := range corgi.Services {
		if s.Port == 0 {
			continue
		}
		if requested != nil {
			if !requested[s.ServiceName] {
				continue
			}
		} else if s.ManualRun {
			continue
		}

		t := tunnelTarget{service: s.ServiceName, port: s.Port}
		if s.Tunnel != nil {
			named, perTargetProvider, err := resolveTunnel(s, flagProvider, flagSet)
			if err != nil {
				return nil, fmt.Errorf("✗ %s: %w", s.ServiceName, err)
			}
			t.named = named
			t.providerOvr = perTargetProvider
		}
		targets = append(targets, t)
	}

	if requested != nil {
		seen := map[string]bool{}
		for _, t := range targets {
			seen[t.service] = true
		}
		for name := range requested {
			if !seen[name] {
				fmt.Printf("⚠ unknown service %q (no compose entry or no port: set)\n", name)
			}
		}
	}
	return targets, nil
}

func preflightTargets(targets []tunnelTarget, provider tunnel.Provider) error {
	for _, t := range targets {
		p := t.effectiveProvider(provider)
		var err error
		if t.named != nil {
			err = p.PreflightNamedAuth(*t.named)
		} else {
			err = p.PreflightAuth()
		}
		if err != nil {
			return fmt.Errorf("✗ %s (%s):\n\n%w", t.service, p.Name(), err)
		}
	}
	return nil
}

func printTunnelSummary(targets []tunnelTarget, provider tunnel.Provider) {
	providersInUse := map[string]bool{}
	for _, t := range targets {
		providersInUse[t.effectiveProvider(provider).Name()] = true
	}
	providerList := make([]string, 0, len(providersInUse))
	for n := range providersInUse {
		providerList = append(providerList, n)
	}
	sort.Strings(providerList)
	fmt.Printf("🌐 Tunnels (%s) — Ctrl+C to stop\n\n", strings.Join(providerList, ", "))
	for _, t := range targets {
		mode := "quick"
		if t.named != nil {
			mode = "named " + t.named.Hostname
		}
		fmt.Printf("  %-30s :%-5d  %s/%s → starting...\n", t.service, t.port, t.effectiveProvider(provider).Name(), mode)
	}
	fmt.Println()
}

func runTunnelTargets(targets []tunnelTarget, provider tunnel.Provider) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		fmt.Println("\n→ Closing tunnels...")
		cancel()
	}()

	events := make(chan tunnel.Event, 32)
	var wg sync.WaitGroup
	for _, t := range targets {
		wg.Add(1)
		go func(t tunnelTarget) {
			defer wg.Done()
			tunnel.Run(ctx, t.effectiveProvider(provider), t.service, t.port, t.named, events)
		}(t)
	}
	go func() {
		wg.Wait()
		close(events)
	}()

	urls := map[string]string{}
	var mu sync.Mutex
	for ev := range events {
		switch {
		case ev.Err != nil:
			fmt.Printf("  ✗ %-28s :%-5d → %s\n", ev.Service, ev.Port, ev.Err)
		case ev.URL != "":
			mu.Lock()
			if urls[ev.Service] == "" {
				urls[ev.Service] = ev.URL
				fmt.Printf("  ✓ %-28s :%-5d → %s\n", ev.Service, ev.Port, ev.URL)
			}
			mu.Unlock()
		case ev.Done:
			// quiet exit
		}
	}
}

func runTunnelCmd(cmd *cobra.Command, args []string) {
	provider, ok := tunnel.Providers[tunnelProvider]
	if !ok {
		names := tunnel.Names()
		sort.Strings(names)
		fmt.Printf("Unknown provider %q. Available: %s\n", tunnelProvider, strings.Join(names, ", "))
		os.Exit(1)
	}

	flagProvider, flagSet := provider, cmd.Flags().Changed("provider")

	var targets []tunnelTarget
	if tunnelPort != 0 {
		targets = []tunnelTarget{{service: fmt.Sprintf("port-%d", tunnelPort), port: tunnelPort}}
	} else {
		built, err := buildTargetsFromCompose(cmd, args, flagProvider, flagSet)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		targets = built
	}

	if len(targets) == 0 {
		fmt.Println("No services with port: matched. Nothing to tunnel.")
		os.Exit(1)
	}

	if err := preflightTargets(targets, provider); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	printTunnelSummary(targets, provider)
	runTunnelTargets(targets, provider)
}

// Resolves a service's tunnel: block into (NamedConfig, provider).
// Substitutes ${VAR} from shell, runtime .env, then source env (in order).
// CLI --provider beats compose. Errors on missing vars or unknown provider.
func resolveTunnel(s utils.Service, flagProvider tunnel.Provider, flagSet bool) (*tunnel.NamedConfig, tunnel.Provider, error) {
	cfg := s.Tunnel

	fileEnv := map[string]string{}
	for _, path := range envFilePaths(s) {
		envMap, err := tunnel.LoadEnvFile(path)
		if err != nil {
			return nil, nil, fmt.Errorf("read env file %s: %w", path, err)
		}
		for k, v := range envMap {
			if _, exists := fileEnv[k]; !exists {
				fileEnv[k] = v
			}
		}
	}

	var missing []string
	hostname := tunnel.Substitute(cfg.Hostname, fileEnv, &missing)
	name := tunnel.Substitute(cfg.Name, fileEnv, &missing)

	if hostname == "" || strings.Contains(hostname, "${") || strings.Contains(hostname, "$") && len(missing) > 0 {
		return nil, nil, tunnel.MissingError("tunnel.hostname", missing)
	}

	providerName := cfg.Provider
	if providerName == "" {
		providerName = "cloudflared"
	}
	if flagSet {
		return &tunnel.NamedConfig{Hostname: hostname, Name: name}, flagProvider, nil
	}
	p, ok := tunnel.Providers[providerName]
	if !ok {
		return nil, nil, fmt.Errorf("unknown tunnel.provider %q (compose). Valid: %s", providerName, strings.Join(tunnel.Names(), ", "))
	}
	return &tunnel.NamedConfig{Hostname: hostname, Name: name}, p, nil
}

// Env files to check for ${VAR}, first match wins:
//  1. <service>/.env — live file devs edit, what corgi run reads.
//  2. copyEnvFromFilePath source — fallback before clone/run.
func envFilePaths(s utils.Service) []string {
	var paths []string
	if s.AbsolutePath != "" {
		paths = append(paths, filepath.Join(s.AbsolutePath, ".env"))
	}
	if s.CopyEnvFromFilePath != "" {
		if filepath.IsAbs(s.CopyEnvFromFilePath) {
			paths = append(paths, s.CopyEnvFromFilePath)
		} else {
			paths = append(paths, filepath.Join(utils.CorgiComposePathDir, s.CopyEnvFromFilePath))
		}
	}
	return paths
}

// Cancel fn for tunnels spawned by `corgi run --tunnel`. run.go's signal
// handler calls it before os.Exit so the subprocesses die first.
var runTunnelsCancel context.CancelFunc

// Spawns one tunnel per service with a resolvable `tunnel:` block,
// alongside `corgi run`. Skips (with a warning) when env vars are missing
// or auth isn't set up — keeps the rest of the stack running.
type runTarget struct {
	service  string
	port     int
	provider tunnel.Provider
	named    *tunnel.NamedConfig
}

func collectRunTargets(services []utils.Service) []runTarget {
	var targets []runTarget
	for _, s := range services {
		if s.Tunnel == nil || s.Port == 0 || s.ManualRun {
			continue
		}
		named, p, err := resolveTunnel(s, nil, false)
		if err != nil {
			fmt.Printf("🌐 ⚠ skipping tunnel for %s: %s\n", s.ServiceName, err)
			continue
		}
		if err := p.PreflightNamedAuth(*named); err != nil {
			fmt.Printf("🌐 ⚠ skipping tunnel for %s (%s):\n    %s\n", s.ServiceName, p.Name(), firstLine(err.Error()))
			continue
		}
		targets = append(targets, runTarget{
			service:  s.ServiceName,
			port:     s.Port,
			provider: p,
			named:    named,
		})
	}
	return targets
}

func startTunnelsForRun(services []utils.Service) {
	targets := collectRunTargets(services)
	if len(targets) == 0 {
		fmt.Println("🌐 no tunnels to start (no resolvable tunnel: blocks)")
		return
	}

	fmt.Printf("🌐 opening %d tunnel(s) alongside services — Ctrl+C to stop everything\n", len(targets))
	for _, t := range targets {
		fmt.Printf("  %-30s :%-5d  %s/named %s\n", t.service, t.port, t.provider.Name(), t.named.Hostname)
	}

	ctx, cancel := context.WithCancel(context.Background())
	runTunnelsCancel = cancel

	events := make(chan tunnel.Event, 32)
	var wg sync.WaitGroup
	for _, t := range targets {
		wg.Add(1)
		go func(t runTarget) {
			defer wg.Done()
			tunnel.Run(ctx, t.provider, t.service, t.port, t.named, events)
		}(t)
	}
	go func() {
		wg.Wait()
		close(events)
	}()
	go func() {
		urls := map[string]string{}
		for ev := range events {
			switch {
			case ev.Err != nil:
				fmt.Printf("  🌐 ✗ %-28s :%-5d → %s\n", ev.Service, ev.Port, ev.Err)
			case ev.URL != "" && urls[ev.Service] == "":
				urls[ev.Service] = ev.URL
				fmt.Printf("  🌐 ✓ %-28s :%-5d → %s\n", ev.Service, ev.Port, ev.URL)
			}
		}
	}()
}

// Called from run.go on SIGINT, before os.Exit. Cancels the tunnel ctx
// so exec.CommandContext sends SIGKILL, then sleeps briefly so the kill
// syscalls land before the parent exits.
func stopRunTunnels() {
	if runTunnelsCancel == nil {
		return
	}
	fmt.Println("🌐 closing tunnels...")
	runTunnelsCancel()
	time.Sleep(300 * time.Millisecond)
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}

func init() {
	rootCmd.AddCommand(tunnelCmd)

	defaults := tunnel.Names()
	sort.Strings(defaults)
	tunnelCmd.Flags().StringVar(
		&tunnelProvider,
		"provider",
		"cloudflared",
		fmt.Sprintf("Tunnel provider (%s)", strings.Join(defaults, "|")),
	)
	tunnelCmd.Flags().IntVar(
		&tunnelPort,
		"port",
		0,
		"Raw local port to tunnel; skips corgi-compose.yml lookup",
	)
}
