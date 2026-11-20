package watch

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJiraDownloadSendsTheTokenOnlyToItsOwnSite(t *testing.T) {
	var elsewhereAuth, siteAuth string
	elsewhere := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhereAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Disposition", `attachment; filename="../../shot.png"`)
		_, _ = w.Write([]byte("picture"))
	}))
	defer elsewhere.Close()
	site := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		siteAuth = r.Header.Get("Authorization")
		http.Redirect(w, r, elsewhere.URL+"/signed/shot.png", http.StatusFound)
	}))
	defer site.Close()

	jira := &Jira{URL: site.URL, Email: "me@example.com", Token: "secret-token", Client: site.Client()}
	var got bytes.Buffer
	name, size, err := jira.Download(context.Background(), site.URL+"/rest/api/3/attachment/content/1", &got)
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "picture" || size != 7 || name != "shot.png" {
		t.Fatalf("got %q, size %d, name %q", got.String(), size, name)
	}
	if siteAuth == "" {
		t.Fatal("the Jira site was not given the credentials")
	}
	if elsewhereAuth != "" {
		t.Fatalf("the token went to another host: %q", elsewhereAuth)
	}
}

func TestJiraDownloadRefusesAnotherHost(t *testing.T) {
	jira := &Jira{URL: "https://acme.atlassian.net", Email: "me@example.com", Token: "t"}
	for _, link := range []string{
		"https://evil.example.com/rest/api/3/attachment/content/1",
		"http://acme.atlassian.net/rest/api/3/attachment/content/1",
		"https://acme.atlassian.net.evil.example.com/x",
		"https://user:pass@acme.atlassian.net/x",
		"https://acme.atlassian.net:8443/x",
		"file:///etc/passwd",
		"",
	} {
		if _, _, err := jira.Download(context.Background(), link, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "acme.atlassian.net") {
			t.Errorf("%q: err = %v", link, err)
		}
	}
	if _, _, err := (&Jira{URL: "https://acme.atlassian.net"}).Download(context.Background(), "https://acme.atlassian.net/x", &bytes.Buffer{}); err == nil {
		t.Fatal("a download with no credentials went out")
	}
}

func TestJiraDownloadWithNoSiteSetTrustsOnlyJiraCloud(t *testing.T) {
	jira := &Jira{Email: "me@example.com", Token: "t"}
	if _, target, err := jira.attachmentURL("https://acme.atlassian.net/rest/api/3/attachment/content/1"); err != nil || target.Hostname() != "acme.atlassian.net" {
		t.Fatalf("a Jira Cloud address was refused: %v", err)
	}
	for _, link := range []string{"https://evil.example.com/x", "https://atlassian.net.evil.example.com/x", "http://acme.atlassian.net/x", "https://acme.atlassian.net:8443/x", "https://u:p@acme.atlassian.net/x"} {
		if _, _, err := jira.attachmentURL(link); err == nil {
			t.Errorf("%q was trusted with no site set", link)
		}
	}
}
