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

Exit code is non-zero if anything's down so CI / scripts can consume it.`,
	Run:     runStatus,
	Aliases: []string{"health", "healthcheck"},
}

func init() {
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

	up, down := 0, 0
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
	if down == 0 {
		fmt.Printf("%s🎉 %d/%d healthy%s\n", art.GreenColor, up, up+down, art.WhiteColor)
		return
	}
	fmt.Printf("%s%d down, %d up%s — check `corgi run` logs for the failing services.\n",
		art.RedColor, down, up, art.WhiteColor)
	os.Exit(1)
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
