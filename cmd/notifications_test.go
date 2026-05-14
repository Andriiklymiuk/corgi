package cmd

import (
	"andriiklymiuk/corgi/utils"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	var buf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&buf, r)
	}()
	fn()
	w.Close()
	os.Stdout = orig
	wg.Wait()
	return buf.String()
}

func withTempHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	utils.ResetNotifyCache()
	t.Cleanup(utils.ResetNotifyCache)
	return dir
}

func TestShowNotificationsStatus_Off(t *testing.T) {
	withTempHome(t)
	out := captureStdout(t, showNotificationsStatus)
	if !strings.Contains(out, "notifications: off") {
		t.Errorf("expected 'notifications: off' in output, got: %q", out)
	}
}

func TestShowNotificationsStatus_On(t *testing.T) {
	withTempHome(t)
	if err := utils.SaveUserConfig(&utils.UserConfig{Notifications: true}); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, showNotificationsStatus)
	if !strings.Contains(out, "notifications: on") {
		t.Errorf("expected 'notifications: on' in output, got: %q", out)
	}
}

func TestWriteNotificationsPref_PersistsAndShowsPath(t *testing.T) {
	dir := withTempHome(t)
	out := captureStdout(t, func() { writeNotificationsPref(true) })

	want := filepath.Join(dir, ".corgi", "config.yml")
	if !strings.Contains(out, want) {
		t.Errorf("expected config path %q in output, got: %q", want, out)
	}
	if !strings.Contains(out, "Notifications on") {
		t.Errorf("expected 'Notifications on' in output, got: %q", out)
	}
	cfg, err := utils.LoadUserConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Notifications {
		t.Error("expected persisted Notifications=true")
	}
}

func TestWriteNotificationsPref_Toggle(t *testing.T) {
	withTempHome(t)
	captureStdout(t, func() { writeNotificationsPref(true) })
	captureStdout(t, func() { writeNotificationsPref(false) })
	cfg, _ := utils.LoadUserConfig()
	if cfg.Notifications {
		t.Error("expected Notifications=false after toggle")
	}
}

func TestRunNotifications_NoArgsPrintsStatus(t *testing.T) {
	withTempHome(t)
	out := captureStdout(t, func() { runNotifications(&cobra.Command{}, nil) })
	if !strings.Contains(out, "notifications:") {
		t.Errorf("expected status line, got: %q", out)
	}
}

func TestRunNotifications_OnEnables(t *testing.T) {
	withTempHome(t)
	captureStdout(t, func() { runNotifications(&cobra.Command{}, []string{"on"}) })
	cfg, _ := utils.LoadUserConfig()
	if !cfg.Notifications {
		t.Error("expected Notifications=true after `on`")
	}
}

func TestRunNotifications_OffDisables(t *testing.T) {
	withTempHome(t)
	captureStdout(t, func() { runNotifications(&cobra.Command{}, []string{"on"}) })
	captureStdout(t, func() { runNotifications(&cobra.Command{}, []string{"off"}) })
	cfg, _ := utils.LoadUserConfig()
	if cfg.Notifications {
		t.Error("expected Notifications=false after `off`")
	}
}

func TestRunNotifications_TestActionDispatches(t *testing.T) {
	withTempHome(t)
	out := captureStdout(t, func() { runNotifications(&cobra.Command{}, []string{"test"}) })
	if !strings.Contains(out, "Test notification") {
		t.Errorf("expected 'Test notification' in output, got: %q", out)
	}
}

func TestRunNotifications_CaseInsensitiveOn(t *testing.T) {
	withTempHome(t)
	captureStdout(t, func() { runNotifications(&cobra.Command{}, []string{"ON"}) })
	cfg, _ := utils.LoadUserConfig()
	if !cfg.Notifications {
		t.Error("expected `ON` (uppercase) to enable")
	}
}
