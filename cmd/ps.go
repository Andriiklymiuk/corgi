package cmd

import (
	"andriiklymiuk/corgi/utils"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

type psRow struct {
	Name      string     `json:"name"`
	Kind      string     `json:"kind"`
	Port      int        `json:"port,omitempty"`
	Status    string     `json:"status"`
	URL       string     `json:"url,omitempty"`
	StartedAt *time.Time `json:"startedAt,omitempty"`
}

var psCmd = &cobra.Command{
	Use:   "ps",
	Short: "Runtime snapshot of declared services and db_services",
	Long: `Reports the topology declared in corgi-compose.yml — name, kind, port —
and infers running/stopped from a port-listening probe where a port is known.

Unlike a single 'corgi run', 'corgi ps' is a separate process and cannot see
in-memory PIDs, so it reports declared topology plus a cheap port probe rather
than live process health.`,
	Run:     runPs,
	Aliases: []string{"processes"},
}

func init() {
	rootCmd.AddCommand(psCmd)
}

func buildPsRows(corgi *utils.CorgiCompose, probe func(port int) bool) []psRow {
	rows := make([]psRow, 0, len(corgi.DatabaseServices)+len(corgi.Services))

	for _, db := range corgi.DatabaseServices {
		rows = append(rows, makePsRow(db.ServiceName, "db_service", db.Port, probe))
	}
	for _, svc := range corgi.Services {
		rows = append(rows, makePsRow(svc.ServiceName, "service", svc.Port, probe))
	}

	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Kind != rows[j].Kind {
			return rows[i].Kind < rows[j].Kind
		}
		return rows[i].Name < rows[j].Name
	})
	return rows
}

func psRowsFromState(st utils.RunState) []psRow {
	rows := make([]psRow, 0, len(st.Services)+len(st.DBServices))
	for _, e := range st.Services {
		rows = append(rows, psRowFromEntry(e))
	}
	for _, e := range st.DBServices {
		rows = append(rows, psRowFromEntry(e))
	}
	return rows
}

func psRowFromEntry(e utils.RunStateEntry) psRow {
	row := psRow{Name: e.Name, Kind: e.Kind, Port: e.Port, Status: e.Status}
	if e.Port != 0 {
		row.URL = fmt.Sprintf("http://localhost:%d", e.Port)
	}
	if !e.StartedAt.IsZero() {
		t := e.StartedAt
		row.StartedAt = &t
	}
	return row
}

// dockerRunnerBootGrace: a container-backed service whose port isn't listening yet
// may still be booting. Don't demote it to "stopped" within this window of start.
const dockerRunnerBootGrace = 15 * time.Second

// probeDockerRunnerServices confirms pid==0 (container-backed) entries by a port
// probe, since Reconcile cannot pid-track them and a dead container would linger
// as "running". Within dockerRunnerBootGrace of start it is left booting; after
// that a closed port marks it stopped and advances StatusChangedAt.
func probeDockerRunnerServices(st utils.RunState, probe func(port int) bool, now time.Time) utils.RunState {
	for i := range st.Services {
		e := &st.Services[i]
		if e.PID != 0 || e.Port == 0 {
			continue
		}
		// Container state beats a port probe: a booting app is running even
		// though its port isn't open yet. Repo-compose containers carry their
		// own names, so a not-found container falls back to the port probe.
		alive := containerCheck(utils.ServiceContainerName(e.Name)) || probe(e.Port)
		if !alive && !e.StartedAt.IsZero() && now.Sub(e.StartedAt) < dockerRunnerBootGrace {
			continue // freshly started; port may not be open yet
		}
		newStatus := "stopped"
		if alive {
			newStatus = "running"
		}
		if newStatus != e.Status {
			e.Status = newStatus
			e.StatusChangedAt = now
		}
	}
	return st
}

// containerCheck is overridable in tests.
var containerCheck = func(containerName string) bool {
	running, err := utils.IsServiceRunning(containerName)
	return err == nil && running
}

func makePsRow(name, kind string, port int, probe func(port int) bool) psRow {
	row := psRow{Name: name, Kind: kind, Port: port, Status: "unknown"}
	if port == 0 {
		return row
	}
	row.URL = fmt.Sprintf("http://localhost:%d", port)
	if probe(port) {
		row.Status = "running"
	} else {
		row.Status = "stopped"
	}
	return row
}

func runPs(cmd *cobra.Command, _ []string) {
	corgi := mustLoadCorgiServices(cmd)

	var rows []psRow
	statePath := utils.RunStatePath(utils.CorgiComposePathDir)
	if _, err := os.Stat(statePath); err == nil {
		st, rerr := utils.ReadRunState(statePath)
		if rerr == nil {
			reconciled := utils.ReconcileRunState(st, utils.PidAlive, utils.ContainerRunning)
			reconciled = probeDockerRunnerServices(reconciled, utils.IsPortListening, time.Now().UTC())
			_ = utils.WriteRunState(statePath, reconciled)
			rows = psRowsFromState(reconciled)
		}
	}
	if rows == nil {
		rows = buildPsRows(corgi, utils.IsPortListening)
	}

	if utils.JSONOutput {
		utils.PrintJSON(rows)
		return
	}

	if len(rows) == 0 {
		utils.Info("No services or db_services declared in corgi-compose.yml.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tKIND\tPORT\tSTATUS\tURL")
	for _, r := range rows {
		port := ""
		if r.Port != 0 {
			port = fmt.Sprintf("%d", r.Port)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", r.Name, r.Kind, port, r.Status, r.URL)
	}
	w.Flush()
}
