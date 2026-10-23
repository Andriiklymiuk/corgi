package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/watch"
	"github.com/spf13/cobra"
)

func leftRows(dir string, all bool, now time.Time) []watch.LeftRow {
	since := now.Add(-24 * time.Hour)
	if all {
		since = now.AddDate(0, 0, -14)
	}
	rows := watch.LeftRows(watch.LoadFixLog(dir).Records(), watch.LoadLeftTicks(dir).All(), since, now)
	if all {
		return rows
	}
	return watch.OpenLeft(rows)
}

func tickLeft(dir, id, by string) error {
	id = strings.TrimSpace(id)
	for _, r := range watch.LeftRows(watch.LoadFixLog(dir).Records(), nil, time.Time{}, time.Now()) {
		if r.ID == id {
			if err := watch.LoadLeftTicks(dir).Tick(id, by, time.Now()); err != nil {
				return err
			}
			if info, err := daemon.ReadInfo(dir); err == nil && info != nil {
				daemon.Nudge(info)
			}
			return nil
		}
	}
	return fmt.Errorf("no item %q left for you - corgi agent left lists them", id)
}

var agentLeftCmd = &cobra.Command{
	Use:   "left [--done <id>] [--all]",
	Short: "What unattended runs left for a person, ticked off as CI goes green, pull requests merge or you say so",
	Long: `Each unattended run ends with what is left for a person. corgi keeps those
items and ticks them off itself when it sees them happen: the checks of the
pull request went green, the pull request merged. The rest wait for you.

  corgi agent left
  corgi agent left --done <id>     # you did it
  corgi agent left --all --json    # with what closed in the last fortnight`,
	Run: func(cmd *cobra.Command, _ []string) {
		dir := mustAgentDir()
		if id, _ := cmd.Flags().GetString("done"); id != "" {
			if err := tickLeft(dir, id, "you"); err != nil {
				exitWithError("agent_left", err, 1)
			}
			fmt.Println("done:", id)
			return
		}
		all, _ := cmd.Flags().GetBool("all")
		rows := leftRows(dir, all, time.Now())
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"left": rows})
			return
		}
		if len(rows) == 0 {
			fmt.Println("nothing left for you")
			return
		}
		for _, r := range rows {
			mark := "◻"
			if !r.DoneAt.IsZero() {
				mark = "✓"
			}
			fmt.Printf("%s %s  %s · %s\n", mark, r.Text, r.Workspace, firstNonEmptyString(r.Ref, r.Title))
			tail := "   " + r.ID
			if r.DoneBy != "" {
				tail += " · " + r.DoneBy
			}
			if r.URL != "" {
				tail += " · " + r.URL
			}
			fmt.Println(tail)
		}
	},
}

func launchLeftHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeLaunchJSON(w, map[string]any{"left": leftRows(dir, r.URL.Query().Get("all") == "1", time.Now())})
	case http.MethodPost:
		var req struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&req); err != nil || strings.TrimSpace(req.ID) == "" {
			writeLaunchError(w, http.StatusBadRequest, "POST {id} of an item left for you")
			return
		}
		if err := tickLeft(dir, req.ID, "phone"); err != nil {
			writeLaunchError(w, http.StatusNotFound, err.Error())
			return
		}
		writeLaunchJSON(w, map[string]any{"done": req.ID})
	default:
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET what is left for you, POST {id} to tick one off")
	}
}

func launchPullHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "POST {link, do: merge|close}")
		return
	}
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var req struct {
		Link string `json:"link"`
		Do   string `json:"do"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<10)).Decode(&req); err != nil {
		writeLaunchError(w, http.StatusBadRequest, "could not read the request")
		return
	}
	pull, ok := watch.LoadPullLog(dir).Get(req.Link)
	if !ok || !pull.Mine || pull.Workspace == "" || pull.Link == "" {
		writeLaunchError(w, http.StatusBadRequest, "not a pull request of yours that corgi watches")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	secrets := watch.LoadSecretsFor(dir, pull.Workspace)
	switch strings.TrimSpace(req.Do) {
	case "merge":
		err = watch.MergePR(ctx, secrets, pull.Link)
	case "close":
		err = watch.ClosePR(ctx, secrets, pull.Link)
	default:
		writeLaunchError(w, http.StatusBadRequest, "do is merge or close")
		return
	}
	if err != nil {
		writeLaunchError(w, http.StatusBadGateway, firstLineOf(err.Error()))
		return
	}
	if info, err := daemon.ReadInfo(dir); err == nil && info != nil {
		daemon.Nudge(info)
	}
	writeLaunchJSON(w, map[string]any{"done": req.Do + " " + pull.Link, "url": pull.Link})
}

func init() {
	agentLeftCmd.Flags().String("done", "", "Tick this item off")
	agentLeftCmd.Flags().Bool("all", false, "Also what closed in the last fortnight")
	agentCmd.AddCommand(agentLeftCmd)
}
