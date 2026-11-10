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
	Workspace  string   `json:"workspace"`
	Enabled    bool     `json:"enabled"`
	Action     string   `json:"action"`
	Comments   bool     `json:"comments"`
	PRs        bool     `json:"prs"`
	Reviews    bool     `json:"reviews"`
	CI         bool     `json:"ci"`
	Labels     []string `json:"labels"`
	Isolate    bool     `json:"isolate"`
	Slots      int      `json:"slots"`
	Quiet      string   `json:"quiet"`
	DaysOff    []string `json:"daysOff"`
	AutoMerge  bool     `json:"autoMerge"`
	Approve    bool     `json:"approve"`
	HandOver   bool     `json:"handOver"`
	AutoAllow  string   `json:"autoAllow"`
	DoneWhen   []string `json:"doneWhen"`
	CompactAt  int      `json:"compactAt"`
	Rebase     bool     `json:"rebase"`
	Lessons    bool     `json:"lessons"`
	PlanReview string   `json:"planReview"`
	AutoCarry  bool     `json:"autoCarry"`
	RerunCI    bool     `json:"rerunCI"`
	Silent     bool     `json:"silent"`
	Headless   bool     `json:"headless"`
	NightShift bool     `json:"nightShift"`
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
	out.NightShift = wc.NightShift
	out.PlanReview = wc.PlanReview
	out.Slots = max(1, wc.Slots)
	return out
}

type watchSwitchRequest struct {
	Workspace  string    `json:"workspace"`
	Enabled    *bool     `json:"enabled"`
	Action     *string   `json:"action"`
	Comments   *bool     `json:"comments"`
	PRs        *bool     `json:"prs"`
	Reviews    *bool     `json:"reviews"`
	CI         *bool     `json:"ci"`
	Isolate    *bool     `json:"isolate"`
	Slots      *int      `json:"slots"`
	Quiet      *string   `json:"quiet"`
	DaysOff    *[]string `json:"daysOff"`
	Labels     *[]string `json:"labels"`
	AutoMerge  *bool     `json:"autoMerge"`
	Approve    *bool     `json:"approve"`
	HandOver   *bool     `json:"handOver"`
	AutoAllow  *string   `json:"autoAllow"`
	DoneWhen   *[]string `json:"doneWhen"`
	CompactAt  *int      `json:"compactAt"`
	Rebase     *bool     `json:"rebase"`
	Lessons    *bool     `json:"lessons"`
	AutoCarry  *bool     `json:"autoCarry"`
	RerunCI    *bool     `json:"rerunCI"`
	Silent     *bool     `json:"silent"`
	Headless   *bool     `json:"headless"`
	NightShift *bool     `json:"nightShift"`
	PlanReview *string   `json:"planReview"`
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
		flipWatchSwitches(w, r, path, user, registry)
	default:
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET the switches, POST {workspace, …} to flip them")
	}
}

func flipWatchSwitches(w http.ResponseWriter, r *http.Request, path string, user *config.UserConfig, registry *workspace.Registry) {
	var req watchSwitchRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil {
		writeLaunchError(w, http.StatusBadRequest, requestUnreadableMessage)
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
	restart, err := applyWatchRequest(wc, req)
	if err != nil {
		writeLaunchError(w, http.StatusBadRequest, err.Error())
		return
	}
	entry.Watch = wc
	user.Workspaces[ws.ID] = entry
	if err := writeUserConfig(path, user); err != nil {
		writeLaunchError(w, http.StatusInternalServerError, "could not write the config")
		return
	}
	writeLaunchJSON(w, map[string]any{"done": "saved", "watch": switchesOf(ws.ID, wc), "restart": restart})
}

func applyWatchRequest(wc *config.WatchConfig, req watchSwitchRequest) (bool, error) {
	restart := applyWatchToggles(wc, req)
	for _, step := range []func(*config.WatchConfig, watchSwitchRequest) (bool, error){
		applyWatchAction, applyWatchSlots, applyWatchCalendar, applyWatchPolicyRequest,
	} {
		again, err := step(wc, req)
		if err != nil {
			return false, err
		}
		restart = restart || again
	}
	if req.Labels != nil {
		wc.Labels, restart = *req.Labels, true
	}
	if req.DoneWhen != nil {
		wc.DoneWhen = nil
		for _, c := range *req.DoneWhen {
			if c = strings.TrimSpace(c); c != "" {
				wc.DoneWhen = append(wc.DoneWhen, c)
			}
		}
	}
	return restart, nil
}

func applyWatchToggles(wc *config.WatchConfig, req watchSwitchRequest) bool {
	restart := false
	for _, t := range []struct {
		in      *bool
		dst     *bool
		restart bool
	}{
		{req.Enabled, &wc.Enabled, true}, {req.Comments, &wc.Comments, true}, {req.PRs, &wc.PRs, true},
		{req.Reviews, &wc.Reviews, true}, {req.CI, &wc.CI, true}, {req.Isolate, &wc.Isolate, true},
		{req.RerunCI, &wc.RerunCI, true}, {req.Silent, &wc.Silent, true},
		{req.Approve, &wc.Approve, false}, {req.AutoMerge, &wc.AutoMerge, false}, {req.HandOver, &wc.HandOver, false},
		{req.Rebase, &wc.Rebase, false}, {req.Lessons, &wc.Lessons, false}, {req.AutoCarry, &wc.AutoCarry, false},
		{req.NightShift, &wc.NightShift, false}, {req.Headless, &wc.Headless, false},
	} {
		if t.in != nil {
			*t.dst = *t.in
			restart = restart || t.restart
		}
	}
	return restart
}

func applyWatchAction(wc *config.WatchConfig, req watchSwitchRequest) (bool, error) {
	if req.Action == nil {
		return false, nil
	}
	if *req.Action != "notify" && *req.Action != "fix" {
		return false, fmt.Errorf("action is notify or fix")
	}
	wc.Action = *req.Action
	return true, nil
}

func applyWatchSlots(wc *config.WatchConfig, req watchSwitchRequest) (bool, error) {
	if req.Slots == nil {
		return false, nil
	}
	if *req.Slots < 1 || *req.Slots > 8 {
		return false, fmt.Errorf("slots is 1 to 8")
	}
	wc.Slots = *req.Slots
	if wc.Slots > 1 {
		wc.Isolate = true
	}
	return true, nil
}

func applyWatchCalendar(wc *config.WatchConfig, req watchSwitchRequest) (bool, error) {
	restart := false
	if req.Quiet != nil {
		q := strings.TrimSpace(*req.Quiet)
		if q != "" {
			if _, err := daemon.ParseQuiet(q); err != nil {
				return false, err
			}
		}
		wc.Quiet, restart = q, true
	}
	if req.DaysOff != nil {
		days, err := daemon.ParseDaysOff(*req.DaysOff)
		if err != nil {
			return false, err
		}
		wc.DaysOff = nil
		for _, d := range days {
			wc.DaysOff = append(wc.DaysOff, strings.ToLower(d.String()[:3]))
		}
		restart = true
	}
	return restart, nil
}

func applyWatchPolicyRequest(wc *config.WatchConfig, req watchSwitchRequest) (bool, error) {
	if req.AutoAllow != nil {
		policy, err := config.ParseAutoAllow(*req.AutoAllow)
		if err != nil {
			return false, err
		}
		wc.AutoAllow = policy
	}
	if req.PlanReview != nil {
		policy, err := config.ParsePlanReview(*req.PlanReview)
		if err != nil {
			return false, err
		}
		wc.PlanReview = policy
	}
	if req.CompactAt != nil {
		if *req.CompactAt < 0 || *req.CompactAt > 100 {
			return false, fmt.Errorf("compactAt is a percent, 0 to 100")
		}
		wc.CompactAt = *req.CompactAt
	}
	return false, nil
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
