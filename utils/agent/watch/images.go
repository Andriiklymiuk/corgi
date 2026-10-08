package watch

import (
	"bytes"
	"context"
	"fmt"
	"io"
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

type ImageCommenter interface {
	CommentWithImages(ctx context.Context, ref, body string, images []string) error
}

type picture struct {
	name, contentType string
	data              []byte
}

func readPictures(paths []string) ([]picture, error) {
	pictures := make([]picture, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		contentType := http.DetectContentType(data)
		if !strings.HasPrefix(contentType, "image/") {
			return nil, fmt.Errorf("%s is not a picture (%s)", path, contentType)
		}
		pictures = append(pictures, picture{name: filepath.Base(path), contentType: contentType, data: data})
	}
	return pictures, nil
}

// Only v2 wiki markup turns !name! into the attached picture; markdown keeps the bare name.
func (j *Jira) CommentWithImages(ctx context.Context, ref, body string, images []string) error {
	pictures, err := readPictures(images)
	if err != nil {
		return err
	}
	taken, err := j.attachmentNames(ctx, ref)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(pictures))
	for _, p := range pictures {
		p.name = unusedName(p.name, taken)
		taken[strings.ToLower(p.name)] = true
		if err := j.attach(ctx, ref, p); err != nil {
			return err
		}
		names = append(names, p.name)
	}
	return j.send(ctx, http.MethodPost, "/rest/api/2/issue/"+ref+"/comment", map[string]any{"body": jiraWikiWithImages(body, names)})
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

func (j *Jira) attach(ctx context.Context, ref string, p picture) error {
	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, p.name))
	header.Set("Content-Type", p.contentType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}
	if _, err := part.Write(p.data); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	path := "/rest/api/3/issue/" + ref + "/attachments"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(j.URL, "/")+path, &form)
	if err != nil {
		return fmt.Errorf("jira %s: %w", path, err)
	}
	req.SetBasicAuth(j.Email, j.Token)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Atlassian-Token", "no-check")
	return doUpload(j.Client, req, "jira "+path)
}

// Escapes what would start an image, macro or link; URLs stay clickable.
func jiraWikiWithImages(body string, names []string) string {
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
	for _, name := range names {
		text += "\n\n!" + name + "|thumbnail!"
	}
	return strings.TrimLeft(text, "\n")
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

func (l *Linear) CommentWithImages(ctx context.Context, ref, body string, images []string) error {
	pictures, err := readPictures(images)
	if err != nil {
		return err
	}
	text := strings.TrimRight(body, "\n")
	for _, p := range pictures {
		asset, err := l.upload(ctx, p)
		if err != nil {
			return err
		}
		text += "\n\n![" + p.name + "](" + asset + ")"
	}
	return l.Comment(ctx, ref, strings.TrimLeft(text, "\n"))
}

func (l *Linear) upload(ctx context.Context, p picture) (string, error) {
	document := fmt.Sprintf("mutation { fileUpload(contentType: %s, filename: %s, size: %d) { success uploadFile { uploadUrl assetUrl headers { key value } } } }",
		jsonString(p.contentType), jsonString(p.name), len(p.data))
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
	file := out.FileUpload.UploadFile
	if !out.FileUpload.Success || file == nil || file.UploadURL == "" {
		return "", fmt.Errorf("linear: the upload of %s was refused", p.name)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, file.UploadURL, bytes.NewReader(p.data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", p.contentType)
	req.Header.Set("Cache-Control", "public, max-age=31536000")
	for _, h := range file.Headers {
		req.Header.Set(h.Key, h.Value)
	}
	if err := doUpload(l.Client, req, "linear upload"); err != nil {
		return "", err
	}
	return file.AssetURL, nil
}

func doUpload(client *http.Client, req *http.Request, label string) error {
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
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
