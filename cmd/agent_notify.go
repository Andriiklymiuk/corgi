package cmd

import (
	"net/http"
	"net/url"
	"strings"
	"time"
)

func webhookNotifier(rawURL string, client *http.Client) func(title, body string) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	target := u.String()
	return func(title, body string) {
		go func() {
			req, err := http.NewRequest(http.MethodPost, target, strings.NewReader(body))
			if err != nil {
				return
			}
			// ntfy shows raw bytes for non-ASCII header values, so keep it ASCII.
			req.Header.Set("Title", asciiHeader(title))
			req.Header.Set("Content-Type", "text/plain; charset=utf-8")
			resp, err := client.Do(req)
			if err != nil {
				return
			}
			_ = resp.Body.Close()
		}()
	}
}

func asciiHeader(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '·':
			b.WriteByte('-')
		case r >= 32 && r < 127:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

func combinedNotifier(notifiers ...func(title, body string)) func(title, body string) {
	var active []func(title, body string)
	for _, n := range notifiers {
		if n != nil {
			active = append(active, n)
		}
	}
	if len(active) == 0 {
		return nil
	}
	return func(title, body string) {
		for _, n := range active {
			n(title, body)
		}
	}
}
