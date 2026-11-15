package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/sessions"
)

type screenCalls struct {
	mu    sync.Mutex
	calls [][]string
	text  string
}

func (c *screenCalls) install(t *testing.T) {
	t.Helper()
	prevRun, prevTTY := runScreenCommand, sessionTTYName
	runScreenCommand = func(_ context.Context, name string, args ...string) (string, error) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.calls = append(c.calls, append([]string{name}, args...))
		return c.text, nil
	}
	sessionTTYName = func(dev uint64) string {
		if dev == 0 {
			return ""
		}
		return "/dev/ttys007"
	}
	screenCacheMu.Lock()
	screenCache = map[string]screenCacheEntry{}
	screenCacheMu.Unlock()
	t.Cleanup(func() { runScreenCommand, sessionTTYName = prevRun, prevTTY })
}

func (c *screenCalls) all() [][]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([][]string(nil), c.calls...)
}

func boardWithTerminals(t *testing.T) string {
	t.Helper()
	data := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", data)
	dir := filepath.Join(data, "agent")
	writeBoardState(t, dir, sessions.State{Sessions: []sessions.Session{
		{ID: "s-tmux", Label: "api", Status: sessions.StatusNeedsInput, Host: sessions.Host{Kind: sessions.HostTmux, Pane: "%3"}},
		{ID: "s-iterm", Label: "web", Status: sessions.StatusWorking, TTY: 7, Host: sessions.Host{Kind: sessions.HostITerm}},
		{ID: "s-term", Label: "docs", Status: sessions.StatusWorking, TTY: 9, Host: sessions.Host{Kind: sessions.HostTerminalApp}},
		{ID: "s-code", Label: "mobile", Status: sessions.StatusWorking, Host: sessions.Host{Kind: sessions.HostVSCodeTerminal}},
		{ID: "s-vs", Label: "ios", Status: sessions.StatusNeedsInput, Host: sessions.Host{Kind: sessions.HostVSCodeTerminal, ShellPID: 777}},
	}})
	return dir
}

func callScreenRoute(t *testing.T, h http.HandlerFunc, method, path, body string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec.Code, rec.Body.String()
}

func TestSessionScreenReadsEachTerminalKind(t *testing.T) {
	boardWithTerminals(t)
	c := &screenCalls{text: "\x1b[1m❯\x1b[0m Allow edit? 1. Yes 2. No\n"}
	c.install(t)
	code, body := callScreenRoute(t, launchSessionScreenHandler, "GET", "/launch/session/screen?session=s-tmux", "")
	if code != 200 || !strings.Contains(body, `"ansi":true`) || !strings.Contains(body, `"source":"tmux"`) {
		t.Fatalf("tmux: %d %s", code, body)
	}
	if got := strings.Join(c.all()[0], " "); got != "tmux capture-pane -p -e -t %3" {
		t.Fatalf("tmux call: %s", got)
	}
	code, body = callScreenRoute(t, launchSessionScreenHandler, "GET", "/launch/session/screen?session=s-iterm", "")
	if code != 200 || !strings.Contains(body, `"source":"iterm"`) || !strings.Contains(strings.Join(c.all()[1], " "), `tty of s is "/dev/ttys007"`) {
		t.Fatalf("iterm: %d %s", code, body)
	}
	code, body = callScreenRoute(t, launchSessionScreenHandler, "GET", "/launch/session/screen?session=s-term", "")
	if code != 200 || !strings.Contains(body, `"keys":false`) {
		t.Fatalf("terminal: %d %s", code, body)
	}
	if code, body = callScreenRoute(t, launchSessionScreenHandler, "GET", "/launch/session/screen?session=s-code", ""); code != 409 || !strings.Contains(body, "VS Code") {
		t.Fatalf("vscode: %d %s", code, body)
	}
	if code, _ = callScreenRoute(t, launchSessionScreenHandler, "GET", "/launch/session/screen?session=nope", ""); code == 200 {
		t.Fatal("unknown session")
	}
}

func TestSessionScreenIsCachedBriefly(t *testing.T) {
	boardWithTerminals(t)
	c := &screenCalls{text: "same"}
	c.install(t)
	s := sessions.Session{ID: "s-tmux", Host: sessions.Host{Kind: sessions.HostTmux, Pane: "%3"}}
	now := time.Now()
	for i := 0; i < 5; i++ {
		_, _ = readSessionScreen(context.Background(), s, now.Add(time.Duration(i)*50*time.Millisecond))
	}
	if n := len(c.all()); n != 1 {
		t.Fatalf("five reads in 250 ms ran tmux %d times", n)
	}
	_, _ = readSessionScreen(context.Background(), s, now.Add(time.Second))
	if n := len(c.all()); n != 2 {
		t.Fatalf("a read after the cache ran %d", n)
	}
}

func TestSessionKeysReachTmuxAndITerm(t *testing.T) {
	boardWithTerminals(t)
	c := &screenCalls{}
	c.install(t)
	code, body := callScreenRoute(t, launchSessionKeysHandler, "POST", "/launch/session/keys", `{"session":"s-tmux","keys":["Down","Enter"],"text":"yes ✓\n"}`)
	if code != 200 {
		t.Fatalf("tmux keys: %d %s", code, body)
	}
	got := strings.Join(c.all()[0], "|")
	if got != "tmux|send-keys|-t|%3|Down|;|send-keys|-t|%3|Enter|;|send-keys|-t|%3|-l|yes ✓|;|send-keys|-t|%3|Enter" {
		t.Fatalf("tmux args: %s", got)
	}
	if code, body = callScreenRoute(t, launchSessionKeysHandler, "POST", "/launch/session/keys", `{"session":"s-iterm","keys":["Escape","1","S-Tab"]}`); code != 200 {
		t.Fatalf("iterm keys: %d %s", code, body)
	}
	script := c.all()[1][2]
	if !strings.Contains(script, `(ASCII character 27) & "1" & (ASCII character 27) & "[Z"`) || !strings.Contains(script, "newline no") {
		t.Fatalf("iterm script: %s", script)
	}
	if code, _ = callScreenRoute(t, launchSessionKeysHandler, "POST", "/launch/session/keys", `{"session":"s-term","keys":["Up"]}`); code != 409 {
		t.Fatal("Terminal.app takes text from Send, not keys")
	}
	if code, body = callScreenRoute(t, launchSessionKeysHandler, "POST", "/launch/session/keys", `{"session":"s-tmux","keys":["rm -rf"]}`); code != 409 || !strings.Contains(body, "unknown key") {
		t.Fatalf("a word is not a key: %d %s", code, body)
	}
	if code, _ = callScreenRoute(t, launchSessionKeysHandler, "POST", "/launch/session/keys", `{"session":"s-tmux"}`); code != 400 {
		t.Fatal("nothing to send")
	}
}

func TestTmuxKeyArgsGroupLetters(t *testing.T) {
	got := strings.Join(tmuxKeysArgs("%1", []string{"g", "i", "t", "S-Tab", "C-c"}), " ")
	if got != "send-keys -t %1 -l git ; send-keys -t %1 BTab ; send-keys -t %1 C-c" {
		t.Fatal(got)
	}
}

func TestVSCodeSessionsShowThroughTheExtension(t *testing.T) {
	dir := boardWithTerminals(t)
	c := &screenCalls{}
	c.install(t)
	mirror := filepath.Join(dir, "mirror")
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(20 * time.Millisecond):
			}
			if want, err := os.ReadFile(filepath.Join(mirror, "777.want")); err == nil && strings.TrimSpace(string(want)) != "" {
				_ = os.WriteFile(filepath.Join(mirror, "777.ansi"), []byte("\x1b[33mDo you want to proceed?\x1b[0m\r\n❯ 1. Yes"), 0o600)
				return
			}
		}
	}()
	code, body := callScreenRoute(t, launchSessionScreenHandler, "GET", "/launch/session/screen?session=s-vs", "")
	if code != 200 || !strings.Contains(body, "proceed?") || !strings.Contains(body, `"source":"vscode"`) || !strings.Contains(body, `"keys":true`) {
		t.Fatalf("vscode: %d %s", code, body)
	}
	if code, body = callScreenRoute(t, launchSessionKeysHandler, "POST", "/launch/session/keys", `{"session":"s-vs","keys":["Down","Enter"]}`); code != 200 {
		t.Fatalf("keys: %d %s", code, body)
	}
	matches, _ := filepath.Glob(filepath.Join(mirror, "777.*.keys"))
	if len(matches) != 1 {
		t.Fatalf("keys files: %v", matches)
	}
	if raw, _ := os.ReadFile(matches[0]); string(raw) != "\x1b[B\r" {
		t.Fatalf("raw keys %q", raw)
	}
}

func TestVSCodeWithoutTheExtensionSaysSo(t *testing.T) {
	boardWithTerminals(t)
	(&screenCalls{}).install(t)
	prev := mirrorWait
	mirrorWait = 100 * time.Millisecond
	defer func() { mirrorWait = prev }()
	code, body := callScreenRoute(t, launchSessionScreenHandler, "GET", "/launch/session/screen?session=s-vs", "")
	if code != 409 || !strings.Contains(body, "VS Code extension 2.35") {
		t.Fatalf("%d %s", code, body)
	}
}
