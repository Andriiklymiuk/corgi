package cmd

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
)

// A phone on cellular reads the board every few seconds: the launcher's
// JSON goes out gzipped, and a body the phone already has is a 304. A
// handler that flushes (events, a transcript stream) is left alone.
const gzipFrom = 1024

func launchCompression(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, "/launch/") {
			next.ServeHTTP(w, r)
			return
		}
		rec := &bufferedResponse{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if rec.passthrough {
			return
		}
		body := rec.buf.Bytes()
		if rec.status == http.StatusOK && strings.HasPrefix(w.Header().Get(headerContentType), mimeJSON) {
			sum := sha256.Sum256(body)
			tag := `"` + hex.EncodeToString(sum[:8]) + `"`
			w.Header().Set("ETag", tag)
			if r.Header.Get("If-None-Match") == tag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			if len(body) >= gzipFrom && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
				w.Header().Set("Content-Encoding", "gzip")
				w.Header().Add("Vary", "Accept-Encoding")
				w.WriteHeader(rec.status)
				zw := gzip.NewWriter(w)
				_, _ = zw.Write(body)
				_ = zw.Close()
				return
			}
		}
		w.WriteHeader(rec.status)
		_, _ = w.Write(body)
	})
}

type bufferedResponse struct {
	http.ResponseWriter
	buf         bytes.Buffer
	status      int
	passthrough bool
}

func (b *bufferedResponse) WriteHeader(status int) {
	if b.passthrough {
		b.ResponseWriter.WriteHeader(status)
		return
	}
	b.status = status
}

func (b *bufferedResponse) Write(p []byte) (int, error) {
	if b.passthrough {
		return b.ResponseWriter.Write(p)
	}
	return b.buf.Write(p)
}

// Flush is a stream: what is buffered goes out as it is and the rest follows.
func (b *bufferedResponse) Flush() {
	if !b.passthrough {
		b.passthrough = true
		b.ResponseWriter.WriteHeader(b.status)
		_, _ = b.ResponseWriter.Write(b.buf.Bytes())
		b.buf.Reset()
	}
	if f, ok := b.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
