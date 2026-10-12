package watch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type AttachmentKind string

const (
	KindPicture  AttachmentKind = "picture"
	KindVideo    AttachmentKind = "video"
	KindDocument AttachmentKind = "document"
)

type Attachment struct {
	Path, Name, ContentType string
	Kind                    AttachmentKind
	Size                    int64
}

type FileCommenter interface {
	CommentWithFiles(ctx context.Context, ref, body string, files []Attachment) error
}

// Linear's fileUpload takes the size as a GraphQL Int; Jira tops out at 2 GB per file.
const maxAttachmentSize = math.MaxInt32

// Sniffing names a family, not the format: a .mov reads as octet-stream, a .docx as
// zip, a .csv as plain text. The extension narrows it only inside the family the bytes show.
var extensionTypes = map[string]struct{ sniffed, contentType string }{
	".mp4":  {"application/octet-stream", "video/mp4"},
	".m4v":  {"application/octet-stream", "video/x-m4v"},
	".mov":  {"application/octet-stream", "video/quicktime"},
	".csv":  {"text/plain", "text/csv"},
	".tsv":  {"text/plain", "text/tab-separated-values"},
	".json": {"text/plain", "application/json"},
	".md":   {"text/plain", "text/markdown"},
	".doc":  {"application/octet-stream", "application/msword"},
	".xls":  {"application/octet-stream", "application/vnd.ms-excel"},
	".docx": {"application/zip", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
	".xlsx": {"application/zip", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
	".pptx": {"application/zip", "application/vnd.openxmlformats-officedocument.presentationml.presentation"},
}

// ReadAttachments checks every file before anything reaches the tracker, so a bad
// path never leaves half a comment behind.
func ReadAttachments(paths []string) ([]Attachment, error) {
	files := make([]Attachment, 0, len(paths))
	for _, path := range paths {
		f, err := readAttachment(path)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}

func readAttachment(path string) (Attachment, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Attachment{}, fileError(path, err)
	}
	switch {
	case info.IsDir():
		return Attachment{}, fmt.Errorf("cannot attach %s: it is a directory", path)
	case !info.Mode().IsRegular():
		return Attachment{}, fmt.Errorf("cannot attach %s: not a regular file", path)
	case info.Size() == 0:
		return Attachment{}, fmt.Errorf("cannot attach %s: it is empty", path)
	case info.Size() > maxAttachmentSize:
		return Attachment{}, fmt.Errorf("cannot attach %s: it is %s, over the %s a tracker takes", path, sizeLabel(info.Size()), sizeLabel(maxAttachmentSize))
	}
	file, err := os.Open(path)
	if err != nil {
		return Attachment{}, fileError(path, err)
	}
	defer file.Close()
	head := make([]byte, 512)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return Attachment{}, fileError(path, err)
	}
	name := filepath.Base(path)
	contentType := detectContentType(name, head[:n])
	return Attachment{Path: path, Name: name, ContentType: contentType, Kind: kindOf(contentType), Size: info.Size()}, nil
}

func fileError(path string, err error) error {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		err = pathErr.Err
	}
	return fmt.Errorf("cannot attach %s: %w", path, err)
}

func detectContentType(name string, head []byte) string {
	sniffed := http.DetectContentType(head)
	family, _, _ := strings.Cut(sniffed, ";")
	if t, ok := extensionTypes[strings.ToLower(filepath.Ext(name))]; ok && t.sniffed == family {
		return t.contentType
	}
	return sniffed
}

func kindOf(contentType string) AttachmentKind {
	switch {
	case strings.HasPrefix(contentType, "image/"):
		return KindPicture
	case strings.HasPrefix(contentType, "video/"):
		return KindVideo
	default:
		return KindDocument
	}
}

// DescribeAttachments reads "2 pictures and 1 video".
func DescribeAttachments(files []Attachment) string {
	counts := map[AttachmentKind]int{}
	for _, f := range files {
		counts[f.Kind]++
	}
	var parts []string
	for _, kind := range []AttachmentKind{KindPicture, KindVideo, KindDocument} {
		switch n := counts[kind]; n {
		case 0:
		case 1:
			parts = append(parts, "1 "+string(kind))
		default:
			parts = append(parts, fmt.Sprintf("%d %ss", n, kind))
		}
	}
	if len(parts) < 2 {
		return strings.Join(parts, "")
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
}

// UploadTimeout gives a slow uplink (512 KB/s) time to send every file.
func UploadTimeout(files []Attachment) time.Duration {
	var total int64
	for _, f := range files {
		total += f.Size
	}
	return 2*time.Minute + time.Duration(total/(512<<10))*time.Second
}

func sizeLabel(n int64) string {
	units := []struct {
		size  int64
		label string
	}{{1 << 30, "GB"}, {1 << 20, "MB"}, {1 << 10, "KB"}}
	for _, u := range units {
		if n >= u.size {
			return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/float64(u.size)), ".0") + " " + u.label
		}
	}
	return fmt.Sprintf("%d bytes", n)
}

// Only v2 wiki markup turns !name! into the attached media; markdown keeps the bare name.
func (j *Jira) CommentWithFiles(ctx context.Context, ref, body string, files []Attachment) error {
	if err := j.checkUploadLimit(ctx, files); err != nil {
		return err
	}
	taken, err := j.attachmentNames(ctx, ref)
	if err != nil {
		return err
	}
	attached := make([]Attachment, 0, len(files))
	for _, f := range files {
		f.Name = unusedName(jiraNameEscaper.Replace(f.Name), taken)
		taken[strings.ToLower(f.Name)] = true
		if err := j.attach(ctx, ref, f); err != nil {
			return err
		}
		attached = append(attached, f)
	}
	return j.send(ctx, http.MethodPost, "/rest/api/2/issue/"+ref+"/comment", map[string]any{"body": jiraWikiWithFiles(body, attached)})
}

// The site's own limit, read before the first upload so an oversized file posts nothing.
func (j *Jira) checkUploadLimit(ctx context.Context, files []Attachment) error {
	var settings struct {
		Enabled     bool  `json:"enabled"`
		UploadLimit int64 `json:"uploadLimit"`
	}
	if err := j.get(ctx, "/rest/api/3/attachment/meta", nil, &settings); err != nil {
		return err
	}
	if !settings.Enabled {
		return errors.New("jira: attachments are turned off on this site")
	}
	for _, f := range files {
		if settings.UploadLimit > 0 && f.Size > settings.UploadLimit {
			return fmt.Errorf("cannot attach %s: it is %s, over the %s this Jira takes per file", f.Path, sizeLabel(f.Size), sizeLabel(settings.UploadLimit))
		}
	}
	return nil
}

func (j *Jira) attachmentNames(ctx context.Context, ref string) (map[string]bool, error) {
	var issue struct {
		Fields struct {
			Attachment []struct {
				Filename string `json:"filename"`
			} `json:"attachment"`
		} `json:"fields"`
	}
	if err := j.get(ctx, "/rest/api/3/issue/"+ref, url.Values{"fields": {"attachment"}}, &issue); err != nil {
		return nil, err
	}
	taken := map[string]bool{}
	for _, a := range issue.Fields.Attachment {
		taken[strings.ToLower(a.Filename)] = true
	}
	return taken, nil
}

// Two attachments with one name make !name! point at whichever Jira picks.
func unusedName(name string, taken map[string]bool) string {
	if !taken[strings.ToLower(name)] {
		return name
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s-%d%s", stem, n, ext)
		if !taken[strings.ToLower(candidate)] {
			return candidate
		}
	}
}

// Streams the file: a video can be a gigabyte, and Jira wants the length up front.
func (j *Jira) attach(ctx context.Context, ref string, f Attachment) error {
	file, err := os.Open(f.Path)
	if err != nil {
		return fileError(f.Path, err)
	}
	defer file.Close()
	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, f.Name))
	header.Set("Content-Type", f.ContentType)
	if _, err := writer.CreatePart(header); err != nil {
		return err
	}
	opening := form.Len()
	if err := writer.Close(); err != nil {
		return err
	}
	envelope := form.Bytes()
	body := io.MultiReader(bytes.NewReader(envelope[:opening]), io.LimitReader(file, f.Size), bytes.NewReader(envelope[opening:]))
	path := "/rest/api/3/issue/" + ref + "/attachments"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(j.URL, "/")+path, body)
	if err != nil {
		return fmt.Errorf("jira %s: %w", path, err)
	}
	req.ContentLength = int64(len(envelope)) + f.Size
	req.SetBasicAuth(j.Email, j.Token)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Atlassian-Token", "no-check")
	return doUpload(j.Client, req, "jira "+path)
}

// Escapes what would start an image, macro or link; URLs stay clickable.
func jiraWikiWithFiles(body string, files []Attachment) string {
	var lines []string
	for _, line := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		var out strings.Builder
		rest := 0
		for _, m := range markdownLinkPattern.FindAllStringSubmatchIndex(line, -1) {
			out.WriteString(wikiEscapeWords(line[rest:m[0]]))
			out.WriteString("[" + wikiEscaper.Replace(strings.ReplaceAll(line[m[2]:m[3]], "|", "/")) + "|" + line[m[4]:m[5]] + "]")
			rest = m[1]
		}
		out.WriteString(wikiEscapeWords(line[rest:]))
		lines = append(lines, out.String())
	}
	text := strings.Join(lines, "\n")
	for _, f := range files {
		text += "\n\n" + jiraEmbed(f)
	}
	return strings.TrimLeft(text, "\n")
}

// A picture shows inline, a video plays inline, anything else is a file card.
func jiraEmbed(f Attachment) string {
	switch f.Kind {
	case KindPicture:
		return "!" + f.Name + "|thumbnail!"
	case KindVideo:
		return "!" + f.Name + "|width=900!"
	default:
		return "[^" + f.Name + "]"
	}
}

func wikiEscapeWords(text string) string {
	words := strings.Split(text, " ")
	for i, word := range words {
		if !strings.Contains(word, "://") {
			words[i] = wikiEscaper.Replace(word)
		}
	}
	return strings.Join(words, " ")
}

var markdownLinkPattern = regexp.MustCompile(`\[([^\[\]\n]+)\]\((https?://[^\s()]+)\)`)

var wikiEscaper = strings.NewReplacer("!", `\!`, "{", `\{`, "}", `\}`, "[", `\[`, "]", `\]`)

// A name with these would end !name! or [^name] early.
var jiraNameEscaper = strings.NewReplacer("!", "-", "|", "-", "[", "(", "]", ")")

var markdownLabelEscaper = strings.NewReplacer("[", `\[`, "]", `\]`)

// The issue is looked up first, so a wrong key uploads nothing.
func (l *Linear) CommentWithFiles(ctx context.Context, ref, body string, files []Attachment) error {
	id, err := l.issueID(ctx, ref)
	if err != nil {
		return err
	}
	text := strings.TrimRight(body, "\n")
	for _, f := range files {
		asset, err := l.upload(ctx, f)
		if err != nil {
			return err
		}
		text += "\n\n" + linearEmbed(f, asset)
	}
	return l.commentOn(ctx, id, ref, strings.TrimLeft(text, "\n"))
}

// Linear shows a markdown image inline; a video or document is a link to the upload.
func linearEmbed(f Attachment, asset string) string {
	label := markdownLabelEscaper.Replace(f.Name)
	if f.Kind == KindPicture {
		return "![" + label + "](" + asset + ")"
	}
	return "[" + label + "](" + asset + ")"
}

func (l *Linear) upload(ctx context.Context, f Attachment) (string, error) {
	document := fmt.Sprintf("mutation { fileUpload(contentType: %s, filename: %s, size: %d) { success uploadFile { uploadUrl assetUrl headers { key value } } } }",
		jsonString(f.ContentType), jsonString(f.Name), f.Size)
	var out struct {
		FileUpload struct {
			Success    bool `json:"success"`
			UploadFile *struct {
				UploadURL string `json:"uploadUrl"`
				AssetURL  string `json:"assetUrl"`
				Headers   []struct {
					Key   string `json:"key"`
					Value string `json:"value"`
				} `json:"headers"`
			} `json:"uploadFile"`
		} `json:"fileUpload"`
	}
	if err := l.query(ctx, document, &out); err != nil {
		return "", err
	}
	upload := out.FileUpload.UploadFile
	if !out.FileUpload.Success || upload == nil || upload.UploadURL == "" {
		return "", fmt.Errorf("linear: the upload of %s was refused", f.Name)
	}
	file, err := os.Open(f.Path)
	if err != nil {
		return "", fileError(f.Path, err)
	}
	defer file.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, upload.UploadURL, io.LimitReader(file, f.Size))
	if err != nil {
		return "", err
	}
	req.ContentLength = f.Size
	req.Header.Set("Content-Type", f.ContentType)
	req.Header.Set("Cache-Control", "public, max-age=31536000")
	for _, h := range upload.Headers {
		req.Header.Set(h.Key, h.Value)
	}
	if err := doUpload(l.Client, req, "linear upload"); err != nil {
		return "", err
	}
	return upload.AssetURL, nil
}

// No client timeout: the caller's context sizes the wait to the files.
func doUpload(client *http.Client, req *http.Request, label string) error {
	if client == nil {
		client = &http.Client{}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s: HTTP %d: %s", label, resp.StatusCode, clip(string(answer), bodyMax))
	}
	return nil
}
