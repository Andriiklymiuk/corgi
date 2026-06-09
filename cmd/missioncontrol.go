package cmd

import (
	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/art"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

var missionControlCmd = &cobra.Command{
	Use:     "mission-control",
	Aliases: []string{"mc"},
	Short:   "One live pane: every service's run state + its branch/PR/CI",
	Long: `Aggregates, in one refreshing read-only view, each declared service's
run state (reusing 'corgi status' probes) and its per-service agent work —
current branch, draft/open/merged PR, and CI status — read locally via git
and gh/glab.

  --watch          Repoll and reprint the frame until Ctrl+C.
  --interval       Delay between refreshes (default 3s).
  --service csv    Narrow to listed services.
  --no-agent-work  Skip the git/gh probe (run-state only, faster).
  --json           Emit one MissionSnapshot object on stdout (a snapshot,
                   not a stream). Human chrome goes to stderr.`,
	Run: runMissionControl,
}

func init() {
	missionControlCmd.Flags().BoolP("watch", "w", false, "Repoll continuously; Ctrl+C to stop")
	missionControlCmd.Flags().DurationP("interval", "i", 3*time.Second, "Delay between refreshes")
	missionControlCmd.Flags().StringSlice("service", nil, "Limit to listed services (csv)")
	missionControlCmd.Flags().Bool("no-agent-work", false, "Skip the branch/PR/CI probe")
	missionControlCmd.Flags().Bool("json", false, "Emit one MissionSnapshot object on stdout")
	rootCmd.AddCommand(missionControlCmd)
}

func runMissionControl(cmd *cobra.Command, _ []string) {
	rows := resolveStatusRows(cmd) // reuse status.go: load compose, collect+sort+filter
	if rows == nil {
		return
	}
	jsonOut, _ := cmd.Flags().GetBool("json")
	watch, _ := cmd.Flags().GetBool("watch")
	interval, _ := cmd.Flags().GetDuration("interval")
	noAgent, _ := cmd.Flags().GetBool("no-agent-work")
	composePath := utils.CorgiComposePath

	probe := agentWorkProber(cmd, noAgent)

	if !watch {
		runMissionOnce(composePath, rows, probe, jsonOut)
		return
	}
	// Watch loops until Ctrl+C; cancel on signal so the loop exits cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runMissionLoop(ctx, composePath, rows, probe, interval, jsonOut)
}

// agentWorkProber returns a name->AgentWork probe that resolves each service's
// repo dir from the loaded compose and runs utils.ProbeAgentWork. When disabled
// (or compose unavailable) it returns a probe that yields nil for everything.
func agentWorkProber(cmd *cobra.Command, disabled bool) func(name string) *utils.AgentWork {
	if disabled {
		return func(string) *utils.AgentWork { return nil }
	}
	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil || corgi == nil {
		return func(string) *utils.AgentWork { return nil }
	}
	dirs := map[string]string{}
	for _, s := range corgi.Services {
		// Prefer the resolved AbsolutePath (honors --service-dir/-branch
		// overrides); fall back to resolving the compose path: directly.
		dir := s.AbsolutePath
		if dir == "" && s.Path != "" {
			dir = utils.ServiceRepoDir(s.Path)
		}
		if dir != "" {
			dirs[s.ServiceName] = dir
		}
	}
	return func(name string) *utils.AgentWork {
		dir, ok := dirs[name]
		if !ok {
			return nil
		}
		return utils.ProbeAgentWork(dir)
	}
}

func runMissionOnce(composePath string, rows []statusRow, probe func(string) *utils.AgentWork, jsonOut bool) {
	snap := buildMissionSnapshot(composePath, rows, probe)
	if jsonOut {
		utils.PrintJSON(snap)
		return
	}
	utils.Info(buildMissionFrame(snap, 0, time.Now()))
}

func runMissionLoop(ctx context.Context, composePath string, rows []statusRow, probe func(string) *utils.AgentWork, interval time.Duration, jsonOut bool) {
	if interval <= 0 {
		interval = 3 * time.Second
	}
	render := func() {
		snap := buildMissionSnapshot(composePath, rows, probe)
		if jsonOut {
			utils.PrintJSON(snap) // one object per tick under --json --watch
			return
		}
		if utils.IsTTY() {
			fmt.Print("\033[H\033[J") // clear + home
		}
		utils.Info(buildMissionFrame(snap, interval, time.Now()))
	}
	render() // first frame immediately
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			render()
		}
	}
}

// MissionSnapshot is the single object emitted by `mission-control --json`.
// It is a snapshot (one object), not the NDJSON stream that `status --watch`
// produces.
type MissionSnapshot struct {
	ComposePath string           `json:"composePath,omitempty"`
	GeneratedAt time.Time        `json:"generatedAt"`
	Services    []MissionService `json:"services"`
	Summary     MissionSummary   `json:"summary"`
}

type MissionService struct {
	Name      string           `json:"name"`
	Kind      string           `json:"kind"`
	Port      int              `json:"port,omitempty"`
	RunState  string           `json:"runState"`
	Healthy   bool             `json:"healthy"`
	Detail    string           `json:"detail,omitempty"`
	AgentWork *utils.AgentWork `json:"agentWork,omitempty"`
}

type MissionSummary struct {
	Total      int `json:"total"`
	Up         int `json:"up"`
	Down       int `json:"down"`
	WithOpenPR int `json:"withOpenPR"`
}

// labelToNameKind splits a status row label ("services.api",
// "db_services.pg (postgres)") into bare name + kind.
func labelToNameKind(label string) (name, kind string) {
	kind = "service"
	rest := label
	if strings.HasPrefix(label, "db_services.") {
		kind = "db_service"
		rest = strings.TrimPrefix(label, "db_services.")
	} else if strings.HasPrefix(label, "services.") {
		rest = strings.TrimPrefix(label, "services.")
	}
	if i := strings.IndexByte(rest, ' '); i >= 0 {
		rest = rest[:i] // drop " (postgres)" suffix
	}
	return rest, kind
}

// buildMissionSnapshot probes run state via status.go's parallel prober, then
// attaches per-service agent work via the injected probe (nil for db_services
// and unresolvable repos). agentWorkFor returns nil to skip a service.
func buildMissionSnapshot(composePath string, rows []statusRow, agentWorkFor func(name string) *utils.AgentWork) MissionSnapshot {
	results := probeAllParallel(rows)
	snap := MissionSnapshot{ComposePath: composePath, GeneratedAt: time.Now().UTC()}
	for _, r := range rows {
		pr := results[r.Label]
		name, kind := labelToNameKind(r.Label)
		ms := MissionService{
			Name: name, Kind: kind, Port: r.Port,
			Healthy: pr.Healthy, Detail: pr.Detail,
			RunState: runStateFor(pr.Healthy),
		}
		if kind == "service" && agentWorkFor != nil {
			ms.AgentWork = agentWorkFor(name)
		}
		snap.Services = append(snap.Services, ms)
		snap.Summary.Total++
		if pr.Healthy {
			snap.Summary.Up++
		} else {
			snap.Summary.Down++
		}
		if ms.AgentWork != nil && ms.AgentWork.PR != nil && ms.AgentWork.PR.State == "open" {
			snap.Summary.WithOpenPR++
		}
	}
	return snap
}

func runStateFor(healthy bool) string {
	if healthy {
		return "running"
	}
	return "stopped"
}

// buildMissionFrame renders one terminal frame: a colored run-state line per
// service plus its branch/PR/CI, then a summary footer. interval is shown in
// the footer when watching (>0).
func buildMissionFrame(snap MissionSnapshot, interval time.Duration, now time.Time) string {
	var buf strings.Builder
	buf.WriteString("🛰️  corgi mission-control\n")
	for _, s := range snap.Services {
		icon, color := "❌", art.RedColor
		if s.Healthy {
			icon, color = "✅", art.GreenColor
		}
		fmt.Fprintf(&buf, "  %s %s %-28s %-10s%s", color, icon, s.Name, s.RunState, art.WhiteColor)
		if s.AgentWork != nil {
			fmt.Fprintf(&buf, "  %s%s%s", art.CyanColor, s.AgentWork.Branch, art.WhiteColor)
			if s.AgentWork.Dirty {
				buf.WriteString(" *")
			}
			if pr := s.AgentWork.PR; pr != nil {
				tag := pr.State
				if pr.Draft {
					tag = "draft"
				}
				fmt.Fprintf(&buf, "  PR #%d [%s] CI:%s", pr.Number, tag, pr.CI)
			}
		}
		buf.WriteByte('\n')
	}
	footer := ""
	if interval > 0 {
		footer = fmt.Sprintf(" every %s", interval)
	}
	fmt.Fprintf(&buf, "\n%s🛰️  %d services%s — %d up, %d down, %d open PRs — last update %s%s\n",
		art.CyanColor, snap.Summary.Total, footer, snap.Summary.Up, snap.Summary.Down,
		snap.Summary.WithOpenPR, now.Format("15:04:05"), art.WhiteColor)
	return buf.String()
}
