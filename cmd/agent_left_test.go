package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestThePhoneListsWhatIsLeftAndTicksItOff(t *testing.T) {
	dir := phoneBoard(t, true)
	log := watch.LoadFixLog(dir)
	now := time.Now()
	log.StartFor(watch.Event{Key: "k1", Workspace: "acme", Ref: "ABC-1", Kind: watch.KindIssueNew}, now)
	log.Finish("k1", nil, "", "", now)
	log.SetLeft("k1", watch.LeftFor("## Left for you\n- Run the migration on staging.\n"))

	rec := httptest.NewRecorder()
	launchLeftHandler(rec, httptest.NewRequest(http.MethodGet, "/launch/left", nil))
	var got struct {
		Left []watch.LeftRow `json:"left"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if rec.Code != 200 || len(got.Left) != 1 || got.Left[0].ID != "k1#0" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := post(launchLeftHandler, "/launch/left", `{"id":"nope#3"}`); rec.Code != 404 {
		t.Fatalf("an item that does not exist: %d %s", rec.Code, rec.Body)
	}
	if rec := post(launchLeftHandler, "/launch/left", `{"id":"k1#0"}`); rec.Code != 200 {
		t.Fatalf("ticked: %d %s", rec.Code, rec.Body)
	}
	if open := leftRows(dir, false, time.Now()); len(open) != 0 {
		t.Fatalf("nothing open after the tick: %+v", open)
	}
}

func TestThePhoneMergesOnlyAPullRequestOfMineThatCorgiWatches(t *testing.T) {
	dir := phoneBoard(t, true)
	_ = watch.LoadPullLog(dir).Set("acme/api#2", watch.PullStatus{State: "open", Mine: false, Link: "https://github.com/acme/api/pull/2", Workspace: "acme"})
	for _, body := range []string{
		`{"link":"https://github.com/someone/else/pull/1","do":"merge"}`,
		`{"link":"https://github.com/acme/api/pull/2","do":"merge"}`,
	} {
		if rec := post(launchPullHandler, "/launch/pull", body); rec.Code != 400 || !strings.Contains(rec.Body.String(), "not a pull request of yours") {
			t.Fatalf("%s: %d %s", body, rec.Code, rec.Body)
		}
	}
}
