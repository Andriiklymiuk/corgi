package watch

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAReplyGoesIntoItsThreadOnBothForges(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		seen = append(seen, r.Method+" "+r.URL.RequestURI()+" "+string(raw))
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := threadsClient{http: srv.Client(), github: srv.URL, ghToken: func() string { return "ghp-x" },
		gitlab: func(Secrets, string) (string, error) { return srv.URL + "/mr", nil }}
	ctx := context.Background()
	github, gitlab := "https://github.com/acme/api/pull/42", "https://git.acme.dev/acme/api/-/merge_requests/7"

	steps := []error{
		c.reply(ctx, Secrets{}, github, ThreadTarget{Thread: "PRRT_1", Note: "41"}, "done"),
		c.reply(ctx, Secrets{}, github, ThreadTarget{}, "thanks"),
		c.resolve(ctx, Secrets{}, github, "PRRT_1"),
		c.reply(ctx, Secrets{GitLab: "gl"}, gitlab, ThreadTarget{Thread: "abc", Note: "9"}, "done"),
		c.resolve(ctx, Secrets{GitLab: "gl"}, gitlab, "abc"),
	}
	for i, err := range steps {
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	want := []string{
		"POST /repos/acme/api/pulls/42/comments/41/replies",
		"POST /repos/acme/api/issues/42/comments",
		"POST /graphql",
		"POST /mr/discussions/abc/notes",
		"PUT /mr/discussions/abc?resolved=true",
	}
	for i, w := range want {
		if !strings.HasPrefix(seen[i], w) {
			t.Fatalf("call %d = %q, want %q", i, seen[i], w)
		}
	}
	if err := c.reply(ctx, Secrets{}, github, ThreadTarget{}, "  "); err == nil {
		t.Fatal("an empty reply is not sent")
	}
}
