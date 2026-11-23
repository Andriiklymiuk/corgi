package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/sendgate"
)

// notifyGate meters every webhook notification, shared with the Slack poster's
// ledger so no loop upstream can flood the channel behind the URL.
var notifyGate = func() *sendgate.Gate { return sendgate.For(agentDirOrEmpty()) }

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

func connectorURL() string {
	launcher := launcherURL()
	if launcher == "" {
		return ""
	}
	return strings.TrimSuffix(launcher, "/app") + "/mcp"
}

func localLauncherURL() string {
	dir, err := agentDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(dir, mcpAddrName))
	if err != nil {
		return ""
	}
	addr := loopbackAddr(strings.TrimSpace(string(data)))
	if addr == "" || !mcpListening(addr) {
		return ""
	}
	return utils.PlainHTTPURL(addr, "/app")
}

func loopbackAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return ""
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		return net.JoinHostPort("127.0.0.1", port)
	}
	return net.JoinHostPort(host, port)
}

func focusCommand(sessionID string) []string {
	self, err := os.Executable()
	if err != nil {
		self = "corgi"
	}
	return []string{self, "agent", "focus", sessionID}
}

func preferLocalLink(link string) string {
	public := strings.TrimSuffix(launcherURL(), "/app")
	if link == "" || public == "" {
		return link
	}
	path, ours := strings.CutPrefix(link, public)
	if !ours || (path != "/app" && !strings.HasPrefix(path, runPagePath)) {
		return link
	}
	if local := localLauncherURL(); local != "" {
		return strings.TrimSuffix(local, "/app") + path
	}
	return link
}

func awayLink(link string) string {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	if ip := net.ParseIP(u.Hostname()); strings.EqualFold(u.Hostname(), "localhost") || (ip != nil && ip.IsLoopback()) {
		return ""
	}
	return u.String()
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
	send := webhookRetractableNotifier(rawURL, client)
	if send == nil {
		return nil
	}
	return func(title, body, link string) { send(title, body, link) }
}

func webhookRetractableNotifier(rawURL string, client *http.Client) func(title, body, link string) func() {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	target := u.String()
	shape := webhookShapeFor(u.Host)
	gate := notifyGate()
	// the URL is the secret for most webhooks, so the ledger keeps a hash of it
	sum := sha256.Sum256([]byte(target))
	family, dest := "webhook", shape+":"+hex.EncodeToString(sum[:6])
	return func(title, body, link string) func() {
		if err := gate.Allow(family, dest, title+"\n"+body+"\n"+link); err != nil {
			utils.Infof("agent: notification not sent (%v): %s\n", err, title)
			return nil
		}
		posted := make(chan int64, 1)
		go func() {
			var id int64
			defer func() { posted <- id }()
			req, err := buildNotifyRequest(shape, target, title, body, link)
			if err != nil {
				return
			}
			resp, err := client.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusTooManyRequests {
				secs, _ := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After")))
				gate.Pause(family, time.Duration(secs)*time.Second)
			}
			if shape == webhookTelegram {
				id = telegramMessageID(resp.Body)
			}
		}()
		if shape != webhookTelegram {
			return nil
		}
		return func() {
			if id := <-posted; id != 0 {
				deleteTelegramMessage(client, target, id)
			}
			posted <- 0
		}
	}
}

func telegramMessageID(body io.Reader) int64 {
	var sent struct {
		Result struct {
			MessageID int64 `json:"message_id"`
		} `json:"result"`
	}
	_ = json.NewDecoder(io.LimitReader(body, 64<<10)).Decode(&sent)
	return sent.Result.MessageID
}

func deleteTelegramMessage(client *http.Client, target string, id int64) {
	u, err := url.Parse(target)
	if err != nil || !strings.HasSuffix(u.Path, "/sendMessage") {
		return
	}
	chat := telegramChatID(target)
	u.Path = strings.TrimSuffix(u.Path, "sendMessage") + "deleteMessage"
	u.RawQuery = ""
	payload, _ := json.Marshal(map[string]any{"chat_id": chat, "message_id": id})
	resp, err := client.Post(u.String(), "application/json", bytes.NewReader(payload))
	if err != nil {
		return
	}
	_ = resp.Body.Close()
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
	if link = awayLink(link); link != "" && strings.Contains(body, link) {
		link = ""
	}
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
