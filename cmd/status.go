package cmd

import (
	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/art"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
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

Modes:
  -w, --watch         Repoll continuously and redraw the table in place
                      (live timestamp + counts). Falls back to append-only
                      transition lines for piped stdout, --json, or --quiet.
  -r, --ready         Block until every target is up. Exit 0 when all healthy,
                      exit 1 on --timeout (default 5m). Alias: --until-healthy.
      --service csv   Narrow probes to listed services.
      --json          Machine-readable. One-shot: array. Watch/ready: NDJSON per transition.
  -q, --quiet         Suppress per-line output; rely on exit code only.`,
	Run:     runStatus,
	Aliases: []string{"health", "healthcheck"},
}

func init() {
	statusCmd.Flags().BoolP("watch", "w", false, "Repoll continuously; press Ctrl+C to stop")
	statusCmd.Flags().DurationP("interval", "i", 2*time.Second, "Delay between watch polls")
	statusCmd.Flags().BoolP("ready", "r", false, "Exit 0 once every probed target is up; exit 1 on --timeout (alias: --until-healthy)")
	statusCmd.Flags().Bool("until-healthy", false, "Same as --ready")
	statusCmd.Flags().Duration("timeout", 5*time.Minute, "Max wait for --until-healthy")
	statusCmd.Flags().StringSlice("service", nil, "Limit checks to listed services (csv); applies to both services + db_services")
	statusCmd.Flags().Bool("json", false, "Emit machine-readable JSON. One-shot: array. Watch: NDJSON one-per-transition.")
	statusCmd.Flags().BoolP("quiet", "q", false, "Suppress per-line output; rely on exit code only")
	rootCmd.AddCommand(statusCmd)
}

type statusRow struct {
	Label string
	Port  int
	Kind  string
	URL   string
}

type statusFlags struct {
	watch        bool
	interval     time.Duration
	untilHealthy bool
	timeout      time.Duration
	jsonOut      bool
	quiet        bool
}

func readStatusFlags(cmd *cobra.Command) statusFlags {
	watch, _ := cmd.Flags().GetBool("watch")
	interval, _ := cmd.Flags().GetDuration("interval")
	untilHealthy, _ := cmd.Flags().GetBool("until-healthy")
	if ready, _ := cmd.Flags().GetBool("ready"); ready {
		untilHealthy = true
	}
	timeout, _ := cmd.Flags().GetDuration("timeout")
	jsonOut, _ := cmd.Flags().GetBool("json")
	quiet, _ := cmd.Flags().GetBool("quiet")
	return statusFlags{watch, interval, untilHealthy, timeout, jsonOut, quiet}
}

func resolveStatusRows(cmd *cobra.Command) []statusRow {
	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		fmt.Printf("couldn't get services config: %s\n", err)
		os.Exit(1)
	}

	rows := collectStatusRows(corgi)
	if len(rows) == 0 {
		fmt.Println("No services with ports declared in corgi-compose.yml — nothing to check.")
		return nil
	}

	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Port < rows[j].Port })

	serviceFilter, _ := cmd.Flags().GetStringSlice("service")
	if len(serviceFilter) > 0 {
		rows = filterRows(rows, serviceFilter)
		if len(rows) == 0 {
			fmt.Printf("No matching services for filter %v.\n", serviceFilter)
			os.Exit(1)
		}
	}
	return rows
}

func runStatusOnce(rows []statusRow, f statusFlags) {
	up, down := probeAll(rows)
	if f.jsonOut {
		emitJSON(up, down)
		if anyDown(up, down) {
			os.Exit(1)
		}
		return
	}
	if !f.quiet {
		renderProbeResults(up, down)
	}
	if !anyDown(up, down) {
		if !f.quiet {
			fmt.Printf("%s🎉 %d/%d healthy%s\n", art.GreenColor, len(up), len(up)+len(down), art.WhiteColor)
		}
		return
	}
	if !f.quiet {
		fmt.Printf("%s%d down, %d up%s — check `corgi run` logs for the failing services.\n",
			art.RedColor, len(down), len(up), art.WhiteColor)
	}
	os.Exit(1)
}

func runStatus(cmd *cobra.Command, _ []string) {
	rows := resolveStatusRows(cmd)
	if rows == nil {
		return
	}

	f := readStatusFlags(cmd)
	switch {
	case f.untilHealthy:
		runStatusUntilHealthy(rows, f.interval, f.timeout, f.jsonOut, f.quiet)
	case f.watch:
		runStatusWatch(rows, f.interval, f.jsonOut, f.quiet)
	default:
		runStatusOnce(rows, f)
	}
}

type probeResult struct {
	Row     statusRow
	Healthy bool
	Detail  string
}

func probeAll(rows []statusRow) (up, down []probeResult) {
	for _, r := range rows {
		ok, detail := probe(r)
		pr := probeResult{Row: r, Healthy: ok, Detail: detail}
		if ok {
			up = append(up, pr)
		} else {
			down = append(down, pr)
		}
	}
	return
}

func anyDown(up, down []probeResult) bool { return len(down) > 0 }

func renderProbeResults(up, down []probeResult) {
	fmt.Println("🩺 corgi status")
	all := append(append([]probeResult{}, up...), down...)
	sort.SliceStable(all, func(i, j int) bool { return all[i].Row.Port < all[j].Row.Port })
	for _, pr := range all {
		if pr.Healthy {
			fmt.Printf("  %s ✅ %-40s %s%s\n", art.GreenColor, pr.Row.Label, pr.Detail, art.WhiteColor)
		} else {
			fmt.Printf("  %s ❌ %-40s %s%s\n", art.RedColor, pr.Row.Label, pr.Detail, art.WhiteColor)
		}
	}
	fmt.Println()
}

func emitJSON(up, down []probeResult) {
	type entry struct {
		Label   string `json:"label"`
		Port    int    `json:"port"`
		Kind    string `json:"kind"`
		URL     string `json:"url,omitempty"`
		Healthy bool   `json:"healthy"`
		Detail  string `json:"detail"`
	}
	var out []entry
	for _, pr := range up {
		out = append(out, entry{pr.Row.Label, pr.Row.Port, pr.Row.Kind, pr.Row.URL, true, pr.Detail})
	}
	for _, pr := range down {
		out = append(out, entry{pr.Row.Label, pr.Row.Port, pr.Row.Kind, pr.Row.URL, false, pr.Detail})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(b))
}

func filterRows(rows []statusRow, names []string) []statusRow {
	want := map[string]bool{}
	for _, n := range names {
		for _, p := range strings.Split(n, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				want[p] = true
			}
		}
	}
	var out []statusRow
	for _, r := range rows {
		bare := r.Label
		if i := strings.IndexByte(bare, '.'); i >= 0 {
			bare = bare[i+1:]
		}
		if i := strings.IndexByte(bare, ' '); i >= 0 {
			bare = bare[:i]
		}
		if want[bare] {
			out = append(out, r)
		}
	}
	return out
}

func runStatusWatchTTY(rows []statusRow, interval time.Duration) {
	fmt.Print("\033[?1049h\033[H")
	restore := func() { fmt.Print("\033[?1049l") }
	defer restore()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		restore()
		os.Exit(0)
	}()

	results := probeAllParallel(rows)
	fmt.Print(buildWatchFrame(rows, results, interval, time.Now()))

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		results = probeAllParallel(rows)
		var buf strings.Builder
		buf.WriteString("\033[H\033[J")
		buf.WriteString(buildWatchFrame(rows, results, interval, time.Now()))
		fmt.Print(buf.String())
	}
}

func runStatusWatch(rows []statusRow, interval time.Duration, jsonOut, quiet bool) {
	if interval <= 0 {
		interval = 2 * time.Second
	}

	switch {
	case jsonOut:
		results := probeAllParallel(rows)
		up, down := splitResults(rows, results)
		emitJSON(up, down)
		runWatchAppend(rows, results, interval, true, false)
	case quiet:
		runWatchAppend(rows, nil, interval, false, true)
	case !isStdoutTTY():
		results := probeAllParallel(rows)
		fmt.Print(buildWatchFrame(rows, results, interval, time.Now()))
		runWatchAppend(rows, results, interval, false, false)
	default:
		runStatusWatchTTY(rows, interval)
	}
}

func runWatchAppend(rows []statusRow, seed map[string]probeResult, interval time.Duration, jsonOut, quiet bool) {
	state := make(map[string]bool, len(rows))
	if seed != nil {
		for _, r := range rows {
			state[r.Label] = seed[r.Label].Healthy
		}
	} else {
		for _, r := range rows {
			ok, _ := probe(r)
			state[r.Label] = ok
		}
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		results := probeAllParallel(rows)
		for _, r := range rows {
			pr := results[r.Label]
			if state[r.Label] == pr.Healthy {
				continue
			}
			state[r.Label] = pr.Healthy
			emitTransition(r, pr.Healthy, pr.Detail, jsonOut, quiet)
		}
	}
}

func probeAllParallel(rows []statusRow) map[string]probeResult {
	out := make(map[string]probeResult, len(rows))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, r := range rows {
		wg.Add(1)
		go func(row statusRow) {
			defer wg.Done()
			ok, detail := probe(row)
			pr := probeResult{Row: row, Healthy: ok, Detail: detail}
			mu.Lock()
			out[row.Label] = pr
			mu.Unlock()
		}(r)
	}
	wg.Wait()
	return out
}

func splitResults(rows []statusRow, results map[string]probeResult) (up, down []probeResult) {
	for _, r := range rows {
		pr := results[r.Label]
		if pr.Healthy {
			up = append(up, pr)
		} else {
			down = append(down, pr)
		}
	}
	return
}

// rows must be pre-sorted by port — runStatus does it once before the loop.
func buildWatchFrame(rows []statusRow, results map[string]probeResult, interval time.Duration, now time.Time) string {
	var buf strings.Builder
	buf.WriteString("🩺 corgi status\n")
	upCount := 0
	for _, r := range rows {
		pr := results[r.Label]
		if pr.Healthy {
			upCount++
			fmt.Fprintf(&buf, "  %s ✅ %-40s %s%s\n", art.GreenColor, r.Label, pr.Detail, art.WhiteColor)
		} else {
			fmt.Fprintf(&buf, "  %s ❌ %-40s %s%s\n", art.RedColor, r.Label, pr.Detail, art.WhiteColor)
		}
	}
	down := len(rows) - upCount
	fmt.Fprintf(&buf, "\n%s👀 watching %d targets every %s — last update %s (%d up, %d down) — Ctrl+C to stop%s\n",
		art.CyanColor, len(rows), interval, now.Format("15:04:05"), upCount, down, art.WhiteColor)
	return buf.String()
}

func isStdoutTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func runStatusUntilHealthy(rows []statusRow, interval, timeout time.Duration, jsonOut, quiet bool) {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	deadline := time.Now().Add(timeout)
	state := initStateMap(rows)

	if !jsonOut && !quiet {
		fmt.Printf("%s⏳ waiting up to %s for %d targets to become healthy...%s\n",
			art.CyanColor, timeout, len(rows), art.WhiteColor)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	check := func() bool { return checkAllHealthy(rows, state, jsonOut, quiet) }

	if check() {
		finalize(rows, jsonOut, quiet, true)
		return
	}
	for range ticker.C {
		if check() {
			finalize(rows, jsonOut, quiet, true)
			return
		}
		if time.Now().After(deadline) {
			finalize(rows, jsonOut, quiet, false)
			os.Exit(1)
		}
	}
}

func initStateMap(rows []statusRow) map[string]bool {
	state := make(map[string]bool, len(rows))
	for _, r := range rows {
		state[r.Label] = false
	}
	return state
}

func checkAllHealthy(rows []statusRow, state map[string]bool, jsonOut, quiet bool) bool {
	allUp := true
	for _, r := range rows {
		ok, detail := probe(r)
		prev := state[r.Label]
		state[r.Label] = ok
		if ok != prev {
			emitTransition(r, ok, detail, jsonOut, quiet)
		}
		if !ok {
			allUp = false
		}
	}
	return allUp
}

func emitTransition(r statusRow, ok bool, detail string, jsonOut, quiet bool) {
	if jsonOut {
		type ev struct {
			Time    string `json:"time"`
			Label   string `json:"label"`
			Port    int    `json:"port"`
			Healthy bool   `json:"healthy"`
			Detail  string `json:"detail"`
		}
		b, _ := json.Marshal(ev{time.Now().Format(time.RFC3339), r.Label, r.Port, ok, detail})
		fmt.Println(string(b))
		return
	}
	if quiet {
		return
	}
	ts := time.Now().Format("15:04:05")
	if ok {
		fmt.Printf("  %s[%s] ✅ %-40s came up — %s%s\n", art.GreenColor, ts, r.Label, detail, art.WhiteColor)
	} else {
		fmt.Printf("  %s[%s] ❌ %-40s went down — %s%s\n", art.RedColor, ts, r.Label, detail, art.WhiteColor)
	}
}

func finalize(rows []statusRow, jsonOut, quiet, healthy bool) {
	up, down := probeAll(rows)
	if jsonOut {
		emitJSON(up, down)
		return
	}
	if quiet {
		return
	}
	if healthy {
		fmt.Printf("%s🎉 all %d targets healthy%s\n", art.GreenColor, len(rows), art.WhiteColor)
	} else {
		fmt.Printf("%s⌛ timeout — %d up, %d down%s\n", art.RedColor, len(up), len(down), art.WhiteColor)
		renderProbeResults(up, down)
	}
}

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
		ok, code, reason := utils.IsHTTPHealthy(r.URL, 5*time.Second)
		if ok {
			return true, fmt.Sprintf("%s [HTTP %d]", r.URL, code)
		}
		if code != 0 {
			return false, fmt.Sprintf("%s [HTTP %d]", r.URL, code)
		}
		return false, fmt.Sprintf("%s (%s)", r.URL, reason)
	}

	if utils.IsPortListening(r.Port) {
		return true, fmt.Sprintf("localhost:%d listening", r.Port)
	}
	return false, fmt.Sprintf("localhost:%d not listening", r.Port)
}
