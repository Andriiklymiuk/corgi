package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/workspace"
)

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
	Slots     int      `json:"slots"`
	Quiet     string   `json:"quiet"`
	DaysOff   []string `json:"daysOff"`
	AutoMerge bool     `json:"autoMerge"`
	Approve   bool     `json:"approve"`
	HandOver  bool     `json:"handOver"`
	AutoAllow string   `json:"autoAllow"`
	DoneWhen  []string `json:"doneWhen"`
	CompactAt int      `json:"compactAt"`
	Rebase    bool     `json:"rebase"`
	Lessons   bool     `json:"lessons"`
	AutoCarry bool     `json:"autoCarry"`
	RerunCI   bool     `json:"rerunCI"`
	Silent    bool     `json:"silent"`
	Headless  bool     `json:"headless"`
}

func switchesOf(id string, wc *config.WatchConfig) WatchSwitches {
	out := WatchSwitches{Workspace: id, Action: "notify", Labels: []string{}, DaysOff: []string{}, DoneWhen: []string{}}
	if wc == nil {
		return out
	}
	out.Enabled, out.Comments, out.PRs, out.Reviews, out.CI, out.Isolate = wc.Enabled, wc.Comments, wc.PRs, wc.Reviews, wc.CI, wc.Isolate
	out.Quiet, out.AutoMerge, out.Approve, out.HandOver, out.AutoAllow = wc.Quiet, wc.AutoMerge, wc.Approve, wc.HandOver, wc.AutoAllow
	if wc.Action != "" {
		out.Action = wc.Action
	}
	if wc.Labels != nil {
		out.Labels = wc.Labels
	}
	if wc.DaysOff != nil {
		out.DaysOff = wc.DaysOff
	}
	if wc.DoneWhen != nil {
		out.DoneWhen = wc.DoneWhen
	}
	out.CompactAt, out.Rebase, out.Lessons, out.AutoCarry, out.RerunCI, out.Headless, out.Silent = wc.CompactAt, wc.Rebase, wc.Lessons, wc.AutoCarry, wc.RerunCI, wc.Headless, wc.Silent
	out.Slots = max(1, wc.Slots)
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
			Slots     *int      `json:"slots"`
			Quiet     *string   `json:"quiet"`
			DaysOff   *[]string `json:"daysOff"`
			Labels    *[]string `json:"labels"`
			AutoMerge *bool     `json:"autoMerge"`
			Approve   *bool     `json:"approve"`
			HandOver  *bool     `json:"handOver"`
			AutoAllow *string   `json:"autoAllow"`
			DoneWhen  *[]string `json:"doneWhen"`
			CompactAt *int      `json:"compactAt"`
			Rebase    *bool     `json:"rebase"`
			Lessons   *bool     `json:"lessons"`
			AutoCarry *bool     `json:"autoCarry"`
			RerunCI   *bool     `json:"rerunCI"`
			Silent    *bool     `json:"silent"`
			Headless  *bool     `json:"headless"`
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
		if req.Slots != nil {
			if *req.Slots < 1 || *req.Slots > 8 {
				writeLaunchError(w, http.StatusBadRequest, "slots is 1 to 8")
				return
			}
			wc.Slots, restart = *req.Slots, true
			if wc.Slots > 1 {
				wc.Isolate = true
			}
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
		if req.Approve != nil {
			wc.Approve = *req.Approve
		}
		if req.AutoMerge != nil {
			wc.AutoMerge = *req.AutoMerge
		}
		if req.HandOver != nil {
			wc.HandOver = *req.HandOver
		}
		if req.AutoAllow != nil {
			policy, err := config.ParseAutoAllow(*req.AutoAllow)
			if err != nil {
				writeLaunchError(w, http.StatusBadRequest, err.Error())
				return
			}
			wc.AutoAllow = policy
		}
		if req.Rebase != nil {
			wc.Rebase = *req.Rebase
		}
		if req.Lessons != nil {
			wc.Lessons = *req.Lessons
		}
		if req.AutoCarry != nil {
			wc.AutoCarry = *req.AutoCarry
		}
		if req.RerunCI != nil {
			wc.RerunCI, restart = *req.RerunCI, true
		}
		if req.Silent != nil {
			wc.Silent, restart = *req.Silent, true
		}
		if req.Headless != nil {
			wc.Headless = *req.Headless
		}
		if req.CompactAt != nil {
			if *req.CompactAt < 0 || *req.CompactAt > 100 {
				writeLaunchError(w, http.StatusBadRequest, "compactAt is a percent, 0 to 100")
				return
			}
			wc.CompactAt = *req.CompactAt
		}
		if req.DoneWhen != nil {
			wc.DoneWhen = nil
			for _, c := range *req.DoneWhen {
				if c = strings.TrimSpace(c); c != "" {
					wc.DoneWhen = append(wc.DoneWhen, c)
				}
			}
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

func setWorkspaceWatch(dir, id string, edit func(*config.WatchConfig)) error {
	registry, err := workspace.Load(agentRegistryPath(dir))
	if err != nil {
		return err
	}
	ws, ok := registry.Find(strings.TrimSpace(id))
	if !ok {
		return fmt.Errorf("no workspace named %s", id)
	}
	path := agentUserConfigPath(dir)
	user, err := config.LoadUser(path)
	if err != nil {
		return err
	}
	if user == nil {
		user = &config.UserConfig{}
	}
	if user.Workspaces == nil {
		user.Workspaces = map[string]config.WorkspaceConfig{}
	}
	entry := user.Workspaces[ws.ID]
	wc := entry.Watch
	if wc == nil {
		wc = &config.WatchConfig{}
	}
	edit(wc)
	entry.Watch = wc
	user.Workspaces[ws.ID] = entry
	return writeUserConfig(path, user)
}
