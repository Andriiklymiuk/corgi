package cmd

import (
	"andriiklymiuk/corgi/utils"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

type psRow struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Port   int    `json:"port,omitempty"`
	Status string `json:"status"`
	URL    string `json:"url,omitempty"`
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
	return row
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
	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		if utils.JSONOutput {
			utils.JSONError("config", err.Error())
		} else {
			utils.Infof("couldn't get services config: %s\n", err)
		}
		os.Exit(1)
	}

	var rows []psRow
	statePath := utils.RunStatePath(utils.CorgiComposePathDir)
	if _, err := os.Stat(statePath); err == nil {
		st, rerr := utils.ReadRunState(statePath)
		if rerr == nil {
			reconciled := utils.ReconcileRunState(st, utils.PidAlive, utils.ContainerRunning)
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
