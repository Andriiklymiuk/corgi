package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"andriiklymiuk/corgi/utils/agent/proc"
	"andriiklymiuk/corgi/utils/agent/sessions"
)

type sessionScreen struct {
	Text   string `json:"text"`
	ANSI   bool   `json:"ansi"`
	Source string `json:"source"`
	Keys   bool   `json:"keys"`
}

var sessionTTYName = proc.TTYName

var errNoScreen = errors.New("this session's terminal cannot be read: it runs in VS Code or has no tab corgi knows; open it on the board instead")

var runScreenCommand = func(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).Output()
	return string(out), err
}

type screenCacheEntry struct {
	at  time.Time
	out sessionScreen
	err error
}

var (
	screenCacheMu sync.Mutex
	screenCache   = map[string]screenCacheEntry{}
)

const screenCacheFor = 400 * time.Millisecond

func readSessionScreen(ctx context.Context, s sessions.Session, now time.Time) (sessionScreen, error) {
	screenCacheMu.Lock()
	if e, ok := screenCache[s.ID]; ok && now.Sub(e.at) < screenCacheFor {
		screenCacheMu.Unlock()
		return e.out, e.err
	}
	screenCacheMu.Unlock()
	out, err := readSessionScreenNow(ctx, s)
	screenCacheMu.Lock()
	screenCache[s.ID] = screenCacheEntry{at: now, out: out, err: err}
	for id, e := range screenCache {
		if now.Sub(e.at) > time.Minute {
			delete(screenCache, id)
		}
	}
	screenCacheMu.Unlock()
	return out, err
}

func readSessionScreenNow(ctx context.Context, s sessions.Session) (sessionScreen, error) {
	switch s.Host.Kind {
	case sessions.HostTmux:
		if s.Host.Pane == "" {
			return sessionScreen{}, errNoScreen
		}
		text, err := runScreenCommand(ctx, "tmux", "capture-pane", "-p", "-e", "-t", s.Host.Pane)
		return sessionScreen{Text: text, ANSI: true, Source: "tmux", Keys: true}, err
	case sessions.HostITerm:
		tty := sessionTTYName(s.TTY)
		if tty == "" {
			return sessionScreen{}, errNoScreen
		}
		text, err := runScreenCommand(ctx, "osascript", "-e", itermContentsScript(tty))
		return sessionScreen{Text: strings.TrimRight(text, "\n"), Source: "iterm", Keys: true}, err
	case sessions.HostTerminalApp:
		tty := sessionTTYName(s.TTY)
		if tty == "" {
			return sessionScreen{}, errNoScreen
		}
		text, err := runScreenCommand(ctx, "osascript", "-e", terminalAppContentsScript(tty))
		return sessionScreen{Text: strings.TrimRight(text, "\n"), Source: "terminal", Keys: false}, err
	case sessions.HostVSCodeTerminal:
		return readMirroredScreen(ctx, s)
	}
	return sessionScreen{}, errNoScreen
}

var (
	mirrorWait  = 900 * time.Millisecond
	mirrorFresh = 4 * time.Second
)

var errNoMirror = errors.New("VS Code terminals show here with the corgi VS Code extension 2.35 or newer: update it, then start the session again")

func mirrorDir() (string, error) {
	dir, err := agentDir()
	if err != nil {
		return "", err
	}
	m := filepath.Join(dir, "mirror")
	return m, os.MkdirAll(m, 0o700)
}

func readMirroredScreen(ctx context.Context, s sessions.Session) (sessionScreen, error) {
	if s.Host.ShellPID == 0 {
		return sessionScreen{}, errNoScreen
	}
	dir, err := mirrorDir()
	if err != nil {
		return sessionScreen{}, err
	}
	cols, rows := 120, 40
	if tty := sessionTTYName(s.TTY); tty != "" {
		if c, r, ok := ttySizeOf(tty); ok {
			cols, rows = c, r
		}
	}
	pid := strconv.Itoa(s.Host.ShellPID)
	if err := os.WriteFile(filepath.Join(dir, pid+".want"), []byte(strconv.Itoa(cols)+" "+strconv.Itoa(rows)), 0o600); err != nil {
		return sessionScreen{}, err
	}
	ansi := filepath.Join(dir, pid+".ansi")
	deadline := time.Now().Add(mirrorWait)
	for {
		if st, err := os.Stat(ansi); err == nil && time.Since(st.ModTime()) < mirrorFresh {
			data, err := os.ReadFile(ansi)
			return sessionScreen{Text: string(data), ANSI: true, Source: "vscode", Keys: true}, err
		}
		if time.Now().After(deadline) {
			return sessionScreen{}, errNoMirror
		}
		select {
		case <-ctx.Done():
			return sessionScreen{}, ctx.Err()
		case <-time.After(40 * time.Millisecond):
		}
	}
}

func keysToBytes(keys []string) string {
	var b strings.Builder
	for _, k := range keys {
		if v, ok := terminalKeyBytes[k]; ok {
			b.WriteString(v)
		} else {
			b.WriteString(k)
		}
	}
	return b.String()
}

func sendMirroredKeys(s sessions.Session, keys []string) error {
	if s.Host.ShellPID == 0 {
		return errNoScreen
	}
	dir, err := mirrorDir()
	if err != nil {
		return err
	}
	name := fmt.Sprintf("%d.%019d.keys", s.Host.ShellPID, time.Now().UnixNano())
	tmp := filepath.Join(dir, name+".tmp")
	if err := os.WriteFile(tmp, []byte(keysToBytes(keys)), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, name))
}

func itermContentsScript(tty string) string {
	return `tell application "iTerm2"
	repeat with w in windows
		repeat with t in tabs of w
			repeat with s in sessions of t
				if tty of s is "` + tty + `" then return contents of s
			end repeat
		end repeat
	end repeat
end tell
return ""`
}

func terminalAppContentsScript(tty string) string {
	return `tell application "Terminal"
	repeat with w in windows
		repeat with t in tabs of w
			if tty of t is "` + tty + `" then return contents of t
		end repeat
	end repeat
end tell
return ""`
}

var terminalKeyBytes = map[string]string{
	"Enter": "\r", "Escape": "\x1b", "Tab": "\t", "BSpace": "\x7f",
	"Up": "\x1b[A", "Down": "\x1b[B", "Right": "\x1b[C", "Left": "\x1b[D",
	"C-c": "\x03", "C-d": "\x04", "C-l": "\x0c", "C-r": "\x12", "C-u": "\x15", "C-z": "\x1a",
	"S-Tab": "\x1b[Z",
}

func validTerminalKey(k string) bool {
	if _, ok := terminalKeyBytes[k]; ok {
		return true
	}
	return isTypedChar(k)
}

func isTypedChar(k string) bool {
	if utf8.RuneCountInString(k) != 1 {
		return false
	}
	r, _ := utf8.DecodeRuneInString(k)
	return r >= ' ' && r != 0x7f
}

var tmuxKeyNames = map[string]string{"S-Tab": "BTab"}

func tmuxKeysArgs(pane string, keys []string) []string {
	var args []string
	var lit strings.Builder
	add := func(more ...string) {
		if len(args) > 0 {
			args = append(args, ";")
		}
		args = append(args, append([]string{"send-keys", "-t", pane}, more...)...)
	}
	flush := func() {
		if lit.Len() > 0 {
			add("-l", lit.String())
			lit.Reset()
		}
	}
	for _, k := range keys {
		if isTypedChar(k) {
			lit.WriteString(k)
			continue
		}
		flush()
		if n, ok := tmuxKeyNames[k]; ok {
			k = n
		}
		add(k)
	}
	flush()
	return args
}

func sendSessionKeys(ctx context.Context, s sessions.Session, keys []string) error {
	for _, k := range keys {
		if !validTerminalKey(k) {
			return errors.New("unknown key " + k + ": Enter, Escape, Tab, BSpace, Up, Down, Left, Right, S-Tab, C-c, C-d, C-l, C-r, C-u, C-z or one character")
		}
	}
	switch s.Host.Kind {
	case sessions.HostTmux:
		if s.Host.Pane == "" {
			return errNoScreen
		}
		_, err := runScreenCommand(ctx, "tmux", tmuxKeysArgs(s.Host.Pane, keys)...)
		return err
	case sessions.HostITerm:
		tty := sessionTTYName(s.TTY)
		if tty == "" {
			return errNoScreen
		}
		_, err := runScreenCommand(ctx, "osascript", "-e", itermRawScript(tty, keysToBytes(keys)))
		return err
	case sessions.HostVSCodeTerminal:
		return sendMirroredKeys(s, keys)
	}
	return errors.New("keys reach tmux, iTerm and VS Code terminal sessions; this one takes text from the board's Send")
}

func itermRawScript(tty, raw string) string {
	var b strings.Builder
	parts := []string{}
	for _, r := range raw {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r < 0x20 || r == 0x7f:
			if b.Len() > 0 {
				parts = append(parts, `"`+b.String()+`"`)
				b.Reset()
			}
			parts = append(parts, "(ASCII character "+itoaRune(r)+")")
		default:
			b.WriteRune(r)
		}
	}
	if b.Len() > 0 {
		parts = append(parts, `"`+b.String()+`"`)
	}
	if len(parts) == 0 {
		parts = []string{`""`}
	}
	return `tell application "iTerm2"
	repeat with w in windows
		repeat with t in tabs of w
			repeat with s in sessions of t
				if tty of s is "` + tty + `" then
					tell s to write text (` + strings.Join(parts, " & ") + `) newline no
					return
				end if
			end repeat
		end repeat
	end repeat
end tell`
}

func itoaRune(r rune) string {
	const digits = "0123456789"
	n := int(r)
	if n == 0 {
		return "0"
	}
	var out []byte
	for n > 0 {
		out = append([]byte{digits[n%10]}, out...)
		n /= 10
	}
	return string(out)
}

func launchSessionScreenHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodGet {
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET ?session=")
		return
	}
	s, code, msg := launchSessionFor(r.URL.Query().Get("session"))
	if code != 0 {
		writeLaunchError(w, code, msg)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	out, err := readSessionScreen(ctx, s, time.Now())
	if err != nil {
		writeLaunchError(w, http.StatusConflict, err.Error())
		return
	}
	writeLaunchJSON(w, out)
}

func launchSessionKeysHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, postOnlyMessage)
		return
	}
	var in struct {
		Session string   `json:"session"`
		Keys    []string `json:"keys"`
		Text    string   `json:"text"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
		writeLaunchError(w, http.StatusBadRequest, "POST {session, keys, text}")
		return
	}
	s, code, msg := launchSessionFor(in.Session)
	if code != 0 {
		writeLaunchError(w, code, msg)
		return
	}
	keys := in.Keys
	for _, r := range in.Text {
		if r == '\n' {
			keys = append(keys, "Enter")
		} else if r >= ' ' && r != 0x7f {
			keys = append(keys, string(r))
		}
	}
	if len(keys) == 0 || len(keys) > 2000 {
		writeLaunchError(w, http.StatusBadRequest, "send 1 to 2000 keys")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := sendSessionKeys(ctx, s, keys); err != nil {
		writeLaunchError(w, http.StatusConflict, err.Error())
		return
	}
	screenCacheMu.Lock()
	delete(screenCache, s.ID)
	screenCacheMu.Unlock()
	writeLaunchJSON(w, map[string]any{"ok": true, "sent": len(keys)})
}
