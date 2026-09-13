package cmd

import (
	"encoding/json"
	"net/http"
	"strings"

	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/workspace"
)

// The watch's switches, for a phone or a menu bar: what each workspace is
// told about and works on its own, and the two loops it closes by itself.
// GET lists them; POST flips some for one workspace. The two automation
// switches take on the daemon's next round; the rest need the daemon to
// read its config again (corgi agent restart), and the answer says so.

// WatchSwitches is one workspace's watch, in switches.
type WatchSwitches struct {
	Workspace string   `json:"workspace"`
	Enabled   bool     `json:"enabled"`
	Action    string   `json:"action"`
	Comments  bool     `json:"comments"`
	PRs       bool     `json:"prs"`
	Reviews   bool     `json:"reviews"`
	CI        bool     `json:"ci"`
	Labels    []string `json:"labels"`
	Isolate   bool     `json:"isolate"`
	Quiet     string   `json:"quiet"`
	DaysOff   []string `json:"daysOff"`
	AutoMerge bool     `json:"autoMerge"`
	HandOver  bool     `json:"handOver"`
}

func switchesOf(id string, wc *config.WatchConfig) WatchSwitches {
	out := WatchSwitches{Workspace: id, Action: "notify", Labels: []string{}, DaysOff: []string{}}
	if wc == nil {
		return out
	}
	out.Enabled, out.Comments, out.PRs, out.Reviews, out.CI, out.Isolate = wc.Enabled, wc.Comments, wc.PRs, wc.Reviews, wc.CI, wc.Isolate
	out.Quiet, out.AutoMerge, out.HandOver = wc.Quiet, wc.AutoMerge, wc.HandOver
	if wc.Action != "" {
		out.Action = wc.Action
	}
	if wc.Labels != nil {
		out.Labels = wc.Labels
	}
	if wc.DaysOff != nil {
		out.DaysOff = wc.DaysOff
	}
	return out
}

func launchWatchHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	path := agentUserConfigPath(dir)
	user, err := config.LoadUser(path)
	if err != nil || user == nil {
		user = &config.UserConfig{}
	}
	registry, err := workspace.Load(agentRegistryPath(dir))
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, "could not read the workspaces")
		return
	}
	switch r.Method {
	case http.MethodGet:
		list := []WatchSwitches{}
		for _, ws := range registry.Sorted() {
			repo, _ := config.LoadRepo(ws.AbsPath)
			list = append(list, switchesOf(ws.ID, config.Resolve(ws.ID, repo, user).Watch))
		}
		writeLaunchJSON(w, map[string]any{"workspaces": list})
	case http.MethodPost:
		var req struct {
			Workspace string    `json:"workspace"`
			Enabled   *bool     `json:"enabled"`
			Action    *string   `json:"action"`
			Comments  *bool     `json:"comments"`
			PRs       *bool     `json:"prs"`
			Reviews   *bool     `json:"reviews"`
			CI        *bool     `json:"ci"`
			Isolate   *bool     `json:"isolate"`
			Quiet     *string   `json:"quiet"`
			DaysOff   *[]string `json:"daysOff"`
			Labels    *[]string `json:"labels"`
			AutoMerge *bool     `json:"autoMerge"`
			HandOver  *bool     `json:"handOver"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil {
			writeLaunchError(w, http.StatusBadRequest, "could not read the request")
			return
		}
		ws, ok := registry.Find(strings.TrimSpace(req.Workspace))
		if !ok {
			writeLaunchError(w, http.StatusNotFound, "no workspace named "+req.Workspace)
			return
		}
		if user.Workspaces == nil {
			user.Workspaces = map[string]config.WorkspaceConfig{}
		}
		entry := user.Workspaces[ws.ID]
		wc := entry.Watch
		if wc == nil {
			wc = &config.WatchConfig{}
		}
		// Only the two loops take on the next round; anything else is read
		// when the daemon starts.
		restart := false
		if req.Enabled != nil {
			wc.Enabled, restart = *req.Enabled, true
		}
		if req.Action != nil {
			if *req.Action != "notify" && *req.Action != "fix" {
				writeLaunchError(w, http.StatusBadRequest, "action is notify or fix")
				return
			}
			wc.Action, restart = *req.Action, true
		}
		if req.Comments != nil {
			wc.Comments, restart = *req.Comments, true
		}
		if req.PRs != nil {
			wc.PRs, restart = *req.PRs, true
		}
		if req.Reviews != nil {
			wc.Reviews, restart = *req.Reviews, true
		}
		if req.CI != nil {
			wc.CI, restart = *req.CI, true
		}
		if req.Isolate != nil {
			wc.Isolate, restart = *req.Isolate, true
		}
		if req.Labels != nil {
			wc.Labels, restart = *req.Labels, true
		}
		if req.Quiet != nil {
			q := strings.TrimSpace(*req.Quiet)
			if q != "" {
				if _, err := daemon.ParseQuiet(q); err != nil {
					writeLaunchError(w, http.StatusBadRequest, err.Error())
					return
				}
			}
			wc.Quiet, restart = q, true
		}
		if req.DaysOff != nil {
			days, err := daemon.ParseDaysOff(*req.DaysOff)
			if err != nil {
				writeLaunchError(w, http.StatusBadRequest, err.Error())
				return
			}
			wc.DaysOff = nil
			for _, d := range days {
				wc.DaysOff = append(wc.DaysOff, strings.ToLower(d.String()[:3]))
			}
			restart = true
		}
		if req.AutoMerge != nil {
			wc.AutoMerge = *req.AutoMerge
		}
		if req.HandOver != nil {
			wc.HandOver = *req.HandOver
		}
		entry.Watch = wc
		user.Workspaces[ws.ID] = entry
		if err := writeUserConfig(path, user); err != nil {
			writeLaunchError(w, http.StatusInternalServerError, "could not write the config")
			return
		}
		writeLaunchJSON(w, map[string]any{"done": "saved", "watch": switchesOf(ws.ID, wc), "restart": restart})
	default:
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET the switches, POST {workspace, …} to flip them")
	}
}
