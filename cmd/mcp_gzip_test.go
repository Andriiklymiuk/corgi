package cmd

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLaunchJSONIsGzippedAndNotModifiedWhenUnchanged(t *testing.T) {
	big := `{"rows":"` + strings.Repeat("x", 3000) + `"}`
	h := launchCompression(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(headerContentType, mimeJSON)
		_, _ = w.Write([]byte(big))
	}))
	req := httptest.NewRequest(http.MethodGet, "/launch/board", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Content-Encoding") != "gzip" || rec.Body.Len() >= len(big)/2 {
		t.Fatalf("not gzipped: %s, %d bytes", rec.Header().Get("Content-Encoding"), rec.Body.Len())
	}
	zr, _ := gzip.NewReader(rec.Body)
	if raw, _ := io.ReadAll(zr); string(raw) != big {
		t.Fatal("the body changed")
	}
	tag := rec.Header().Get("ETag")
	again := httptest.NewRequest(http.MethodGet, "/launch/board", nil)
	again.Header.Set("If-None-Match", tag)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, again)
	if rec2.Code != http.StatusNotModified || rec2.Body.Len() != 0 {
		t.Fatalf("same body is a 304, got %d with %d bytes", rec2.Code, rec2.Body.Len())
	}
	small := httptest.NewRecorder()
	h2 := launchCompression(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(headerContentType, mimeJSON)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	h2.ServeHTTP(small, req)
	if small.Header().Get("Content-Encoding") != "" || small.Body.String() != `{"ok":true}` {
		t.Fatal("a small body is sent as it is")
	}
}

func TestAStreamingLaunchHandlerIsLeftAlone(t *testing.T) {
	h := launchCompression(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(headerContentType, "text/event-stream")
		_, _ = w.Write([]byte("data: one\n\n"))
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte("data: two\n\n"))
	}))
	req := httptest.NewRequest(http.MethodGet, "/launch/events", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Body.String() != "data: one\n\ndata: two\n\n" || rec.Header().Get("Content-Encoding") != "" || !rec.Flushed {
		t.Fatalf("got %q flushed=%v", rec.Body.String(), rec.Flushed)
	}
}
