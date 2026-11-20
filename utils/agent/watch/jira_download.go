package watch

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const (
	jiraCloudSuffix       = ".atlassian.net"
	MaxAttachmentDownload = 100 << 20
	maxDownloadRedirects  = 5
)

func sameSite(site, link *url.URL) bool {
	return link.Scheme == "https" && strings.EqualFold(link.Hostname(), site.Hostname()) && link.Port() == site.Port()
}

func (j *Jira) attachmentURL(link string) (*url.URL, *url.URL, error) {
	target, err := url.Parse(strings.TrimSpace(link))
	if err == nil && strings.TrimSpace(j.URL) == "" && target.Scheme == "https" && target.User == nil && target.Port() == "" && strings.HasSuffix(strings.ToLower(target.Hostname()), jiraCloudSuffix) {
		return target, target, nil
	}
	site, siteErr := url.Parse(strings.TrimSpace(j.URL))
	if siteErr != nil || site.Hostname() == "" {
		return nil, nil, fmt.Errorf("jira is not set up for this workspace: corgi agent watch auth jira")
	}
	if err != nil || target.User != nil || !sameSite(site, target) {
		return nil, nil, fmt.Errorf("that is not an https address on %s: corgi sends the Jira token only to your own Jira site", site.Hostname())
	}
	return site, target, nil
}

func (j *Jira) Download(ctx context.Context, link string, into io.Writer) (name string, size int64, err error) {
	if strings.TrimSpace(j.Email) == "" || strings.TrimSpace(j.Token) == "" {
		return "", 0, fmt.Errorf("jira is not set up for this workspace: corgi agent watch auth jira")
	}
	site, target, err := j.attachmentURL(link)
	if err != nil {
		return "", 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return "", 0, err
	}
	req.SetBasicAuth(j.Email, j.Token)
	client := &http.Client{Timeout: 5 * time.Minute}
	if j.Client != nil {
		copied := *j.Client
		client = &copied
	}
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= maxDownloadRedirects {
			return fmt.Errorf("too many redirects")
		}
		if next.URL.Scheme != "https" {
			return fmt.Errorf("the download moved to an address that is not https")
		}
		if !sameSite(site, next.URL) {
			next.Header.Del("Authorization")
		}
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("jira attachment: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("jira attachment: HTTP %d", resp.StatusCode)
	}
	size, err = io.Copy(into, io.LimitReader(resp.Body, MaxAttachmentDownload+1))
	if err != nil {
		return "", size, fmt.Errorf("jira attachment: %w", err)
	}
	if size > MaxAttachmentDownload {
		return "", size, fmt.Errorf("the attachment is over %d MB", MaxAttachmentDownload>>20)
	}
	return downloadName(resp, target), size, nil
}

func downloadName(resp *http.Response, target *url.URL) string {
	if _, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err == nil {
		if name := path.Base(strings.ReplaceAll(params["filename"], "\\", "/")); name != "" && name != "." && name != "/" {
			return name
		}
	}
	if name := path.Base(target.Path); name != "" && name != "." && name != "/" {
		return name
	}
	return "attachment"
}
