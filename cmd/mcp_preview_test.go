package cmd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Preview: a ticket names one running service of one workspace's stack
// and opens a door to that port alone, for ten minutes, with no header —
// a WebView loads a page and its assets through it. Anything else is a 404.
func TestPreviewTicketOpensOneServiceForAWhile(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", base)
	_ = os.MkdirAll(filepath.Join(base, "agent"), 0o700)

	// The "web" service: a local server that says where it was asked.
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, "web says "+r.URL.Path+"?"+r.URL.RawQuery+" via "+r.Header.Get("X-Forwarded-Prefix"))
	}))
	t.Cleanup(web.Close)
	u, _ := url.Parse(web.URL)
	port, _ := strconv.Atoi(u.Port())

	origRoot, origSnap := previewWorkspaceRoot, previewSnapshot
	t.Cleanup(func() { previewWorkspaceRoot, previewSnapshot = origRoot, origSnap })
	previewWorkspaceRoot = func(id string) (string, error) {
		if id != "acme" {
			return "", os.ErrNotExist
		}
		return base, nil
	}
	previewSnapshot = func(ctx context.Context, root string) ([]StackService, int, error) {
		return []StackService{{Name: "web", Kind: "service", Port: port, Status: "running"}, {Name: "db", Kind: "database", Port: port + 1, Status: "running"}, {Name: "worker", Kind: "service", Status: "stopped"}}, 2, nil
	}
	previewNow = func() time.Time { return time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { previewNow = time.Now })

	ask := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		launchPreviewHandler(rec, httptest.NewRequest(http.MethodPost, "/launch/preview", strings.NewReader(body)))
		return rec
	}
	if rec := ask(`{"workspace":"acme","service":"worker"}`); rec.Code != http.StatusConflict {
		t.Fatalf("a stopped service has no door: %d %s", rec.Code, rec.Body)
	}
	if rec := ask(`{"workspace":"acme","service":"db"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("a database is not a web page: %d %s", rec.Code, rec.Body)
	}
	if rec := ask(`{"workspace":"nope","service":"web"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown workspace: %d", rec.Code)
	}
	rec := ask(`{"workspace":"acme","service":"web"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("ticket: %d %s", rec.Code, rec.Body)
	}
	var ticket struct {
		URL       string    `json:"url"`
		ExpiresAt time.Time `json:"expiresAt"`
		Service   string    `json:"service"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &ticket)
	if !strings.HasPrefix(ticket.URL, "/launch/preview/") || !strings.HasSuffix(ticket.URL, "/") || ticket.Service != "web" {
		t.Fatalf("the door: %+v", ticket)
	}
	if ticket.ExpiresAt.Sub(previewNow()) != previewFor {
		t.Fatalf("ten minutes: %s", ticket.ExpiresAt.Sub(previewNow()))
	}
	token := strings.TrimSuffix(strings.TrimPrefix(ticket.URL, "/launch/preview/"), "/")
	if len(token) < 40 {
		t.Fatalf("a ticket is a long random string, not %q", token)
	}

	// Through the door: the path after the ticket, the query with it.
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		previewProxyHandler(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	rec = get(ticket.URL + "assets/app.js?v=2")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "web says /assets/app.js?v=2 via "+ticket.URL) {
		t.Fatalf("proxied: %d %s", rec.Code, rec.Body)
	}
	if rec := get("/launch/preview/" + strings.Repeat("x", len(token)) + "/"); rec.Code != http.StatusNotFound {
		t.Fatalf("a made-up ticket: %d", rec.Code)
	}
	if rec := get("/launch/preview/" + token); rec.Code != http.StatusNotFound {
		t.Fatalf("the ticket alone, no trailing slash, is not a page: %d", rec.Code)
	}
	// Ten minutes later the door is shut.
	previewNow = func() time.Time { return time.Date(2026, 9, 14, 12, 11, 0, 0, time.UTC) }
	if rec := get(ticket.URL); rec.Code != http.StatusNotFound {
		t.Fatalf("expired: %d", rec.Code)
	}
}

func TestPreviewRefusesAViewer(t *testing.T) {
	if viewerMay(http.MethodPost, "/launch/preview") {
		t.Fatal("a viewer opens no doors")
	}
}
