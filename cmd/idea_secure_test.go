package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/idea"
)

func sharedPage(t *testing.T) (string, *idea.Idea, http.HandlerFunc) {
	t.Helper()
	dir := phoneBoard(t, false)
	quietIdeaNotify(t)
	was := ideaAutoAnswerHere
	ideaAutoAnswerHere = false
	t.Cleanup(func() { ideaAutoAnswerHere = was })
	ideaGuestWrites = &guestLimiter{seen: map[string][]time.Time{}}
	id, err := idea.Create(dir, "", "Checkout", "# Checkout\n\nCard first.\n", idea.Meta{Name: "Checkout", Summary: "One tap pay"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return dir, id, ideaOpenHandler("secret", "")
}

func TestAnExpiredOrReadOnlyLinkKeepsGuestsOut(t *testing.T) {
	dir, id, open := sharedPage(t)
	base, k := "/idea/"+id.Meta.Slug, "?k="+id.Meta.Key
	if len(id.Meta.Key) != 32 {
		t.Fatalf("a new key is 128 bits: %q", id.Meta.Key)
	}
	ro := true
	if _, err := shareIdea(dir, shareRequest{Target: id.Meta.Slug, ReadOnly: &ro}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if rec := ideaGet(open, base+"/doc"+k); rec.Code != 200 {
		t.Fatalf("a read-only link reads: %d", rec.Code)
	}
	if rec := post(open, base+"/comment"+k, `{"text":"hi","by":"x"}`); rec.Code != 403 {
		t.Fatalf("a read-only link does not write: %d", rec.Code)
	}
	ro = false
	hour := time.Hour
	if _, err := shareIdea(dir, shareRequest{Target: id.Meta.Slug, ReadOnly: &ro, For: &hour}, time.Now().Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if rec := ideaGet(open, base+"/doc"+k); rec.Code != 403 || !strings.Contains(rec.Body.String(), "expired") {
		t.Fatalf("an expired link: %d %s", rec.Code, rec.Body)
	}
	res, err := shareIdea(dir, shareRequest{Target: id.Meta.Slug}, time.Now())
	if err != nil || strings.Contains(res.Link, id.Meta.Key) || !res.Until.IsZero() {
		t.Fatalf("sharing an expired page again gives a fresh link with no limit: %+v %v", res, err)
	}
	if d, err := parseLinkFor("7d"); err != nil || d != 7*24*time.Hour {
		t.Fatalf("7d: %v %v", d, err)
	}
	if _, err := parseLinkFor("soon"); err == nil {
		t.Fatal("soon is not a duration")
	}
}

func TestGuestWritesAreMarkedAndLimited(t *testing.T) {
	_, id, open := sharedPage(t)
	base, k := "/idea/"+id.Meta.Slug, "?k="+id.Meta.Key
	rec := post(open, base+"/comment"+k, `{"text":"why card first?","by":"andrii"}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"guest":true`) {
		t.Fatalf("a comment through the link says so, whatever name it gives: %s", rec.Body)
	}
	ok := 1
	for i := 0; i < 30; i++ {
		req := httptest.NewRequest(http.MethodPost, base+"/comment"+k, strings.NewReader(`{"text":"spam","by":"bot"}`))
		r := httptest.NewRecorder()
		open(r, req)
		if r.Code == 200 {
			ok++
		} else if r.Code != http.StatusTooManyRequests {
			t.Fatalf("unexpected %d", r.Code)
		}
	}
	if ok != guestWritesPerAddress {
		t.Fatalf("one address writes %d a minute, got %d", guestWritesPerAddress, ok)
	}
	if rec := ideaGet(open, base+"/doc"+k); rec.Code != 200 {
		t.Fatal("reading is never limited")
	}
}

func TestOnlyTheOwnerDeletesAComment(t *testing.T) {
	dir, id, open := sharedPage(t)
	base, k := "/idea/"+id.Meta.Slug, "?k="+id.Meta.Key
	post(open, base+"/comment"+k, `{"text":"buy pills","by":"spam"}`)
	if rec := post(open, base+"/delete"+k, `{"id":1}`); rec.Code != 403 {
		t.Fatalf("a link holder cannot delete: %d", rec.Code)
	}
	if _, _, err := ideaPerform(dir, ideaAction{Do: "delete", Slug: id.Meta.Slug, ID: 1}, true, "me"); err != nil {
		t.Fatal(err)
	}
	got, _ := idea.Load(dir, id.Meta.Slug)
	if len(got.Comments.Comments) != 0 {
		t.Fatalf("deleted: %+v", got.Comments.Comments)
	}
}

func TestALeakedLinkCannotApproveItsWayIntoABuild(t *testing.T) {
	dir, id, open := sharedPage(t)
	on := "on"
	if _, err := setIdea(dir, id.Meta.Slug, ideaChanges{BuildWhenApproved: on}); err != nil {
		t.Fatal(err)
	}
	rec := post(open, "/idea/"+id.Meta.Slug+"/approve?k="+id.Meta.Key, `{"by":"mallory"}`)
	var res approveResult
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res.JustApproved || !strings.Contains(res.Why, "your own sign-off") {
		t.Fatalf("a guest's sign-off approves but never builds: %s", rec.Body)
	}
	got, _ := idea.Load(dir, id.Meta.Slug)
	if got.Meta.OwnerApproved() || got.Meta.Status != idea.Approved {
		t.Fatalf("approved by a guest, not by the owner: %+v", got.Meta)
	}
}

func TestTheShellIsLockedDownAndUnfurlsOnlyForItsKey(t *testing.T) {
	_, id, open := sharedPage(t)
	rec := ideaGet(open, "/idea/"+id.Meta.Slug+"?k="+id.Meta.Key)
	h := rec.Header()
	if !strings.Contains(h.Get("Content-Security-Policy"), "frame-ancestors 'none'") || h.Get("Referrer-Policy") != "no-referrer" || !strings.Contains(h.Get("X-Robots-Tag"), "noindex") {
		t.Fatalf("headers: %v", h)
	}
	if !strings.Contains(rec.Body.String(), `<meta property="og:title" content="Checkout">`) {
		t.Fatal("a link with its key unfurls with the page's name")
	}
	if strings.Contains(ideaGet(open, "/idea/"+id.Meta.Slug+"?k=wrong").Body.String(), "One tap pay") {
		t.Fatal("a wrong key learns nothing about the page")
	}
}

func TestBatchedNoticesGoOutAsOneSummary(t *testing.T) {
	dir := phoneBoard(t, false)
	var sent []string
	was := sendIdeaNotice
	sendIdeaNotice = func(_, title, body, _, _ string) { sent = append(sent, title+"\n"+body) }
	t.Cleanup(func() { sendIdeaNotice = was })
	os.WriteFile(agentUserConfigPath(dir), []byte("ideaNotify: batch\nideaNotifyEvery: 5m\n"), 0o600)
	if mode, every := ideaNotifyMode(dir); mode != ideaNotifyBatch || every != 5*time.Minute {
		t.Fatalf("mode %s every %s", mode, every)
	}
	t0 := time.Now().Add(-6 * time.Minute)
	for _, n := range []ideaNotice{
		{Slug: "checkout", Page: "Checkout", What: "comment by max", At: t0},
		{Slug: "checkout", Page: "Checkout", What: "comment by max", At: t0},
		{Slug: "rota", Page: "Rota", What: "answered 2 comment(s)", At: t0.Add(time.Minute)},
	} {
		queueIdeaNotice(dir, n)
	}
	title, body, slug := ideaSummary(readIdeaNotices(ideaNoticesPath(dir)))
	if title != "💡 3 update(s) on 2 page(s)" || slug != "" || body != "Rota: answered 2 comment(s)\nCheckout: comment by max ×2" {
		t.Fatalf("%q %q %q", title, body, slug)
	}
	flushIdeaNotices(dir, time.Now())
	if len(readIdeaNotices(ideaNoticesPath(dir))) != 0 || len(sent) != 1 || !strings.HasPrefix(sent[0], "💡 3 update(s)") {
		t.Fatalf("sent once, gone: %q", sent)
	}
	queueIdeaNotice(dir, ideaNotice{Slug: "checkout", Page: "Checkout", What: "comment by ana", At: time.Now()})
	flushIdeaNotices(dir, time.Now())
	if len(readIdeaNotices(ideaNoticesPath(dir))) != 1 {
		t.Fatal("a fresh notice waits for its batch")
	}
}
