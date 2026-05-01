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

func runTunnelCmd(cmd *cobra.Command, args []string) {
	provider, ok := tunnel.Providers[tunnelProvider]
	if !ok {
		names := tunnel.Names()
		sort.Strings(names)
		fmt.Printf("Unknown provider %q. Available: %s\n", tunnelProvider, strings.Join(names, ", "))
		os.Exit(1)
	}

	type target struct {
		service        string
		port           int
		providerOvr    tunnel.Provider // per-target override (compose `tunnel.provider`); nil = use --provider flag
		named          *tunnel.NamedConfig
	}
	var targets []target

	flagProvider, flagSet := provider, cmd.Flags().Changed("provider")

	if tunnelPort != 0 {
		targets = append(targets, target{service: fmt.Sprintf("port-%d", tunnelPort), port: tunnelPort})
	} else {
		corgi, err := utils.GetCorgiServices(cmd)
		if err != nil {
			fmt.Printf("couldn't load corgi-compose.yml: %s\n", err)
			os.Exit(1)
		}

		var requested map[string]bool
		if len(args) > 0 {
			requested = map[string]bool{}
			for _, a := range args {
				for _, name := range strings.Split(a, ",") {
					name = strings.TrimSpace(name)
					if name != "" {
						requested[name] = true
					}
				}
			}
		}

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

			t := target{service: s.ServiceName, port: s.Port}
			if s.Tunnel != nil {
				named, perTargetProvider, err := resolveTunnel(s, flagProvider, flagSet)
				if err != nil {
					fmt.Printf("✗ %s: %s\n", s.ServiceName, err)
					os.Exit(1)
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
	}

	if len(targets) == 0 {
		fmt.Println("No services with port: matched. Nothing to tunnel.")
		os.Exit(1)
	}

	for _, t := range targets {
		p := t.providerOvr
		if p == nil {
			p = provider
		}
		var err error
		if t.named != nil {
			err = p.PreflightNamedAuth(*t.named)
		} else {
			err = p.PreflightAuth()
		}
		if err != nil {
			fmt.Printf("✗ %s (%s):\n\n%s\n", t.service, p.Name(), err)
			os.Exit(1)
		}
	}

	providersInUse := map[string]bool{}
	for _, t := range targets {
		p := provider
		if t.providerOvr != nil {
			p = t.providerOvr
		}
		providersInUse[p.Name()] = true
	}
	providerList := make([]string, 0, len(providersInUse))
	for n := range providersInUse {
		providerList = append(providerList, n)
	}
	sort.Strings(providerList)
	fmt.Printf("🌐 Tunnels (%s) — Ctrl+C to stop\n\n", strings.Join(providerList, ", "))
	for _, t := range targets {
		mode := "quick"
		p := provider
		if t.providerOvr != nil {
			p = t.providerOvr
		}
		if t.named != nil {
			mode = "named " + t.named.Hostname
		}
		fmt.Printf("  %-30s :%-5d  %s/%s → starting...\n", t.service, t.port, p.Name(), mode)
	}
	fmt.Println()

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
		go func(t target) {
			defer wg.Done()
			p := t.providerOvr
			if p == nil {
				p = provider
			}
			tunnel.Run(ctx, p, t.service, t.port, t.named, events)
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

// resolveTunnel produces (NamedConfig, providerOverride) for a service that
// has `tunnel:` set. Substitutes ${VAR} from shell env first, then from the
// service's runtime .env (<service>/.env), then from the source env file
// declared by copyEnvFromFilePath. CLI --provider flag wins over compose
// `tunnel.provider`. Strict on missing env vars + unsupported providers.
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

// envFilePaths returns env files to consult for ${VAR} substitution, in
// priority order (first match wins):
//  1. The service's runtime .env at <AbsolutePath>/.env — the live file devs
//     edit and where corgi run injects values. Reading this matches what
//     `corgi run` actually loads.
//  2. The source env file declared by copyEnvFromFilePath (e.g.
//     env/source/api.env) — fallback for pre-clone or pre-run usage.
//
// Returns nil for services with neither configured.
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
