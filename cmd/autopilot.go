package cmd

import (
	"fmt"
	"os"
	"time"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

var autopilotCmd = &cobra.Command{
	Use:   "autopilot",
	Short: "Supervised loop state: status / pause / resume / stop / heartbeat",
	Long: `Durable state for the autopilot supervised loop (the autopilot skill drives
the loop; this command owns its kill switch + heartbeat). No daemon — state lives
in corgi_services/.autopilot.json and coordinates iterations across /loop or
/schedule runs. Draft PRs only; never merges.`,
}

// failAutopilot reports a config/IO error (JSON via JSONError, else a human line
// routed through utils.Info so --json stdout stays pure) and exits 1.
func failAutopilot(humanMsg string, err error) {
	if utils.JSONOutput {
		utils.JSONError(utils.ErrConfig, err.Error())
	} else {
		utils.Infof("%s: %s\n", humanMsg, err)
	}
	os.Exit(1)
}

// autopilotStateDir resolves the compose dir the same way sibling commands do
// (loads corgi-compose.yml, which sets utils.CorgiComposePathDir). On a resolve
// failure it emits the shared E_CONFIG error and exits 1.
func autopilotStateDir(cmd *cobra.Command) string {
	if _, err := utils.GetCorgiServices(cmd); err != nil {
		failAutopilot("couldn't get services config", err)
	}
	return utils.CorgiComposePathDir
}

// loadAutopilotStatus reads state for the compose dir; absent file → an
// uninitialized state (a genuine first run, distinct from an explicit stop), so
// the loop can start instead of mistaking it for the kill switch. Never errors
// on absence — status must always answer.
func loadAutopilotStatus(composeDir string) (utils.AutopilotState, error) {
	path := utils.AutopilotStatePath(composeDir)
	st, err := utils.ReadAutopilotState(path)
	if err != nil {
		if os.IsNotExist(err) {
			return utils.AutopilotState{Mode: utils.AutopilotUninitialized}, nil
		}
		return st, err
	}
	return st, nil
}

var autopilotStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the autopilot loop mode, heartbeat age, and last iteration",
	Run: func(cmd *cobra.Command, _ []string) {
		dir := autopilotStateDir(cmd)
		st, err := loadAutopilotStatus(dir)
		if err != nil {
			if utils.JSONOutput {
				utils.JSONError(utils.ErrConfig, err.Error())
			} else {
				utils.Infof("couldn't read autopilot state: %s\n", err)
			}
			os.Exit(1)
		}
		if utils.JSONOutput {
			utils.PrintJSON(st)
			return
		}
		printAutopilotStatus(st)
	},
}

// printAutopilotStatus renders the human view: mode, heartbeat age, and the last
// iteration summary. Routed via utils.Info so --json stdout stays pure JSON.
func printAutopilotStatus(st utils.AutopilotState) {
	utils.Infof("autopilot: %s\n", st.Mode)
	if st.LastHeartbeat.IsZero() {
		utils.Info("  heartbeat: none yet")
	} else {
		age := time.Since(st.LastHeartbeat).Round(time.Second)
		utils.Infof("  heartbeat: %s ago (iter %d)\n", age, st.Iteration)
	}
	sum := st.LastSummary
	if sum.Phase != "" {
		utils.Infof("  last: %s · built %d · skipped %d · awaiting %d · %s\n",
			sum.Phase, sum.Built, sum.Skipped, sum.Awaiting, sum.Note)
	}
}

func newAutopilotModeCmd(use, short string, mode utils.AutopilotMode) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Run: func(cmd *cobra.Command, _ []string) {
			dir := autopilotStateDir(cmd)
			path := utils.AutopilotStatePath(dir)
			st, err := utils.SetAutopilotMode(path, mode)
			if err != nil {
				if utils.JSONOutput {
					utils.JSONError(utils.ErrConfig, err.Error())
				} else {
					utils.Infof("couldn't write autopilot state: %s\n", err)
				}
				os.Exit(1)
			}
			if utils.JSONOutput {
				utils.PrintJSON(st)
				return
			}
			utils.Infof("autopilot: %s\n", st.Mode)
		},
	}
}

var (
	autopilotPauseCmd  = newAutopilotModeCmd("pause", "Pause the loop; it no-ops at the next iteration boundary", utils.AutopilotPaused)
	autopilotResumeCmd = newAutopilotModeCmd("resume", "Resume (or initialize) the loop in running mode", utils.AutopilotRunning)
	autopilotStopCmd   = newAutopilotModeCmd("stop", "Kill switch: the next iteration sees stopped and no-ops", utils.AutopilotStopped)
)

var autopilotHeartbeatCmd = &cobra.Command{
	Use:   "heartbeat",
	Short: "Record a heartbeat + iteration summary (called by the loop each iteration)",
	Run: func(cmd *cobra.Command, _ []string) {
		dir := autopilotStateDir(cmd)
		path := utils.AutopilotStatePath(dir)

		phase, _ := cmd.Flags().GetString("phase")
		built, _ := cmd.Flags().GetInt("built")
		skipped, _ := cmd.Flags().GetInt("skipped")
		awaiting, _ := cmd.Flags().GetInt("awaiting")
		note, _ := cmd.Flags().GetString("note")

		it := utils.AutopilotIteration{
			Phase:    phase,
			Built:    built,
			Skipped:  skipped,
			Awaiting: awaiting,
			Note:     note,
		}
		st, err := utils.RecordAutopilotHeartbeat(path, it)
		if err != nil {
			if utils.JSONOutput {
				utils.JSONError(utils.ErrConfig, err.Error())
			} else {
				utils.Infof("couldn't record heartbeat: %s\n", err)
			}
			os.Exit(1)
		}
		if utils.JSONOutput {
			utils.PrintJSON(st)
			return
		}
		utils.Info(fmt.Sprintf("autopilot · iter %d · built %d · skipped %d · awaiting %d · %s",
			st.Iteration, it.Built, it.Skipped, it.Awaiting, it.Note))
	},
}

func init() {
	autopilotHeartbeatCmd.Flags().String("phase", "", "Iteration phase: built | idle | awaiting_spec_signoff | error")
	autopilotHeartbeatCmd.Flags().Int("built", 0, "Tickets built into draft PRs this iteration")
	autopilotHeartbeatCmd.Flags().Int("skipped", 0, "Tickets drift-skipped this iteration")
	autopilotHeartbeatCmd.Flags().Int("awaiting", 0, "Tickets staged and awaiting the spec gate")
	autopilotHeartbeatCmd.Flags().String("note", "", "Short human note for this iteration")

	// Local --json mirrors status/mission-control; PersistentPreRun reads it into utils.JSONOutput.
	autopilotCmd.PersistentFlags().Bool("json", false, "Emit the autopilot state object as JSON")

	autopilotCmd.AddCommand(autopilotStatusCmd, autopilotPauseCmd, autopilotResumeCmd, autopilotStopCmd, autopilotHeartbeatCmd)
	rootCmd.AddCommand(autopilotCmd)
}
