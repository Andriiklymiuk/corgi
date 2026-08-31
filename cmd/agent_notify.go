package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Written by the MCP server, read by the daemon — two processes, so a file.
const publicURLName = "public.url"

func recordPublicURL(url string) {
	dir, err := agentDir()
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, publicURLName), []byte(strings.TrimSpace(url)+"\n"), 0o600)
}

func launcherURL() string {
	dir, err := agentDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(dir, publicURLName))
	if err != nil {
		return ""
	}
	u, err := url.Parse(strings.TrimSpace(string(data)))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return strings.TrimSuffix(u.String(), "/") + "/app"
}

func webhookNotifier(rawURL string, client *http.Client) func(title, body string) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	linked := webhookLinkNotifier(rawURL, client)
	if linked == nil {
		return nil
	}
	return func(title, body string) {
		linked(title, body, launcherURL())
	}
}

func webhookLinkNotifier(rawURL string, client *http.Client) func(title, body, link string) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	target := u.String()
	shape := webhookShapeFor(u.Host)
	return func(title, body, link string) {
		go func() {
			req, err := buildNotifyRequest(shape, target, title, body, link)
			if err != nil {
				return
			}
			resp, err := client.Do(req)
			if err != nil {
				return
			}
			_ = resp.Body.Close()
		}()
	}
}

const (
	webhookNtfy     = "ntfy"
	webhookDiscord  = "discord"
	webhookSlack    = "slack"
	webhookTelegram = "telegram"
)

func webhookShapeFor(host string) string {
	host = strings.ToLower(host)
	switch {
	case strings.HasSuffix(host, "discord.com") || strings.HasSuffix(host, "discordapp.com"):
		return webhookDiscord
	case strings.HasSuffix(host, "slack.com"):
		return webhookSlack
	case strings.HasSuffix(host, "api.telegram.org"):
		return webhookTelegram
	}
	return webhookNtfy
}

func buildNotifyRequest(shape, target, title, body, link string) (*http.Request, error) {
	if shape == webhookNtfy {
		req, err := http.NewRequest(http.MethodPost, target, strings.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Title", asciiHeader(title))
		req.Header.Set("Content-Type", "text/plain; charset=utf-8")
		if link != "" {
			req.Header.Set("Click", link)
		}
		return req, nil
	}

	text := title + "\n" + body
	if link != "" {
		text += "\n" + link
	}
	payload := map[string]any{}
	switch shape {
	case webhookDiscord:
		payload["content"] = text
	case webhookSlack:
		payload["text"] = text
	case webhookTelegram:
		payload["text"] = text
		payload["disable_web_page_preview"] = true
		if chat := telegramChatID(target); chat != "" {
			payload["chat_id"] = chat
		}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
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

func telegramChatID(target string) string {
	u, err := url.Parse(target)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(u.Query().Get("chat_id"))
}
