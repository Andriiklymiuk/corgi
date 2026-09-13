package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/workspace"
)

// A phone reads a workspace's watch as switches and flips them; the two
// loops the daemon closes by itself take on the next round, the rest say
// a restart is needed.
func TestWatchSwitchesFromThePhone(t *testing.T) {
	dir := phoneBoard(t, true)
	reg := &workspace.Registry{}
	reg.Upsert(workspace.Workspace{ID: "api", AbsPath: t.TempDir(), Status: workspace.StatusOK})
	if err := workspace.Save(agentRegistryPath(dir), reg); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	launchWatchHandler(rec, httptest.NewRequest(http.MethodGet, "/launch/watch", nil))
	var got struct {
		Workspaces []WatchSwitches `json:"workspaces"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if rec.Code != 200 || len(got.Workspaces) != 1 || got.Workspaces[0].Enabled || got.Workspaces[0].Action != "notify" {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}

	rec = post(launchWatchHandler, "/launch/watch", `{"workspace":"api","autoMerge":true,"handOver":true}`)
	var saved struct {
		Watch   WatchSwitches `json:"watch"`
		Restart bool          `json:"restart"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &saved)
	if rec.Code != 200 || !saved.Watch.AutoMerge || !saved.Watch.HandOver || saved.Restart {
		t.Fatalf("the loops take at once: %d %s", rec.Code, rec.Body)
	}
	rec = post(launchWatchHandler, "/launch/watch", `{"workspace":"api","enabled":true,"prs":true,"quiet":"23:00-07:00"}`)
	_ = json.Unmarshal(rec.Body.Bytes(), &saved)
	if rec.Code != 200 || !saved.Watch.Enabled || !saved.Watch.PRs || saved.Watch.Quiet != "23:00-07:00" || !saved.Restart {
		t.Fatalf("the rest needs a restart: %d %s", rec.Code, rec.Body)
	}
	user, err := config.LoadUser(agentUserConfigPath(dir))
	if err != nil || user.Workspaces["api"].Watch == nil || !user.Workspaces["api"].Watch.AutoMerge || !user.Workspaces["api"].Watch.PRs {
		t.Fatalf("written to the config: %+v %v", user, err)
	}
	if rec := post(launchWatchHandler, "/launch/watch", `{"workspace":"api","quiet":"25:00-07:00"}`); rec.Code != 400 {
		t.Fatalf("bad quiet: %d", rec.Code)
	}
	if rec := post(launchWatchHandler, "/launch/watch", `{"workspace":"nope","prs":true}`); rec.Code != 404 {
		t.Fatalf("unknown workspace: %d", rec.Code)
	}
}
