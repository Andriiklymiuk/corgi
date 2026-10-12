package watch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var pngBytes = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")

var (
	mp4Bytes  = []byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom\x00\x00\x00\x08free")
	movBytes  = []byte("\x00\x00\x00\x14ftypqt  \x00\x00\x02\x00qt  \x00\x00\x00\x08wide")
	webmBytes = []byte("\x1a\x45\xdf\xa3\x9f\x42\x86\x81\x01\x42\xf7\x81\x01\x42\xf2\x81\x04webm")
	pdfBytes  = []byte("%PDF-1.7\n1 0 obj\n<< /Type /Catalog >>\nendobj\n")
	zipBytes  = []byte("PK\x03\x04\x14\x00\x06\x00\x08\x00\x00\x00!\x00[Content_Types].xml")
)

func writeFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writePicture(t *testing.T, name string) string {
	t.Helper()
	return writeFile(t, name, pngBytes)
}

func mustRead(t *testing.T, paths ...string) []Attachment {
	t.Helper()
	files, err := ReadAttachments(paths)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestReadAttachmentsTellsPicturesVideosAndDocumentsApart(t *testing.T) {
	cases := []struct {
		name        string
		data        []byte
		contentType string
		kind        AttachmentKind
	}{
		{"grid.png", pngBytes, "image/png", KindPicture},
		{"repro.mp4", mp4Bytes, "video/mp4", KindVideo},
		{"repro.m4v", mp4Bytes, "video/mp4", KindVideo},
		{"phone.mov", movBytes, "video/quicktime", KindVideo},
		{"screen.webm", webmBytes, "video/webm", KindVideo},
		{"report.pdf", pdfBytes, "application/pdf", KindDocument},
		{"orders.csv", []byte("id,total\n1,9.99\n"), "text/csv", KindDocument},
		{"console.log", []byte("12:00 boot\n12:01 crash\n"), "text/plain; charset=utf-8", KindDocument},
		{"answer.json", []byte(`{"ok":true}`), "application/json", KindDocument},
		{"spec.docx", zipBytes, "application/vnd.openxmlformats-officedocument.wordprocessingml.document", KindDocument},
		{"numbers.xlsx", zipBytes, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", KindDocument},
		{"logs.zip", zipBytes, "application/zip", KindDocument},
		{"fake.mp4", []byte("just some text"), "text/plain; charset=utf-8", KindDocument},
		{"fake.png", []byte("just some text"), "text/plain; charset=utf-8", KindDocument},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := mustRead(t, writeFile(t, c.name, c.data))[0]
			if f.ContentType != c.contentType || f.Kind != c.kind {
				t.Fatalf("%s: got %s (%s), want %s (%s)", c.name, f.ContentType, f.Kind, c.contentType, c.kind)
			}
			if f.Name != c.name || f.Size != int64(len(c.data)) {
				t.Fatalf("name and size come from the file: %+v", f)
			}
		})
	}
}

func TestReadAttachmentsRefusesWhatCannotBeSent(t *testing.T) {
	dir := t.TempDir()
	empty := writeFile(t, "empty.log", nil)
	huge := filepath.Join(dir, "huge.mov")
	sparse, err := os.Create(huge)
	if err != nil {
		t.Fatal(err)
	}
	if err := sparse.Truncate(3 << 30); err != nil {
		t.Fatal(err)
	}
	sparse.Close()
	cases := []struct {
		path, want string
	}{
		{filepath.Join(dir, "missing.mp4"), "missing.mp4: no such file or directory"},
		{dir, "it is a directory"},
		{empty, "it is empty"},
		{huge, "it is 3 GB, over the 2 GB a tracker takes"},
	}
	for _, c := range cases {
		_, err := ReadAttachments([]string{writePicture(t, "ok.png"), c.path})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: want %q, got %v", c.path, c.want, err)
		}
	}
	if _, err := ReadAttachments([]string{filepath.Join(dir, "missing.pdf")}); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a missing file keeps its cause: %v", err)
	}
}

func TestReadAttachmentsRefusesAnUnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	path := writeFile(t, "secret.pdf", pdfBytes)
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	_, err := ReadAttachments([]string{path})
	if err == nil || !strings.Contains(err.Error(), "cannot attach "+path+": permission denied") {
		t.Fatalf("an unreadable file is named with the reason: %v", err)
	}
}

type fakeJira struct {
	mu          sync.Mutex
	uploadLimit int64
	enabled     bool
	uploaded    []string
	bodies      map[string][]byte
	lengths     map[string]int64
	comment     string
	commentPath string
}

func newFakeJira(t *testing.T, uploadLimit int64) (*Jira, *fakeJira) {
	t.Helper()
	fake := &fakeJira{uploadLimit: uploadLimit, enabled: true, bodies: map[string][]byte{}, lengths: map[string]int64{}}
	srv := httptest.NewServer(http.HandlerFunc(fake.serve))
	t.Cleanup(srv.Close)
	return &Jira{URL: srv.URL, Email: "me@example.com", Token: "t", Client: srv.Client()}, fake
}

func (f *fakeJira) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/attachment/meta":
		json.NewEncoder(w).Encode(map[string]any{"enabled": f.enabled, "uploadLimit": f.uploadLimit})
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
		data, _ := io.ReadAll(file)
		file.Close()
		f.uploaded = append(f.uploaded, header.Filename+" "+header.Header.Get("Content-Type"))
		f.bodies[header.Filename] = data
		f.lengths[header.Filename] = r.ContentLength
		io.WriteString(w, `[{"id":"1"}]`)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/comment"):
		f.commentPath = r.URL.Path
		var body struct {
			Body string `json:"body"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		f.comment = body.Body
		w.WriteHeader(http.StatusCreated)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestJiraCommentShowsUploadedPicturesInline(t *testing.T) {
	j, fake := newFakeJira(t, 1<<30)

	err := j.CommentWithFiles(context.Background(), "ABC-1", "Looks right on 412!", mustRead(t, writePicture(t, "grid.png"), writePicture(t, "hero.png")))
	if err != nil {
		t.Fatal(err)
	}
	if fake.commentPath != "/rest/api/2/issue/ABC-1/comment" {
		t.Fatalf("only the v2 API turns !name! into the picture, posted to %s", fake.commentPath)
	}
	if len(fake.uploaded) != 2 || fake.uploaded[0] != "grid-2.png image/png" || fake.uploaded[1] != "hero.png image/png" {
		t.Fatalf("each picture is uploaded with its type, and a taken name gets a suffix: %v", fake.uploaded)
	}
	if !strings.Contains(fake.comment, "!grid-2.png|thumbnail!") || !strings.Contains(fake.comment, "!hero.png|thumbnail!") {
		t.Fatalf("the comment must embed every picture it uploaded: %q", fake.comment)
	}
	if !strings.HasPrefix(fake.comment, `Looks right on 412\!`) {
		t.Fatalf("a ! in the text must not open an image: %q", fake.comment)
	}
}

func TestJiraCommentPlaysAVideoAndCardsADocument(t *testing.T) {
	j, fake := newFakeJira(t, 1<<30)
	video := bytes.Repeat(mp4Bytes, 4096)
	files := mustRead(t, writeFile(t, "repro.mp4", video), writeFile(t, "report.pdf", pdfBytes))

	if err := j.CommentWithFiles(context.Background(), "ABC-1", "Repro on 412", files); err != nil {
		t.Fatal(err)
	}
	if strings.Join(fake.uploaded, ",") != "repro.mp4 video/mp4,report.pdf application/pdf" {
		t.Fatalf("each file is uploaded with its type: %v", fake.uploaded)
	}
	if !bytes.Equal(fake.bodies["repro.mp4"], video) || !bytes.Equal(fake.bodies["report.pdf"], pdfBytes) {
		t.Fatal("the streamed upload must carry the whole file, byte for byte")
	}
	if fake.lengths["repro.mp4"] <= int64(len(video)) {
		t.Fatalf("the upload states its length up front, got %d", fake.lengths["repro.mp4"])
	}
	want := "Repro on 412\n\n!repro.mp4|width=900!\n\n[^report.pdf]"
	if fake.comment != want {
		t.Fatalf("a video plays inline and a document is a file card:\n got %q\nwant %q", fake.comment, want)
	}
}

func TestJiraRefusesAFileOverTheSiteLimitBeforeUploading(t *testing.T) {
	j, fake := newFakeJira(t, 10)

	err := j.CommentWithFiles(context.Background(), "ABC-1", "x", mustRead(t, writeFile(t, "report.pdf", pdfBytes)))
	want := fmt.Sprintf("report.pdf: it is %d bytes, over the 10 bytes this Jira takes per file", len(pdfBytes))
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("an oversized file is refused with both sizes: %v", err)
	}
	if len(fake.uploaded) != 0 || fake.comment != "" {
		t.Fatal("nothing is uploaded or posted when a file is over the limit")
	}
}

func TestJiraRefusesWhenAttachmentsAreOff(t *testing.T) {
	j, fake := newFakeJira(t, 1<<30)
	fake.enabled = false

	err := j.CommentWithFiles(context.Background(), "ABC-1", "x", mustRead(t, writePicture(t, "a.png")))
	if err == nil || !strings.Contains(err.Error(), "attachments are turned off") {
		t.Fatalf("a site without attachments says so: %v", err)
	}
	if len(fake.uploaded) != 0 || fake.comment != "" {
		t.Fatal("nothing is uploaded or posted")
	}
}

func TestJiraNamesCannotBreakTheMarkup(t *testing.T) {
	j, fake := newFakeJira(t, 1<<30)

	if err := j.CommentWithFiles(context.Background(), "ABC-1", "x", mustRead(t, writeFile(t, "a|b![1].pdf", pdfBytes))); err != nil {
		t.Fatal(err)
	}
	if fake.uploaded[0] != "a-b-(1).pdf application/pdf" || !strings.HasSuffix(fake.comment, "[^a-b-(1).pdf]") {
		t.Fatalf("| ! [ ] would end the embed early: %v %q", fake.uploaded, fake.comment)
	}
}

func TestJiraWikiEmbedsEachKind(t *testing.T) {
	got := jiraWikiWithFiles("see", []Attachment{
		{Name: "a.png", Kind: KindPicture},
		{Name: "b.mov", Kind: KindVideo},
		{Name: "c.csv", Kind: KindDocument},
	})
	if got != "see\n\n!a.png|thumbnail!\n\n!b.mov|width=900!\n\n[^c.csv]" {
		t.Fatalf("picture thumbnail, video player, document card: %q", got)
	}
}

func TestJiraWikiKeepsTextPlainAndLinksWorking(t *testing.T) {
	got := jiraWikiWithFiles("see [this] {code} at https://x.io/a[1]!\n", nil)
	if got != `see \[this\] \{code\} at https://x.io/a[1]!` {
		t.Fatalf("markup characters are escaped, links are not: %q", got)
	}
}

func TestJiraWikiTurnsMarkdownLinksIntoWikiLinks(t *testing.T) {
	got := jiraWikiWithFiles("fixed in [!9168](https://gitlab.com/g/p/-/merge_requests/9168) [x]", nil)
	if got != `fixed in [\!9168|https://gitlab.com/g/p/-/merge_requests/9168] \[x\]` {
		t.Fatalf("markdown link becomes a wiki link: %q", got)
	}
}

type fakeLinear struct {
	mu       sync.Mutex
	noIssue  bool
	uploads  []string
	puts     []http.Header
	putBody  [][]byte
	putSizes []int64
	comment  string
}

func newFakeLinear(t *testing.T) (*Linear, *fakeLinear) {
	t.Helper()
	fake := &fakeLinear{}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		if r.Method == http.MethodPut {
			data, _ := io.ReadAll(r.Body)
			fake.puts = append(fake.puts, r.Header.Clone())
			fake.putBody = append(fake.putBody, data)
			fake.putSizes = append(fake.putSizes, r.ContentLength)
			return
		}
		var q struct {
			Query string `json:"query"`
		}
		json.NewDecoder(r.Body).Decode(&q)
		switch {
		case strings.Contains(q.Query, "fileUpload"):
			fake.uploads = append(fake.uploads, q.Query)
			n := len(fake.uploads)
			io.WriteString(w, `{"data":{"fileUpload":{"success":true,"uploadFile":{"uploadUrl":"`+srv.URL+`/put","assetUrl":"https://uploads.linear.app/`+string(rune('0'+n))+`","headers":[{"key":"x-goog-meta","value":"1"}]}}}}`)
		case strings.Contains(q.Query, "issue(id:"):
			if fake.noIssue {
				io.WriteString(w, `{"data":{"issue":null}}`)
				return
			}
			io.WriteString(w, `{"data":{"issue":{"id":"uuid"}}}`)
		case strings.Contains(q.Query, "commentCreate"):
			fake.comment = q.Query
			io.WriteString(w, `{"data":{"commentCreate":{"success":true}}}`)
		}
	}))
	t.Cleanup(srv.Close)
	return &Linear{Token: "t", URL: srv.URL, Client: srv.Client()}, fake
}

func TestLinearCommentEmbedsTheUploadedAsset(t *testing.T) {
	l, fake := newFakeLinear(t)

	if err := l.CommentWithFiles(context.Background(), "ABC-1", "here", mustRead(t, writePicture(t, "a.png"))); err != nil {
		t.Fatal(err)
	}
	if fake.puts[0].Get("Content-Type") != "image/png" || fake.puts[0].Get("X-Goog-Meta") != "1" {
		t.Fatalf("the PUT must carry the type and the headers Linear signed: %v", fake.puts[0])
	}
	if !strings.Contains(fake.comment, `![a.png](https://uploads.linear.app/1)`) {
		t.Fatalf("the comment must embed the asset: %s", fake.comment)
	}
}

func TestLinearCommentLinksAVideoAndADocument(t *testing.T) {
	l, fake := newFakeLinear(t)
	video := bytes.Repeat(movBytes, 2048)
	files := mustRead(t, writeFile(t, "phone.mov", video), writeFile(t, "orders.csv", []byte("id\n1\n")))

	if err := l.CommentWithFiles(context.Background(), "ABC-1", "repro", files); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fake.uploads[0], `contentType: "video/quicktime"`) || !strings.Contains(fake.uploads[0], fmt.Sprintf("size: %d", len(video))) {
		t.Fatalf("Linear signs the upload for the real type and size: %s", fake.uploads[0])
	}
	if !bytes.Equal(fake.putBody[0], video) || fake.putSizes[0] != int64(len(video)) {
		t.Fatalf("the PUT streams the whole file with its length: %d bytes, length %d", len(fake.putBody[0]), fake.putSizes[0])
	}
	if fake.puts[1].Get("Content-Type") != "text/csv" {
		t.Fatalf("a csv goes up as text/csv: %v", fake.puts[1])
	}
	if !strings.Contains(fake.comment, `[phone.mov](https://uploads.linear.app/1)`) || strings.Contains(fake.comment, `![phone.mov]`) {
		t.Fatalf("a video is a link, not a broken image: %s", fake.comment)
	}
	if !strings.Contains(fake.comment, `[orders.csv](https://uploads.linear.app/2)`) {
		t.Fatalf("a document is a link: %s", fake.comment)
	}
}

func TestLinearLooksUpTheIssueBeforeUploading(t *testing.T) {
	l, fake := newFakeLinear(t)
	fake.noIssue = true

	err := l.CommentWithFiles(context.Background(), "ABC-404", "x", mustRead(t, writeFile(t, "report.pdf", pdfBytes)))
	if err == nil || !strings.Contains(err.Error(), "no issue ABC-404") {
		t.Fatalf("a wrong key is refused: %v", err)
	}
	if len(fake.uploads) != 0 || fake.comment != "" {
		t.Fatal("a wrong key uploads nothing and posts nothing")
	}
}

func TestDescribeAttachmentsCountsEachKind(t *testing.T) {
	cases := []struct {
		kinds []AttachmentKind
		want  string
	}{
		{[]AttachmentKind{KindPicture}, "1 picture"},
		{[]AttachmentKind{KindPicture, KindPicture}, "2 pictures"},
		{[]AttachmentKind{KindDocument, KindPicture, KindVideo}, "1 picture, 1 video and 1 document"},
		{[]AttachmentKind{KindVideo, KindDocument, KindDocument}, "1 video and 2 documents"},
	}
	for _, c := range cases {
		var files []Attachment
		for _, k := range c.kinds {
			files = append(files, Attachment{Kind: k})
		}
		if got := DescribeAttachments(files); got != c.want {
			t.Fatalf("%v: got %q, want %q", c.kinds, got, c.want)
		}
	}
}

func TestUploadTimeoutGrowsWithTheFiles(t *testing.T) {
	if got := UploadTimeout(nil); got != 2*time.Minute {
		t.Fatalf("no files keep the usual two minutes: %s", got)
	}
	if got := UploadTimeout([]Attachment{{Size: 512 << 20}}); got != 2*time.Minute+1024*time.Second {
		t.Fatalf("512 MB at 512 KB/s adds 1024s: %s", got)
	}
}

func TestSizeLabel(t *testing.T) {
	for n, want := range map[int64]string{12: "12 bytes", 2048: "2 KB", 1536 << 10: "1.5 MB", 1 << 30: "1 GB"} {
		if got := sizeLabel(n); got != want {
			t.Fatalf("%d: got %q, want %q", n, got, want)
		}
	}
}
