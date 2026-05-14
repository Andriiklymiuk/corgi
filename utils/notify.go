package utils

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// notifyThrottleWindow is the dedupe gap between two notifications with
// the same title+body — stops crash-loop services from spamming toasts.
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

// ResetNotifyCache forces the next Notify to re-read ~/.corgi/config.yml.
// Call after SaveUserConfig if an in-process toggle should take effect now.
func ResetNotifyCache() {
	notifyConfigOnce = sync.Once{}
	notifyConfigEnabled.Store(false)
}

// Notify sends a desktop notification if the user opted in via doctor.
// Same-message notifications inside notifyThrottleWindow are dropped so
// a crash-looping service can't spam the desktop. Fails silently.
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
	// Sweep expired entries at high-water mark so the map stays bounded.
	if len(notifyLastSent) > 1024 {
		for k, t := range notifyLastSent {
			if now.Sub(t) > notifyThrottleWindow {
				delete(notifyLastSent, k)
			}
		}
	}
	return true
}

// ResetNotifyThrottleForTests clears the dedupe map.
func ResetNotifyThrottleForTests() {
	notifyThrottleMu.Lock()
	defer notifyThrottleMu.Unlock()
	notifyLastSent = map[string]time.Time{}
}

// NotifyRaw sends a notification without checking the opt-in flag. Used
// by doctor and `corgi config notifications test` to fire one regardless.
func NotifyRaw(title, body string) {
	sendNotification(title, body)
}

func sendNotification(title, body string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		script := fmt.Sprintf(
			`display notification %q with title %q`,
			body, title,
		)
		cmd = exec.Command("osascript", "-e", script)
	case "linux":
		cmd = exec.Command("notify-send", title, body)
	case "windows":
		// PowerShell toast via Windows Runtime APIs (works on Win 10+).
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
	_ = cmd.Run() // best-effort; notification failure is never fatal
}

// IsNotificationsEnabled returns true when the user has opted into notifications.
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
