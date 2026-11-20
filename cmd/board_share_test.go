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

func sharedBoardFixture(t *testing.T) (dir, key string) {
	t.Helper()
	dir = phoneBoard(t, false)
	boardGuestMisses = &guestLimiter{seen: map[string][]time.Time{}}
	now := time.Now()
	tasks := watch.LoadTasks(dir)
	task, err := tasks.Add("Ship the limits", "private body words", "", "cli", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.Comment(task.Ref(), "you", "private comment words", now); err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.Link(task.Ref(), []string{"https://acme.atlassian.net/browse/ABC-12", "https://github.com/acme/api/pull/59"}, now); err != nil {
		t.Fatal(err)
	}
	_, _ = tasks.Claim(task.Ref(), "anna", false, now)
	_, _ = tasks.SetPriority(task.Ref(), "high", now)
	_, key, err = watch.LoadBoardShares(dir).Add("", "for Anna", time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	return dir, key
}

func boardGet(method, path, key string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if key != "" {
		req.Header.Set(boardKeyHeader, key)
	}
	rec := httptest.NewRecorder()
	boardShareHandler()(rec, req)
	return rec
}

func TestSharedBoardShowsCardsAndNothingPrivate(t *testing.T) {
	_, key := sharedBoardFixture(t)
	rec := boardGet(http.MethodGet, "/board/doc", key)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, private := range []string{"private body words", "private comment words", "session", "cost", "branch", "\"body\"", "/Users/", "keyHash"} {
		if strings.Contains(body, private) {
			t.Fatalf("the shared board leaks %q: %s", private, body)
		}
	}
	var out struct {
		Label string       `json:"label"`
		Cards []sharedCard `json:"cards"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Label != "for Anna" || len(out.Cards) != 1 {
		t.Fatalf("out = %+v", out)
	}
	card := out.Cards[0]
	if card.Title != "Ship the limits" || card.Owner != "anna" || card.Priority != "high" || len(card.Links) != 2 {
		t.Fatalf("card = %+v", card)
	}
	if card.Links[0].Source != "jira" || card.Links[0].Label != "ABC-12" || card.Links[1].Source != "github" {
		t.Fatalf("links = %+v", card.Links)
	}
}

func TestSharedBoardNeedsItsKey(t *testing.T) {
	dir, key := sharedBoardFixture(t)
	for _, bad := range []string{"", "nope", strings.Repeat("a", 48)} {
		if rec := boardGet(http.MethodGet, "/board/doc", bad); rec.Code != http.StatusForbidden {
			t.Fatalf("key %q: status = %d", bad, rec.Code)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/board/doc?k="+key, nil)
	rec := httptest.NewRecorder()
	boardShareHandler()(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a key in the query opened the board: %d", rec.Code)
	}
	if rec := boardGet(http.MethodPost, "/board/doc", key); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d", rec.Code)
	}
	if _, err := watch.LoadBoardShares(dir).Close("all"); err != nil {
		t.Fatal(err)
	}
	if rec := boardGet(http.MethodGet, "/board/doc", key); rec.Code != http.StatusForbidden {
		t.Fatalf("a closed link opened the board: %d", rec.Code)
	}
}

func TestSharedBoardSlowsGuessing(t *testing.T) {
	sharedBoardFixture(t)
	last := 0
	for i := 0; i <= guestWritesPerAddress; i++ {
		last = boardGet(http.MethodGet, "/board/doc", strings.Repeat("b", 48)).Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("status after many wrong keys = %d", last)
	}
}

func TestSharedBoardPageIsLockedDown(t *testing.T) {
	rec := boardGet(http.MethodGet, "/board", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<title>Board</title>") {
		t.Fatalf("page = %d", rec.Code)
	}
	h := rec.Header()
	if !strings.Contains(h.Get(headerContentSecurityPolicy), "default-src 'none'") || !strings.Contains(h.Get(headerContentSecurityPolicy), "frame-ancestors 'none'") {
		t.Fatalf("csp = %q", h.Get(headerContentSecurityPolicy))
	}
	if h.Get("Referrer-Policy") != "no-referrer" || h.Get("X-Robots-Tag") == "" || h.Get("Cache-Control") != "no-store" {
		t.Fatalf("headers = %v", h)
	}
	if strings.Contains(rec.Body.String(), "innerHTML") {
		t.Fatal("the page writes HTML from data")
	}
}

func TestSharedCardDropsLinksThatAreNotWebAddresses(t *testing.T) {
	card := sharedCardOf(KanbanCard{Ref: "X-1", URL: "javascript:alert(1)", Body: "secret",
		Fix: &CardFix{PRs: []string{"file:///etc/passwd", "https://gitlab.com/a/b/-/merge_requests/2"}}})
	if card.URL != "" || len(card.Links) != 1 || card.Links[0].Label != "a/b!2" {
		t.Fatalf("card = %+v", card)
	}
}
