package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/daemon"

	"github.com/spf13/cobra"
)

const defaultLidHours = "08:00-23:00"

var agentLidCmd = &cobra.Command{
	Use:   "lid [on|off|lock|shutdown]",
	Short: "Ring the phone when this laptop wakes or its lid opens by day; lock or shut it down from there",
	Long: `A laptop closed and left in a room says when someone opens it. The daemon
notices the wake (or the lid) and rings the phone - by day only, inside
--hours, since at night the one opening it is you. When you open it
yourself, that is one push to glance at. When it was not you, the push
carries two buttons: Lock (sleep; the password is asked at the wake, past
any caffeinate) and Shut down (as the Apple menu does). Neither needs root.

  corgi agent lid on                     ring between 08:00 and 23:00
  corgi agent lid on --hours 09:00-22:00
  corgi agent lid off
  corgi agent lid                        what is set now
  corgi agent lid lock | shutdown        do it from here

One ring per opening, never twice in ten minutes. Off by default; the
daemon reads the setting at start (corgi agent restart).`,
	Args: cobra.MaximumNArgs(1),
	Run:  runAgentLid,
}

var lidLine = regexp.MustCompile(`(?m)^lid:.*$`)

func runAgentLid(cmd *cobra.Command, args []string) {
	dir, err := agentDir()
	if err != nil {
		exitWithError("agent_data_dir", err, 1)
	}
	path := agentUserConfigPath(dir)
	if len(args) == 0 {
		user, _ := config.LoadUser(path)
		if user == nil || strings.TrimSpace(user.Lid) == "" {
			utils.Info("lid: off - corgi agent lid on")
			return
		}
		utils.Info("lid: on between " + user.Lid)
		return
	}
	switch args[0] {
	case "on":
		hours, _ := cmd.Flags().GetString("hours")
		if hours == "" {
			hours = defaultLidHours
		}
		if _, err := daemon.ParseQuiet(hours); err != nil {
			exitWithError(utils.ErrUsage, err, 2)
		}
		if err := writeUserConfigLine(path, lidLine, "lid: "+hours); err != nil {
			exitWithError("agent_lid", err, 1)
		}
		utils.Infof("✓ lid: %s in %s - a wake or an open lid in that window rings the phone; corgi agent restart\n", hours, path)
	case "off":
		if err := writeUserConfigLine(path, lidLine, "lid: \"\""); err != nil {
			exitWithError("agent_lid", err, 1)
		}
		utils.Infof("✓ lid off in %s; corgi agent restart\n", path)
	case "lock", "shutdown":
		said, err := daemon.LidAct(args[0])
		if err != nil {
			exitWithError("agent_lid", err, 1)
		}
		utils.Info(said)
	default:
		exitWithError(utils.ErrUsage, fmt.Errorf("lid takes on, off, lock or shutdown, not %q", args[0]), 2)
	}
}

func launchLidHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	switch r.Method {
	case http.MethodGet:
		user, _ := config.LoadUser(agentUserConfigPath(dir))
		hours := ""
		if user != nil {
			hours = strings.TrimSpace(user.Lid)
		}
		writeLaunchJSON(w, map[string]any{"on": hours != "", "hours": hours})
	case http.MethodPost:
		var req struct {
			Action string `json:"action"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&req); err != nil {
			writeLaunchError(w, http.StatusBadRequest, "could not read the request")
			return
		}
		said, err := daemon.LidAct(strings.TrimSpace(req.Action))
		if err != nil {
			writeLaunchError(w, http.StatusBadRequest, err.Error())
			return
		}
		utils.Infof("agent: lid: %s (from the phone at %s)\n", said, time.Now().Format("15:04"))
		writeLaunchJSON(w, map[string]any{"done": said})
	default:
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET or POST")
	}
}

func init() {
	agentLidCmd.Flags().String("hours", "", "The local HH:MM-HH:MM window in which an opening rings (default "+defaultLidHours+")")
	agentCmd.AddCommand(agentLidCmd)
}
