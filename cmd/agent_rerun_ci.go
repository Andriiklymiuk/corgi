package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils/agent/watch"
)

func rerunRedBuildOn(ctx context.Context, dir string, secrets watch.Secrets, ref string, since time.Time) (watch.Rerun, error) {
	forge := "github"
	find, retry := watch.NewestFailedRun, watch.RerunFailedJobs
	if strings.Contains(ref, "!") {
		forge, find, retry = "gitlab", watch.NewestFailedPipeline, watch.RetryPipeline
	}
	run, err := find(ctx, secrets, ref, since)
	if err != nil {
		return watch.Rerun{}, err
	}
	reruns := watch.LoadReruns(dir)
	if reruns.SeenOn(forge, run.ID) {
		return watch.Rerun{}, fmt.Errorf("%s (%s) was rerun already", run.Name, ref)
	}
	if err := retry(ctx, secrets, ref, run.ID); err != nil {
		return watch.Rerun{}, err
	}
	r := watch.Rerun{RunID: run.ID, Forge: forge, URL: run.URL, At: time.Now()}
	_ = reruns.Set(r)
	return r, nil
}
