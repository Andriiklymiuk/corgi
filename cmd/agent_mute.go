package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/daemon"
)

var agentMuteCmd = &cobra.Command{
	Use:   "mute [1h|30m|off]",
	Short: "Nothing rings for a while - no toast, no push; the board goes on",
	Long: `Holds every notification - the desktop toast, the phone push, the permission
ping - until the time passes; the inbox, the board and the rows are as they
were. Without an argument: an hour. off ends it early.

  corgi agent mute           an hour
  corgi agent mute 30m
  corgi agent mute off
  corgi agent mute --json    what the mute is now`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		if len(args) == 0 && utils.JSONOutput {
			until := daemon.MutedUntil(dir)
			utils.PrintJSON(map[string]any{"muted": !until.IsZero(), "until": until})
			return
		}
		word := "1h"
		if len(args) == 1 {
			word = strings.ToLower(strings.TrimSpace(args[0]))
		}
		var until time.Time
		if word != "off" && word != "0" {
			d, err := time.ParseDuration(word)
			if err != nil || d <= 0 || d > 24*time.Hour {
				exitWithError("agent_mute", fmt.Errorf("mute takes a duration up to 24h (1h, 30m) or off, not %q", word), 2)
			}
			until = time.Now().Add(d)
		}
		if err := daemon.SetMute(dir, until); err != nil {
			exitWithError("agent_mute", err, 1)
		}
		if info, err := daemon.ReadInfo(dir); err == nil && info != nil {
			daemon.Nudge(info)
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"muted": !until.IsZero(), "until": until})
			return
		}
		if until.IsZero() {
			fmt.Println("Ringing again.")
			return
		}
		fmt.Printf("Muted until %s - nothing rings; the board goes on.\n", until.Local().Format("15:04"))
	},
}

func init() {
	agentCmd.AddCommand(agentMuteCmd)
}

func launchMuteHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	switch r.Method {
	case http.MethodGet:
		until := daemon.MutedUntil(dir)
		writeLaunchJSON(w, map[string]any{"muted": !until.IsZero(), "until": until})
	case http.MethodPost:
		var req struct {
			Minutes int `json:"minutes"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&req); err != nil {
			writeLaunchError(w, http.StatusBadRequest, "could not read the request")
			return
		}
		if req.Minutes < 0 || req.Minutes > 24*60 {
			writeLaunchError(w, http.StatusBadRequest, "minutes is 0 to 1440")
			return
		}
		var until time.Time
		if req.Minutes > 0 {
			until = time.Now().Add(time.Duration(req.Minutes) * time.Minute)
		}
		if err := daemon.SetMute(dir, until); err != nil {
			writeLaunchError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if info, err := daemon.ReadInfo(dir); err == nil && info != nil {
			daemon.Nudge(info)
		}
		writeLaunchJSON(w, map[string]any{"muted": !until.IsZero(), "until": until})
	default:
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET the mute, POST {minutes} to set it")
	}
}
