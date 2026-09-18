package cmd

import (
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestWatchStatusCarriesPlanReview(t *testing.T) {
	specs := []daemon.WatchSpec{{Workspace: "api", PlanReview: "risk>=7", Interval: time.Minute}, {Workspace: "web", Interval: time.Minute}}
	rows := watchStatusWorkspaces(specs, watch.LoadState(t.TempDir()), time.Now())
	if len(rows) != 2 || rows[0].PlanReview != "risk>=7" || rows[1].PlanReview != "" {
		t.Fatalf("%+v", rows)
	}
}
