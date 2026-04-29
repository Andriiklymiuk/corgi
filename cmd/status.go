package cmd

import (
	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/art"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/spf13/cobra"
)

// statusCmd is the post-boot sibling of `corgi doctor`:
//
//	doctor → are prereqs in place and ports free BEFORE `corgi run`?
//	status → is each declared service / db actually responding AFTER `corgi run`?
var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Healthcheck every declared service and db_service",
	Long: `Verifies every db_service and (non-manualRun) service declared in
corgi-compose.yml is reachable on its port.

For each db_service:
  - TCP connect on localhost:<port>. If 'healthCheck' is set, or the driver is
    'localstack', corgi additionally does an HTTP GET and accepts any non-5xx
    response as healthy.

For each service:
  - TCP connect on localhost:<port>. If 'healthCheck' is set, corgi does
    GET http://localhost:<port><healthCheck> and accepts any non-5xx response.

Exit code is non-zero if anything's down so CI / scripts can consume it.

Add --watch (or -w) to repoll continuously; --interval controls the cadence.`,
	Run:     runStatus,
	Aliases: []string{"health", "healthcheck"},
}

func init() {
	statusCmd.Flags().BoolP("watch", "w", false, "Repoll continuously; press Ctrl+C to stop")
	statusCmd.Flags().DurationP("interval", "i", 2*time.Second, "Delay between watch polls")
	rootCmd.AddCommand(statusCmd)
}

type statusRow struct {
	Label string // e.g. "db_services.api-db (postgres)"
	Port  int
	Kind  string // "tcp" or "http"
	URL   string // for http kind
}

func runStatus(cmd *cobra.Command, _ []string) {
	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		fmt.Printf("couldn't get services config: %s\n", err)
		os.Exit(1)
	}

	rows := collectStatusRows(corgi)
	if len(rows) == 0 {
		fmt.Println("No services with ports declared in corgi-compose.yml — nothing to check.")
		return
	}

	// Sort by port for predictable output.
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Port < rows[j].Port })

	watch, _ := cmd.Flags().GetBool("watch")
	interval, _ := cmd.Flags().GetDuration("interval")

	if watch {
		runStatusWatch(rows, interval)
		return
	}

	up, down := printStatusOnce(rows)
	if down == 0 {
		fmt.Printf("%s🎉 %d/%d healthy%s\n", art.GreenColor, up, up+down, art.WhiteColor)
		return
	}
	fmt.Printf("%s%d down, %d up%s — check `corgi run` logs for the failing services.\n",
		art.RedColor, down, up, art.WhiteColor)
	os.Exit(1)
}

// printStatusOnce renders the full table once. Returns counts.
func printStatusOnce(rows []statusRow) (up, down int) {
	fmt.Println("🩺 corgi status")
	for _, r := range rows {
		ok, detail := probe(r)
		if ok {
			fmt.Printf("  %s ✅ %-40s %s%s\n", art.GreenColor, r.Label, detail, art.WhiteColor)
			up++
		} else {
			fmt.Printf("  %s ❌ %-40s %s%s\n", art.RedColor, r.Label, detail, art.WhiteColor)
			down++
		}
	}
	fmt.Println()
	return up, down
}

// runStatusWatch prints the initial table, then polls quietly and only
// emits a line when a service's health transitions (up→down or down→up).
// Mimics `kubectl get -w` — quiet while stable, alerts on change. Ctrl+C
// to stop. Reactive, not periodic-spam.
func runStatusWatch(rows []statusRow, interval time.Duration) {
	if interval <= 0 {
		interval = 2 * time.Second
	}

	state := make(map[string]bool, len(rows))
	for _, r := range rows {
		ok, _ := probe(r)
		state[r.Label] = ok
	}
	up, down := printStatusOnce(rows)
	fmt.Printf("%s👀 watching %d targets every %s — Ctrl+C to stop (%d up, %d down)%s\n",
		art.CyanColor, len(rows), interval, up, down, art.WhiteColor)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		for _, r := range rows {
			ok, detail := probe(r)
			prev := state[r.Label]
			if ok == prev {
				continue
			}
			state[r.Label] = ok
			ts := time.Now().Format("15:04:05")
			if ok {
				fmt.Printf("  %s[%s] ✅ %-40s came up — %s%s\n",
					art.GreenColor, ts, r.Label, detail, art.WhiteColor)
			} else {
				fmt.Printf("  %s[%s] ❌ %-40s went down — %s%s\n",
					art.RedColor, ts, r.Label, detail, art.WhiteColor)
			}
		}
	}
}

// collectStatusRows turns the parsed compose into a flat list of things to probe.
func collectStatusRows(corgi *utils.CorgiCompose) []statusRow {
	var rows []statusRow

	for _, db := range corgi.DatabaseServices {
		if db.Port == 0 || db.ManualRun {
			continue
		}
		row := statusRow{
			Label: fmt.Sprintf("db_services.%s (%s)", db.ServiceName, db.Driver),
			Port:  db.Port,
			Kind:  "tcp",
		}
		if db.HealthCheck != "" {
			row.Kind = "http"
			row.URL = fmt.Sprintf("http://localhost:%d%s", db.Port, db.HealthCheck)
		} else if db.Driver == "localstack" {
			// Sensible default for the localstack driver — it ships a canonical health endpoint.
			row.Kind = "http"
			row.URL = fmt.Sprintf("http://localhost:%d/_localstack/health", db.Port)
		}
		rows = append(rows, row)
	}

	for _, svc := range corgi.Services {
		if svc.Port == 0 || svc.ManualRun {
			continue
		}
		row := statusRow{
			Label: fmt.Sprintf("services.%s", svc.ServiceName),
			Port:  svc.Port,
			Kind:  "tcp",
		}
		if svc.HealthCheck != "" {
			row.Kind = "http"
			row.URL = fmt.Sprintf("http://localhost:%d%s", svc.Port, svc.HealthCheck)
		}
		rows = append(rows, row)
	}

	return rows
}

func probe(r statusRow) (bool, string) {
	if r.Kind == "http" {
		ok, code := utils.IsHTTPHealthy(r.URL, 5*time.Second)
		if ok {
			return true, fmt.Sprintf("%s [HTTP %d]", r.URL, code)
		}
		if code == 0 {
			return false, fmt.Sprintf("%s (no response)", r.URL)
		}
		return false, fmt.Sprintf("%s [HTTP %d]", r.URL, code)
	}

	if utils.IsPortListening(r.Port) {
		return true, fmt.Sprintf("localhost:%d listening", r.Port)
	}
	return false, fmt.Sprintf("localhost:%d not listening", r.Port)
}
