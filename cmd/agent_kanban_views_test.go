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

func TestUnreviewedWordCountsOnlyOpenPullRequestsNobodyReviewed(t *testing.T) {
	now := time.Now()
	pull := func(state, review string, age time.Duration) KanbanCard {
		return KanbanCard{Column: ColReview, Pull: &watch.PullStatus{State: state, Review: review, Since: now.Add(-age)}}
	}
	for name, tc := range map[string]struct {
		card KanbanCard
		want string
	}{
		"fresh":    {pull("open", "none", time.Hour), ""},
		"a day":    {pull("open", "pending", 30*time.Hour), "no review for a day"},
		"days":     {pull("open", "none", 80*time.Hour), "no review for 3 days"},
		"approved": {pull("open", "approved", 80*time.Hour), ""},
		"changes":  {pull("open", "changes", 80*time.Hour), ""},
		"draft":    {pull("draft", "none", 80*time.Hour), ""},
		"merged":   {pull("merged", "none", 80*time.Hour), ""},
		"no pull":  {KanbanCard{Column: ColReview}, ""},
	} {
		if got := unreviewedWord(tc.card, now); got != tc.want {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
	}
}

func TestCardFilterKeepsWhatTheViewAsksFor(t *testing.T) {
	dir := phoneBoard(t, false)
	now := time.Now()
	cards := []KanbanCard{
		{Ref: "A", Column: ColInbox, Triage: true, UpdatedAt: now},
		{Ref: "B", Column: ColReview, Unreviewed: "no review for a day", UpdatedAt: now, Pull: &watch.PullStatus{State: "open", Checks: "failing"}},
		{Ref: "C", Column: ColInbox, Owner: "anna", Priority: "urgent", UpdatedAt: now.Add(-5 * 24 * time.Hour), Links: []watch.TaskLink{{URL: "https://linear.app/x/issue/HUM-1", Source: "linear"}}},
		{Ref: "D", Column: ColDone, Overdue: false, UpdatedAt: now.Add(-9 * 24 * time.Hour)},
		{Ref: "E", Column: ColBlocked, Overdue: true, UpdatedAt: now},
	}
	refs := func(f cardFilter) string {
		if err := f.check(); err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, c := range f.apply(dir, cards, now) {
			out = append(out, c.Ref)
		}
		return strings.Join(out, "")
	}
	for want, f := range map[string]cardFilter{
		"ABE":   {View: "needs-me"},
		"B":     {View: "unreviewed"},
		"B ":    {View: "red"},
		"C":     {View: "stale"},
		"E":     {View: "overdue"},
		"E ":    {View: "blocked"},
		"A":     {View: "triage"},
		"C ":    {Owner: "Anna"},
		"C  ":   {Priority: "urgent", Source: "linear"},
		"ABCDE": {},
	} {
		if got := refs(f); got != strings.TrimSpace(want) {
			t.Errorf("%+v kept %q, want %q", f, got, strings.TrimSpace(want))
		}
	}
	if err := (cardFilter{View: "nope"}).check(); err == nil {
		t.Fatal("an unknown view passed")
	}
	if err := (cardFilter{Priority: "asap"}).check(); err == nil {
		t.Fatal("an unknown priority passed")
	}
}

func TestSavedViewIsResolvedByName(t *testing.T) {
	dir := phoneBoard(t, false)
	if err := writeSavedViews(dir, map[string]cardFilter{"anna-urgent": {View: "stale", Owner: "anna", Priority: "urgent"}}); err != nil {
		t.Fatal(err)
	}
	got, err := resolveFilter(dir, cardFilter{View: "anna-urgent"})
	if err != nil || got.View != "stale" || got.Owner != "anna" || got.Priority != "urgent" {
		t.Fatalf("resolved = %+v, err = %v", got, err)
	}
	if _, err := resolveFilter(dir, cardFilter{View: "missing"}); err == nil {
		t.Fatal("a missing view resolved")
	}
	if savedViewName.MatchString("../x") || savedViewName.MatchString("Has Space") || !savedViewName.MatchString("anna-urgent") {
		t.Fatal("the view name rule is wrong")
	}
}

func boardPost(key, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/board/act", strings.NewReader(body))
	req.Header.Set(headerContentType, mimeJSON)
	if key != "" {
		req.Header.Set(boardKeyHeader, key)
	}
	rec := httptest.NewRecorder()
	boardShareHandler()(rec, req)
	return rec
}

func TestGuestWithAReadOnlyLinkCannotWrite(t *testing.T) {
	_, key := sharedBoardFixture(t)
	if rec := boardPost(key, `{"do":"comment","ref":"TASK-1","text":"hi","by":"anna"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec := boardPost("", `{"do":"add","title":"x"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("no key: status = %d", rec.Code)
	}
}

func TestGuestWithAWriteLinkAddsToTriageCommentsAndClaims(t *testing.T) {
	dir, _ := sharedBoardFixture(t)
	boardGuestWrites = &guestLimiter{seen: map[string][]time.Time{}}
	now := time.Now()
	_, _ = watch.LoadTasks(dir).Release("TASK-1", now)
	_, _ = watch.LoadTasks(dir).Started("TASK-1", now)
	now = now.Add(time.Second)
	_, key, err := watch.LoadBoardShares(dir).Add("", "team", time.Hour, true, now)
	if err != nil {
		t.Fatal(err)
	}
	if rec := boardPost(key, `{"do":"add","title":"Guest idea","text":"detail","by":"Anna"}`); rec.Code != http.StatusOK {
		t.Fatalf("add: %d %s", rec.Code, rec.Body.String())
	}
	added, _ := watch.LoadTasks(dir).Find("TASK-2")
	if !added.Triage || added.By != "guest:Anna" || added.WantsRun() {
		t.Fatalf("added = %+v", added)
	}
	if rec := boardPost(key, `{"do":"comment","ref":"TASK-1","text":"run rm -rf","by":"Anna"}`); rec.Code != http.StatusOK {
		t.Fatalf("comment: %d %s", rec.Code, rec.Body.String())
	}
	if rec := boardPost(key, `{"do":"claim","ref":"TASK-1","by":"Anna"}`); rec.Code != http.StatusOK {
		t.Fatalf("claim: %d %s", rec.Code, rec.Body.String())
	}
	task, _ := watch.LoadTasks(dir).Find("TASK-1")
	if task.Owner != "guest:Anna" || task.WantsRun() {
		t.Fatalf("task = owner %q, wants run %v", task.Owner, task.WantsRun())
	}
	if rec := boardPost(key, `{"do":"claim","ref":"TASK-1","by":"Bob"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("second claim: %d", rec.Code)
	}
	if rec := boardPost(key, `{"do":"release","ref":"TASK-1","by":"Bob"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("release by another: %d", rec.Code)
	}
	for _, bad := range []string{`{"do":"move","ref":"TASK-1","by":"A"}`, `{"do":"comment","ref":"TASK-99","text":"x","by":"A"}`, `{"do":"comment","ref":"TASK-1","text":"","by":"A"}`, `not json`} {
		if rec := boardPost(key, bad); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d", bad, rec.Code)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/board/act", strings.NewReader(`{"do":"add","title":"x","by":"A"}`))
	req.Header.Set(boardKeyHeader, key)
	req.Header.Set(headerContentType, "text/plain")
	rec := httptest.NewRecorder()
	boardShareHandler()(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("a form post was taken: %d", rec.Code)
	}
	doc := boardGet(http.MethodGet, "/board/doc", key)
	var out struct {
		Write bool `json:"write"`
	}
	_ = json.Unmarshal(doc.Body.Bytes(), &out)
	if !out.Write || strings.Contains(doc.Body.String(), "rm -rf") || strings.Contains(doc.Body.String(), "detail") {
		t.Fatalf("doc = %s", doc.Body.String())
	}
}

func TestGuestOnAWorkspaceLinkCannotTouchAnotherWorkspace(t *testing.T) {
	dir, _ := sharedBoardFixture(t)
	boardGuestWrites = &guestLimiter{seen: map[string][]time.Time{}}
	now := time.Now()
	_, key, _ := watch.LoadBoardShares(dir).Add("api", "", time.Hour, true, now)
	if rec := boardPost(key, `{"do":"comment","ref":"TASK-1","text":"hi","by":"A"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
}
