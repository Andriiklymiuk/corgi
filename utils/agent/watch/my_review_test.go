package watch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func reviewFake(t *testing.T, reviews, comments, notes string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			_, _ = w.Write([]byte(`{"login":"andrii"}`))
		case "/repos/acme/web/pulls/456/reviews":
			_, _ = w.Write([]byte(reviews))
		case "/repos/acme/web/pulls/456/comments":
			_, _ = w.Write([]byte(comments))
		case "/repos/acme/web/issues/456/comments":
			_, _ = w.Write([]byte(notes))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestMyReviewSinceSaysWhatTheRunPosted(t *testing.T) {
	since := time.Date(2026, 9, 21, 15, 0, 0, 0, time.UTC)
	srv := reviewFake(t,
		`[{"state":"COMMENTED","submitted_at":"2026-09-21T15:10:00Z","user":{"login":"andrii"}},
		  {"state":"APPROVED","submitted_at":"2026-09-20T10:00:00Z","user":{"login":"andrii"}},
		  {"state":"APPROVED","submitted_at":"2026-09-21T15:20:00Z","user":{"login":"maria"}}]`,
		`[{"created_at":"2026-09-21T15:11:00Z","user":{"login":"andrii"}},
		  {"created_at":"2026-09-21T15:12:00Z","user":{"login":"andrii"}},
		  {"created_at":"2026-09-21T14:00:00Z","user":{"login":"andrii"}}]`,
		`[]`)
	g := &GitHub{Token: "tok", URL: srv.URL}

	got, ok := g.MyReviewSince(context.Background(), "acme/web#456", since)
	if !ok || got.Approved || got.ChangesRequested || got.Comments != 2 {
		t.Fatalf("two inline comments in this run, an old approval and someone else's do not count: %+v ok=%v", got, ok)
	}
	if got.Line() != "comments added (2)" {
		t.Fatalf("line = %q", got.Line())
	}

	srv2 := reviewFake(t, `[{"state":"APPROVED","submitted_at":"2026-09-21T15:30:00Z","user":{"login":"andrii"}}]`, `[]`, `[]`)
	g2 := &GitHub{Token: "tok", URL: srv2.URL}
	got, _ = g2.MyReviewSince(context.Background(), "acme/web#456", since)
	if !got.Approved || got.Line() != "approved ✅" {
		t.Fatalf("an approval says so: %+v %q", got, got.Line())
	}

	srv3 := reviewFake(t, `[]`, `[]`, `[]`)
	g3 := &GitHub{Token: "tok", URL: srv3.URL}
	got, _ = g3.MyReviewSince(context.Background(), "acme/web#456", since)
	if got.Line() != "nothing posted" {
		t.Fatalf("no review, no comment: %q", got.Line())
	}
}
