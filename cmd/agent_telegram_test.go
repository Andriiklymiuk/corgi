package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/supervisor"
	"andriiklymiuk/corgi/utils/agent/usage"
	"andriiklymiuk/corgi/utils/agent/workspace"
)

func TestTelegramControlFromParsesTokenAndChat(t *testing.T) {
	c := telegramControlFrom("https://api.telegram.org/bot12345:AAsecret/sendMessage?chat_id=99", "/agent")
	if c == nil {
		t.Fatal("a telegram notifyUrl must produce a controller")
	}
	if c.token != "12345:AAsecret" {
		t.Errorf("token = %q", c.token)
	}
	if c.chatID != "99" {
		t.Errorf("chatID = %q", c.chatID)
	}
}

func TestTelegramControlFromIgnoresOtherDestinations(t *testing.T) {
	for _, raw := range []string{
		"",
		"https://ntfy.sh/topic",
		"https://discord.com/api/webhooks/1/x",
		"https://hooks.slack.com/services/x",
		"https://api.telegram.org/bot123/sendMessage",
		"https://api.telegram.org/sendMessage?chat_id=1",
		"::broken",
	} {
		if got := telegramControlFrom(raw, "/agent"); got != nil {
			t.Errorf("%q must not start a telegram controller, got %+v", raw, got)
		}
	}
}

func TestTelegramHelpNamesEveryCommand(t *testing.T) {
	for _, verb := range []string{"/status", "/start", "/stop", "/help"} {
		if !strings.Contains(telegramHelp, verb) {
			t.Errorf("help does not mention %s", verb)
		}
	}
}

func TestSessionFromNotificationReadsTheTitleLine(t *testing.T) {
	for text, want := range map[string]string{
		"corgi agent · acme-api\npermission: Bash go test": "acme-api",
		"corgi agent · corgi-77: a session finished":       "corgi-77",
		"something else entirely":                          "",
		"":                                                 "",
	} {
		if got := sessionFromNotification(text); got != want {
			t.Errorf("%q: got %q want %q", text, got, want)
		}
	}
}

// fakeTelegram is a stand-in for api.telegram.org: it serves queued
// getUpdates bodies in order and records every sendMessage text.
type fakeTelegram struct {
	srv     *httptest.Server
	mu      sync.Mutex
	sent    []string
	updates []string
	polls   int
	onPoll  func(poll int)
}

func newFakeTelegram(t *testing.T, updates ...string) *fakeTelegram {
	t.Helper()
	f := &fakeTelegram{updates: updates}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.sent = append(f.sent, fmt.Sprint(body["text"]))
			_, _ = w.Write([]byte(`{"ok":true}`))
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			f.polls++
			if f.onPoll != nil {
				f.onPoll(f.polls)
			}
			body := `{"ok":true,"result":[]}`
			if len(f.updates) > 0 {
				body, f.updates = f.updates[0], f.updates[1:]
			}
			_, _ = w.Write([]byte(body))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	previous := telegramAPIBase
	telegramAPIBase = f.srv.URL
	t.Cleanup(func() { telegramAPIBase = previous })
	return f
}

func (f *fakeTelegram) messages() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.sent...)
}

func (f *fakeTelegram) last() string {
	m := f.messages()
	if len(m) == 0 {
		return ""
	}
	return m[len(m)-1]
}

// fakeRunningDaemon writes a daemon record naming this very process, which
// passes ReadInfo's liveness and name checks.
func fakeRunningDaemon(t *testing.T, dir string, commands bool) {
	t.Helper()
	exe, _ := os.Executable()
	data, _ := json.Marshal(daemon.Info{PID: os.Getpid(), Executable: exe, Commands: commands})
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "daemon.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeBoardState(t *testing.T, dir string, st sessions.State) {
	t.Helper()
	data, _ := json.Marshal(st)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(daemon.SessionsPath(dir), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func noSettle(t *testing.T, fn func()) {
	t.Helper()
	previous := waitForDaemonToAct
	waitForDaemonToAct = fn
	t.Cleanup(func() { waitForDaemonToAct = previous })
}

// telegramUnderTest wires a controller to an isolated agent dir and home.
func telegramUnderTest(t *testing.T) (*telegramControl, string) {
	t.Helper()
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	dir := mustAgentDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	c := telegramControlFrom("https://api.telegram.org/bot1:x/sendMessage?chat_id=99", dir)
	if c == nil {
		t.Fatal("controller")
	}
	return c, dir
}

func TestTelegramPollReadsUpdatesForThisChat(t *testing.T) {
	fake := newFakeTelegram(t, `{"ok":true,"result":[
		{"update_id":10,"message":{"text":"/help","chat":{"id":99}}},
		{"update_id":11,"message":{"text":"not mine","chat":{"id":7}}},
		{"update_id":12,"message":{"text":"   ","chat":{"id":99}}},
		{"update_id":13,"message":{"text":"yes","chat":{"id":99},"reply_to_message":{"text":"corgi agent · acme\nneeds you"}}}
	]}`, `{"ok":false}`, `{not json`)
	c := telegramControlFrom("https://api.telegram.org/bot1:x/sendMessage?chat_id=99", "")

	got := c.poll(context.Background())
	if len(got) != 2 || got[0].Text != "/help" || got[1].Text != "yes" || got[1].ReplyTo != "corgi agent · acme\nneeds you" {
		t.Fatalf("poll = %+v", got)
	}
	if c.offset != 14 {
		t.Errorf("offset = %d, want one past the last update", c.offset)
	}
	if got := c.poll(context.Background()); got != nil {
		t.Errorf("ok:false must yield nothing, got %+v", got)
	}
	if got := c.poll(context.Background()); got != nil {
		t.Errorf("bad json must yield nothing, got %+v", got)
	}
	if fake.polls != 3 {
		t.Errorf("polls = %d", fake.polls)
	}

	fake.srv.Close()
	if got := c.poll(context.Background()); got != nil {
		t.Errorf("a dead server yields nothing, got %+v", got)
	}
	telegramAPIBase = "://bad"
	if got := c.poll(context.Background()); got != nil {
		t.Errorf("an unbuildable request yields nothing, got %+v", got)
	}
}

func TestTelegramSendPostsToTheChat(t *testing.T) {
	fake := newFakeTelegram(t)
	c := telegramControlFrom("https://api.telegram.org/bot1:x/sendMessage?chat_id=99", "")
	c.send("hello")
	if m := fake.messages(); len(m) != 1 || m[0] != "hello" {
		t.Fatalf("sent = %v", m)
	}
	fake.srv.Close()
	c.send("lost") // a dead server is not fatal
	telegramAPIBase = "://bad"
	c.send("unbuildable")
	if len(fake.messages()) != 1 {
		t.Error("failed sends must not be recorded")
	}
}

func TestTelegramHandleWithoutADaemon(t *testing.T) {
	fake := newFakeTelegram(t)
	c, _ := telegramUnderTest(t)
	for _, tc := range []struct {
		text, replyTo, want string
	}{
		{"", "", ""},
		{"hello", "", "reply to a session's notification to type into it, or /help"},
		{"hello", "corgi agent · acme\nneeds you", "corgi agent is not running"},
		{"hello", "something unrelated", ""},
		{"/help", "", telegramHelp},
		{"/HELP@corgibot", "", telegramHelp},
		{"/start_help", "", telegramHelp},
		{"/status", "", "no daemon status yet"},
		{"/ls", "", "no daemon status yet"},
		{"/list", "", "no daemon status yet"},
		{"/start", "", "which workspace? /status lists them"},
		{"/run nope", "", `no workspace called "nope". /status lists them`},
		{"/stop nope", "", `no workspace called "nope". /status lists them`},
		{"/sessions", "", "no sessions on the board"},
		{"/board", "", "no sessions on the board"},
		{"/usage", "", "default: no /usage snapshot yet"},
		{"/limits", "", "default: no /usage snapshot yet"},
		{"/allow", "", "/allow <session>"},
		{"/yes s1", "", "corgi agent is not running"},
		{"/always", "", "/always <session>"},
		{"/deny", "", "/deny <session>"},
		{"/no s1", "", "corgi agent is not running"},
		{"/send", "", "/send <session> <text>"},
		{"/send s1", "", "/send <session> <text>"},
		{"/say s1 go on", "", "corgi agent is not running"},
		{"/focus s1", "", "corgi agent is not running"},
		{"/bogus", "", "unknown command. /help"},
	} {
		before := len(fake.messages())
		c.handle(tc.text, tc.replyTo)
		after := fake.messages()
		if tc.want == "" {
			if len(after) != before {
				t.Errorf("%q: sent %q, want nothing", tc.text, after[len(after)-1])
			}
			continue
		}
		if len(after) != before+1 || after[len(after)-1] != tc.want {
			t.Errorf("%q: sent %v, want %q", tc.text, after[before:], tc.want)
		}
	}
}

func TestTelegramBoardReportsTheDaemonsVerdict(t *testing.T) {
	fake := newFakeTelegram(t)
	c, dir := telegramUnderTest(t)
	t0 := time.Now().Add(-time.Minute)
	writeBoardState(t, dir, sessions.State{UpdatedAt: t0, NoticeAt: t0, Sessions: []sessions.Session{{ID: "s1", Display: "acme"}}})

	c.agentIn = ""
	c.sendToSession("s1", "hi")
	if len(fake.messages()) != 0 {
		t.Fatal("no agent dir, no reply")
	}
	c.agentIn = dir

	fakeRunningDaemon(t, dir, true)
	noSettle(t, func() {})
	c.answer("acme", "allow")
	if fake.last() != "allow sent to acme" {
		t.Errorf("quiet daemon: %q", fake.last())
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, "commands")); len(entries) != 1 {
		t.Errorf("spool has %d entries", len(entries))
	}

	c.board(command.Command{Action: command.ActionFocus}, "focused")
	if !strings.HasPrefix(fake.last(), "could not queue that: ") {
		t.Errorf("invalid command: %q", fake.last())
	}

	noSettle(t, func() {
		writeBoardState(t, dir, sessions.State{UpdatedAt: t0, Notice: "no window to open in", NoticeAt: time.Now()})
	})
	c.sendToSession("s1", "hi")
	if fake.last() != "corgi: no window to open in" {
		t.Errorf("notice: %q", fake.last())
	}

	writeBoardState(t, dir, sessions.State{UpdatedAt: t0, NoticeAt: t0})
	noSettle(t, func() {
		writeBoardState(t, dir, sessions.State{UpdatedAt: t0, NoticeAt: t0, Sessions: []sessions.Session{
			{ID: "s1", Display: "ACME", FocusError: "window is gone", FocusAt: time.Now()},
		}})
	})
	c.board(command.Command{Action: command.ActionFocus, SessionID: "acme"}, "focusing acme")
	if fake.last() != "corgi: window is gone" {
		t.Errorf("focus error: %q", fake.last())
	}

	writeBoardState(t, dir, sessions.State{UpdatedAt: t0, NoticeAt: t0, Sessions: []sessions.Session{
		{ID: "s1", Display: "acme", FocusError: "old news", FocusAt: t0.Add(-time.Hour)},
	}})
	noSettle(t, func() {})
	c.board(command.Command{Action: command.ActionFocus, SessionID: "s1"}, "focusing s1")
	if fake.last() != "focusing s1" {
		t.Errorf("a stale focus error is not reported: %q", fake.last())
	}
}

func TestTelegramBoardText(t *testing.T) {
	c, dir := telegramUnderTest(t)
	if got := c.boardText(); got != "no sessions on the board" {
		t.Errorf("empty: %q", got)
	}
	writeBoardState(t, dir, sessions.State{Sessions: []sessions.Session{
		{ID: "a", Display: "acme", Status: sessions.StatusNeedsInput, Detail: "permission: Bash", Context: &usage.Context{Percent: 42}},
		{ID: "b", Display: "web", Status: sessions.StatusWorking},
	}})
	got := c.boardText()
	for _, want := range []string{"▲ acme — NEEDS YOU · permission: Bash · ctx 42%", "● web — WORKING\n", "/send <session> <text>"} {
		if !strings.Contains(got, want) {
			t.Errorf("board text %q lacks %q", got, want)
		}
	}
	_ = os.WriteFile(daemon.SessionsPath(dir), []byte("{"), 0o600)
	if got := c.boardText(); got != "no sessions on the board" {
		t.Errorf("corrupt board: %q", got)
	}
}

func TestTelegramUsageText(t *testing.T) {
	c, dir := telegramUnderTest(t)
	resets := time.Now().Add(2 * time.Hour)
	writeBoardState(t, dir, sessions.State{Accounts: []sessions.Account{
		{Profile: "personal"},
		{Profile: "work", ConfigDir: filepath.Join(dir, "claude-work"),
			Limits:   &usage.Limits{FiveHour: usage.Window{Percent: 62, ResetsAt: resets}, SevenDay: usage.Window{Percent: 30}},
			Forecast: &usage.Forecast{FiveHour: &usage.WindowForecast{PercentPerHour: 10, Safe: true, ExhaustAt: resets.Add(time.Hour)}}},
	}})
	if err := usage.RecordWait(dir, usage.Wait{At: time.Now(), Kind: "wait", Label: "acme", Seconds: 90}); err != nil {
		t.Fatal(err)
	}
	got := c.usageText()
	for _, want := range []string{
		"personal: no /usage snapshot yet",
		"work: 5h 62% (resets " + resetText(resets) + ") · week 30% · 5h: 10%/h, lasts until the reset",
		"waited on you 1× today, longest 1m",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("usage text %q lacks %q", got, want)
		}
	}
}

func TestTelegramStatusText(t *testing.T) {
	c, dir := telegramUnderTest(t)
	fakeRunningDaemon(t, dir, true)
	registerStacks(t, dir, map[string]string{"acme": t.TempDir(), "beta": t.TempDir()})
	status, _ := json.Marshal(daemon.Status{Running: true, Workspaces: []supervisor.RunState{{WorkspaceID: "acme", Running: true}}})
	if err := os.WriteFile(filepath.Join(dir, "status.json"), status, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := c.statusText(); got != "▶ acme\n· beta" {
		t.Errorf("status = %q", got)
	}
	if err := os.WriteFile(agentRegistryPath(dir), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := c.statusText(); got != "no registered workspaces" {
		t.Errorf("unreadable registry: %q", got)
	}
}

func TestTelegramControlStartsAndStopsAWorkspace(t *testing.T) {
	fake := newFakeTelegram(t)
	c, dir := telegramUnderTest(t)
	reg := &workspace.Registry{}
	reg.Upsert(workspace.Workspace{ID: "acme", AbsPath: t.TempDir(), Aliases: []string{"api"}, Status: workspace.StatusOK})
	if err := workspace.Save(agentRegistryPath(dir), reg); err != nil {
		t.Fatal(err)
	}

	c.control(command.ActionStart, "API")
	if fake.last() != "the daemon is not accepting commands right now" {
		t.Errorf("no daemon: %q", fake.last())
	}
	fakeRunningDaemon(t, dir, false)
	c.control(command.ActionStart, "api")
	if fake.last() != "the daemon is not accepting commands right now" {
		t.Errorf("old daemon: %q", fake.last())
	}
	fakeRunningDaemon(t, dir, true)
	c.control(command.ActionStop, "Acme")
	if fake.last() != "stop acme — asked" {
		t.Errorf("stop: %q", fake.last())
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, "commands")); len(entries) != 1 {
		t.Errorf("spool has %d entries", len(entries))
	}

	if id, ok := c.resolveWorkspace("nope"); ok || id != "" {
		t.Errorf("unknown = %q, %v", id, ok)
	}
	_ = os.WriteFile(agentRegistryPath(dir), []byte("{"), 0o600)
	if _, ok := c.resolveWorkspace("acme"); ok {
		t.Error("an unreadable registry resolves nothing")
	}
}

func TestTelegramRunGreetsOnceThenDispatches(t *testing.T) {
	fake := newFakeTelegram(t, `{"ok":true,"result":[{"update_id":1,"message":{"text":"/help","chat":{"id":99}}}]}`)
	c, dir := telegramUnderTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fake.onPoll = func(poll int) {
		if poll == 2 {
			cancel()
		}
	}
	c.run(ctx)
	if m := fake.messages(); len(m) != 2 || !strings.HasPrefix(m[0], "corgi is listening here") || m[1] != telegramHelp {
		t.Fatalf("sent = %v", m)
	}
	if _, err := os.Stat(filepath.Join(dir, "telegram", "greeted-99")); err != nil {
		t.Error("the greeting must be marked so a restart stays quiet")
	}

	// Cancelled before it starts: no poll, and the chat was greeted already.
	c.run(ctx)
	if len(fake.messages()) != 2 || fake.polls != 2 {
		t.Errorf("a restart must neither greet nor poll: sent=%v polls=%d", fake.messages(), fake.polls)
	}
}

func TestTelegramFirstTimeInChat(t *testing.T) {
	c := telegramControlFrom("https://api.telegram.org/bot1:x/sendMessage?chat_id=99", "")
	if !c.firstTimeInChat() || !c.firstTimeInChat() {
		t.Error("with no agent dir there is nowhere to remember, so every start greets")
	}
	c.agentIn = t.TempDir()
	if !c.firstTimeInChat() {
		t.Error("first time")
	}
	if c.firstTimeInChat() {
		t.Error("second time")
	}
}

// A cancelled context must stop the control loop before it greets, because
// the greeting is a network call. The test used to assert nothing and leak
// the goroutine that made it, which is what failed a release on CI.
func TestStartTelegramControlStopsBeforeItSaysAnything(t *testing.T) {
	c, dir := telegramUnderTest(t)
	c.firstTimeInChat() // greeted, so this chat has nothing to send anyway
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Not a telegram URL: no control at all, and a channel already closed so
	// a caller waiting on it is never held up.
	notTelegram := startTelegramControl(ctx, "https://ntfy.sh/topic", dir)
	select {
	case <-notTelegram:
	case <-time.After(time.Second):
		t.Fatal("a URL with no chat behind it must not leave the caller waiting")
	}

	// A chat nobody has greeted yet, so the marker can only appear if the
	// cancelled loop went ahead and greeted it.
	done := startTelegramControl(ctx, "https://api.telegram.org/bot1:x/sendMessage?chat_id=100", dir)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the chat loop must be joinable: a cancelled context has to end it")
	}

	marker := filepath.Join(dir, "telegram", "greeted-100")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			t.Fatal("a cancelled daemon greeted the chat anyway — that is a network call on the way out")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
