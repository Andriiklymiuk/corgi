package daemon

import (
	"context"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestChecksGoingGreenAfterAFixCreditTheRun(t *testing.T) {
	d := testDaemon(t)
	spec := ticketSpec(t)
	link := "https://gitlab.com/acme/core/-/merge_requests/9096"
	fixes := watch.LoadFixLog(d.Dir)
	fixes.StartFor(watch.Event{Key: "k1", Workspace: spec.Workspace, Ref: "acme/core!9096", Kind: watch.KindCIFailed, URL: link}, time.Now().Add(-time.Hour))
	fixes.Finish("k1", nil, "pushed", "", time.Now().Add(-50*time.Minute))

	was := watch.PullStatus{State: "open", Checks: "failing"}
	now := watch.PullStatus{State: "open", Checks: "passing", At: time.Now()}
	d.pullChanged(context.Background(), spec, "acme/core!9096", link, was, now, true)

	if watch.LoadFixLog(d.Dir).Records()[0].GreenAt.IsZero() {
		t.Error("the run that fixed the pipeline is credited when the checks pass")
	}
}
