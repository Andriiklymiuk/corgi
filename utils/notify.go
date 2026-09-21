package utils

import (
	_ "embed"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

//go:embed notify_icon.png
var notifyIconPNG []byte

var notifyIconOnce sync.Once
var notifyIconFile string

func notifyIconPath() string {
	notifyIconOnce.Do(func() {
		base, err := os.UserCacheDir()
		if err != nil {
			return
		}
		dir := filepath.Join(base, "corgi")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return
		}
		path := filepath.Join(dir, "notify-icon.png")
		if err := os.WriteFile(path, notifyIconPNG, 0o600); err == nil {
			notifyIconFile = path
		}
	})
	return notifyIconFile
}

var notifyThrottleWindow = 30 * time.Second

var (
	notifyConfigOnce    sync.Once
	notifyConfigEnabled atomic.Bool

	notifyThrottleMu sync.Mutex
	notifyLastSent   = map[string]time.Time{}
)

func loadNotifyEnabled() bool {
	notifyConfigOnce.Do(func() {
		cfg, err := LoadUserConfig()
		notifyConfigEnabled.Store(err == nil && cfg.Notifications)
	})
	return notifyConfigEnabled.Load()
}

func ResetNotifyCache() {
	notifyConfigOnce = sync.Once{}
	notifyConfigEnabled.Store(false)
}

func Notify(title, body string) {
	if !loadNotifyEnabled() {
		return
	}
	if !claimNotifyToken(title + "\x00" + body) {
		return
	}
	sendNotification(title, body)
}

func claimNotifyToken(key string) bool {
	notifyThrottleMu.Lock()
	defer notifyThrottleMu.Unlock()
	now := time.Now()
	if last, ok := notifyLastSent[key]; ok && now.Sub(last) < notifyThrottleWindow {
		return false
	}
	notifyLastSent[key] = now
	if len(notifyLastSent) > 1024 {
		for k, t := range notifyLastSent {
			if now.Sub(t) > notifyThrottleWindow {
				delete(notifyLastSent, k)
			}
		}
	}
	return true
}

func ResetNotifyThrottleForTests() {
	notifyThrottleMu.Lock()
	defer notifyThrottleMu.Unlock()
	notifyLastSent = map[string]time.Time{}
}

func NotifyRaw(title, body string) {
	sendNotification(title, body)
}

func NotifyWithLink(title, body, link string) {
	notifyLink = link
	defer func() { notifyLink = "" }()
	Notify(title, body)
}

func NotifyWithCommand(title, body string, argv []string, link string) {
	notifyCommand = argv
	notifyLink = link
	defer func() { notifyCommand = nil; notifyLink = "" }()
	Notify(title, body)
}

var notifyLink string
var notifyCommand []string

var sendNotificationOverride func(title, body string)

var runNotifyCommand = func(cmd *exec.Cmd) error { return cmd.Run() }

func SilenceNotifyDispatchForTests(t interface{ Cleanup(func()) }) {
	original := runNotifyCommand
	runNotifyCommand = func(*exec.Cmd) error { return nil }
	t.Cleanup(func() { runNotifyCommand = original })
}

func SilenceNotificationsForTests() {
	sendNotificationOverride = func(string, string) {
	}
}

func sendNotification(title, body string) {
	if sendNotificationOverride != nil {
		sendNotificationOverride(title, body)
		return
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		if path, err := exec.LookPath("terminal-notifier"); err == nil {
			cmd = exec.Command(path, terminalNotifierArgs(title, body, notifyIconPath(), notifyLink, notifyCommand)...)
			_ = runNotifyCommand(cmd)
			return
		}
		script := fmt.Sprintf(
			`display notification %q with title %q`,
			body, title,
		)
		cmd = exec.Command("osascript", "-e", script)
	case "linux":
		cmd = exec.Command("notify-send", notifySendArgs(title, body, notifyIconPath())...)
	case "windows":
		ps := fmt.Sprintf(
			`[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null; `+
				`$template = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02); `+
				`$template.SelectSingleNode('//text[@id=1]').InnerText = %s; `+
				`$template.SelectSingleNode('//text[@id=2]').InnerText = %s; `+
				`$notif = [Windows.UI.Notifications.ToastNotification]::new($template); `+
				`[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('corgi').Show($notif)`,
			powershellQuote(title), powershellQuote(body),
		)
		cmd = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps)
	default:
		return
	}
	_ = runNotifyCommand(cmd)
}

func IsNotificationsEnabled() bool {
	cfg, err := LoadUserConfig()
	if err != nil {
		return false
	}
	return cfg.Notifications
}

func powershellQuote(s string) string {
	escaped := strings.ReplaceAll(s, "'", "''")
	return fmt.Sprintf("'%s'", escaped)
}

func terminalNotifierArgs(title, body, icon, link string, command []string) []string {
	args := []string{
		"-title", title,
		"-message", body,
		"-group", "com.andriiklymiuk.corgi",
	}
	if icon != "" {
		args = append(args, "-appIcon", icon, "-contentImage", icon)
	}
	switch {
	case len(command) > 0:
		quoted := make([]string, len(command))
		for i, word := range command {
			quoted[i] = shellQuote(word)
		}
		args = append(args, "-execute", strings.Join(quoted, " "))
	case safeNotifyLink(link) != "":
		args = append(args, "-open", safeNotifyLink(link))
	}
	return args
}

func shellQuote(word string) string {
	if word == "" {
		return "''"
	}
	safe := true
	for _, r := range word {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/._-+:@%", r)) {
			safe = false
			break
		}
	}
	if safe {
		return word
	}
	return "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
}

func safeNotifyLink(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	u, err := url.Parse(trimmed)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return u.String()
}

func notifySendArgs(title, body, icon string) []string {
	args := []string{"--app-name=corgi"}
	if icon != "" {
		args = append(args, "--icon="+icon)
	}
	return append(args, title, body)
}
