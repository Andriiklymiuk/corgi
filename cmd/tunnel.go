/*
Copyright © 2026 ANDRII KLYMIUK
*/
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
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
		service string
		port    int
	}
	var targets []target

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
				// In --all mode, skip manualRun services. Explicit name selects them.
				continue
			}
			targets = append(targets, target{service: s.ServiceName, port: s.Port})
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

	if err := provider.PreflightAuth(); err != nil {
		fmt.Printf("✗ %s authentication required:\n\n%s\n", provider.Name(), err)
		os.Exit(1)
	}

	fmt.Printf("🌐 Tunnels (%s) — Ctrl+C to stop\n\n", provider.Name())
	for _, t := range targets {
		fmt.Printf("  %-30s :%-5d → starting...\n", t.service, t.port)
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
		go func(svc string, port int) {
			defer wg.Done()
			tunnel.Run(ctx, provider, svc, port, events)
		}(t.service, t.port)
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
				if ev.Service == "api" {
					fmt.Printf("    DocuSeal webhook: %s/webhooks/docuseal\n", ev.URL)
				}
			}
			mu.Unlock()
		case ev.Done:
			// quiet exit
		}
	}
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
