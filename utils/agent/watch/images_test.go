package watch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var pngBytes = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")

func writePicture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, pngBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestJiraCommentShowsUploadedPicturesInline(t *testing.T) {
	var mu sync.Mutex
	var uploaded []string
	var comment, commentPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/issue/ABC-1":
			io.WriteString(w, `{"fields":{"attachment":[{"filename":"grid.png"}]}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/3/issue/ABC-1/attachments":
			if r.Header.Get("X-Atlassian-Token") != "no-check" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			file, header, err := r.FormFile("file")
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			file.Close()
			uploaded = append(uploaded, header.Filename+" "+header.Header.Get("Content-Type"))
			io.WriteString(w, `[{"id":"1"}]`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/comment"):
			commentPath = r.URL.Path
			var body struct {
				Body string `json:"body"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			comment = body.Body
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	j := &Jira{URL: srv.URL, Email: "me@example.com", Token: "t", Client: srv.Client()}

	err := j.CommentWithImages(context.Background(), "ABC-1", "Looks right on 412!", []string{writePicture(t, "grid.png"), writePicture(t, "hero.png")})
	if err != nil {
		t.Fatal(err)
	}
	if commentPath != "/rest/api/2/issue/ABC-1/comment" {
		t.Fatalf("only the v2 API turns !name! into the picture, posted to %s", commentPath)
	}
	if len(uploaded) != 2 || uploaded[0] != "grid-2.png image/png" || uploaded[1] != "hero.png image/png" {
		t.Fatalf("each picture is uploaded with its type, and a taken name gets a suffix: %v", uploaded)
	}
	if !strings.Contains(comment, "!grid-2.png|thumbnail!") || !strings.Contains(comment, "!hero.png|thumbnail!") {
		t.Fatalf("the comment must embed every picture it uploaded: %q", comment)
	}
	if !strings.HasPrefix(comment, `Looks right on 412\!`) {
		t.Fatalf("a ! in the text must not open an image: %q", comment)
	}
}

func TestJiraCommentRefusesAFileThatIsNotAPicture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.txt")
	os.WriteFile(path, []byte("hello"), 0o600)
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true }))
	defer srv.Close()
	j := &Jira{URL: srv.URL, Client: srv.Client()}

	if err := j.CommentWithImages(context.Background(), "ABC-1", "x", []string{path}); err == nil {
		t.Fatal("a text file is not a picture")
	}
	if called {
		t.Fatal("nothing may reach the tracker when a file is refused")
	}
}

func TestJiraWikiKeepsTextPlainAndLinksWorking(t *testing.T) {
	got := jiraWikiWithImages("see [this] {code} at https://x.io/a[1]!\n", nil)
	if got != `see \[this\] \{code\} at https://x.io/a[1]!` {
		t.Fatalf("markup characters are escaped, links are not: %q", got)
	}
}

func TestLinearCommentEmbedsTheUploadedAsset(t *testing.T) {
	var put http.Header
	var comment string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			put = r.Header.Clone()
			return
		}
		var q struct {
			Query string `json:"query"`
		}
		json.NewDecoder(r.Body).Decode(&q)
		switch {
		case strings.Contains(q.Query, "fileUpload"):
			io.WriteString(w, `{"data":{"fileUpload":{"success":true,"uploadFile":{"uploadUrl":"`+srv.URL+`/put","assetUrl":"https://uploads.linear.app/a.png","headers":[{"key":"x-goog-meta","value":"1"}]}}}}`)
		case strings.Contains(q.Query, "issue(id:"):
			io.WriteString(w, `{"data":{"issue":{"id":"uuid"}}}`)
		case strings.Contains(q.Query, "commentCreate"):
			comment = q.Query
			io.WriteString(w, `{"data":{"commentCreate":{"success":true}}}`)
		}
	}))
	defer srv.Close()
	l := &Linear{Token: "t", URL: srv.URL, Client: srv.Client()}

	if err := l.CommentWithImages(context.Background(), "ABC-1", "here", []string{writePicture(t, "a.png")}); err != nil {
		t.Fatal(err)
	}
	if put.Get("Content-Type") != "image/png" || put.Get("X-Goog-Meta") != "1" {
		t.Fatalf("the PUT must carry the type and the headers Linear signed: %v", put)
	}
	if !strings.Contains(comment, `![a.png](https://uploads.linear.app/a.png)`) {
		t.Fatalf("the comment must embed the asset: %s", comment)
	}
}

func TestJiraWikiTurnsMarkdownLinksIntoWikiLinks(t *testing.T) {
	got := jiraWikiWithImages("fixed in [!9168](https://gitlab.com/g/p/-/merge_requests/9168) [x]", nil)
	if got != `fixed in [\!9168|https://gitlab.com/g/p/-/merge_requests/9168] \[x\]` {
		t.Fatalf("markdown link becomes a wiki link: %q", got)
	}
}
