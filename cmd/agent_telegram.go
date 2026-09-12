package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/workspace"
)

// telegramAPIBase is swapped for an httptest server in tests.
var telegramAPIBase = "https://api.telegram.org"

// waitForDaemonToAct gives the daemon a moment to refuse a board command
// before the reply goes out; tests replace it.
var waitForDaemonToAct = func() { time.Sleep(1500 * time.Millisecond) }

type telegramControl struct {
	token   string
	chatID  string
	agentIn string
	client  *http.Client
	offset  int64
}

func telegramControlFrom(notifyURL, agentDir string) *telegramControl {
	u, err := url.Parse(strings.TrimSpace(notifyURL))
	if err != nil || !strings.EqualFold(u.Host, "api.telegram.org") {
		return nil
	}
	segment := strings.SplitN(strings.TrimPrefix(u.Path, "/"), "/", 2)[0]
	if !strings.HasPrefix(segment, "bot") {
		return nil
	}
	token := strings.TrimPrefix(segment, "bot")
	chat := strings.TrimSpace(u.Query().Get("chat_id"))
	if token == "" || chat == "" {
		return nil
	}
	return &telegramControl{
		token:   token,
		chatID:  chat,
		agentIn: agentDir,
		client:  &http.Client{Timeout: 65 * time.Second},
	}
}

func (t *telegramControl) run(ctx context.Context) {
	// A daemon that is already shutting down has nothing to say. Checked
	// before the greeting, which is a network call: without this, stopping
	// the daemon in its first moment still posts into the chat.
	if ctx.Err() != nil {
		return
	}
	// Once per chat, not on every daemon restart: the greeting is for a
	// chat that just got wired up, and a restart is not news.
	if t.firstTimeInChat() {
		t.sendCtx(ctx, "corgi is listening here. /help for what it can do.")
	}
	for {
		if ctx.Err() != nil {
			return
		}
		texts := t.poll(ctx)
		for _, m := range texts {
			t.handle(m.Text, m.ReplyTo)
		}
		if len(texts) == 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
	}
}

// telegramMessage is one incoming message: its text and, when it answers
// one of corgi's own notifications, that notification's text.
type telegramMessage struct {
	Text    string
	ReplyTo string
}

func (t *telegramControl) poll(ctx context.Context) []telegramMessage {
	endpoint := fmt.Sprintf("%s/bot%s/getUpdates?timeout=30&offset=%d",
		telegramAPIBase, t.token, t.offset)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var payload struct {
		OK     bool `json:"ok"`
		Result []struct {
			UpdateID int64 `json:"update_id"`
			Message  struct {
				Text string `json:"text"`
				Chat struct {
					ID int64 `json:"id"`
				} `json:"chat"`
				ReplyTo *struct {
					Text string `json:"text"`
				} `json:"reply_to_message"`
			} `json:"message"`
		} `json:"result"`
	}
	if json.NewDecoder(resp.Body).Decode(&payload) != nil || !payload.OK {
		return nil
	}

	var texts []telegramMessage
	for _, update := range payload.Result {
		if update.UpdateID >= t.offset {
			t.offset = update.UpdateID + 1
		}
		if fmt.Sprint(update.Message.Chat.ID) != t.chatID {
			continue
		}
		if text := strings.TrimSpace(update.Message.Text); text != "" {
			m := telegramMessage{Text: text}
			if update.Message.ReplyTo != nil {
				m.ReplyTo = update.Message.ReplyTo.Text
			}
			texts = append(texts, m)
		}
	}
	return texts
}

func (t *telegramControl) handle(text, replyTo string) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return
	}
	if !strings.HasPrefix(fields[0], "/") {
		// Plain text is either a reply to a "needs you" notification —
		// typed into that session — or noise.
		if label := sessionFromNotification(replyTo); label != "" {
			t.sendToSession(label, text)
		} else if replyTo == "" {
			t.send("reply to a session's notification to type into it, or /help")
		}
		return
	}
	verb := strings.ToLower(strings.TrimPrefix(fields[0], "/"))
	if at := strings.Index(verb, "@"); at >= 0 {
		verb = verb[:at]
	}
	arg := ""
	if len(fields) > 1 {
		arg = fields[1]
	}

	switch verb {
	case "help", "start_help":
		t.send(telegramHelp)
	case "status", "ls", "list":
		t.send(t.statusText())
	case "start", "run":
		t.control(command.ActionStart, arg)
	case "stop":
		t.control(command.ActionStop, arg)
	case "sessions", "board":
		t.send(t.boardText())
	case "usage", "limits":
		t.send(t.usageText())
	case "allow", "yes":
		t.answer(arg, "allow")
	case "always":
		t.answer(arg, "always")
	case "deny", "no":
		t.answer(arg, "deny")
	case "send", "say":
		if len(fields) < 3 {
			t.send("/send <session> <text>")
			return
		}
		t.sendToSession(arg, strings.Join(fields[2:], " "))
	case "focus":
		t.board(command.Command{Action: command.ActionFocus, SessionID: arg}, "focusing "+arg)
	case "ask":
		if len(fields) < 2 {
			t.send("/ask <question about the board>")
			return
		}
		// The chief runs a short claude; the answer follows in a moment,
		// and the poll loop is not held while it thinks.
		question := strings.Join(fields[1:], " ")
		go func() {
			answer, err := askBoard(context.Background(), t.agentIn, question)
			if err != nil {
				t.send("could not ask: " + err.Error())
				return
			}
			t.send(answer)
		}()
	default:
		t.send("unknown command. /help")
	}
}

// sessionFromNotification finds the session a corgi notification was about:
// its title line reads "corgi agent · <label>".
func sessionFromNotification(text string) string {
	first := strings.TrimSpace(strings.SplitN(text, "\n", 2)[0])
	_, label, ok := strings.Cut(first, "corgi agent · ")
	if !ok {
		return ""
	}
	if i := strings.IndexAny(label, ":\n"); i >= 0 {
		label = label[:i]
	}
	return strings.TrimSpace(label)
}

func (t *telegramControl) sendToSession(ref, text string) {
	t.board(command.Command{Action: command.ActionSend, SessionID: ref, Text: text, Enter: true, Source: "telegram"},
		"typed into "+ref)
}

func (t *telegramControl) answer(ref, answer string) {
	if ref == "" {
		t.send("/" + answer + " <session>")
		return
	}
	t.board(command.Command{Action: command.ActionAnswer, SessionID: ref, Answer: answer, Source: "telegram"},
		answer+" sent to "+ref)
}

// board drops a board command in the spool for the daemon, and reports the
// board's notice a moment later when the daemon refused it.
func (t *telegramControl) board(c command.Command, done string) {
	if t.agentIn == "" {
		return
	}
	info, err := daemon.ReadInfo(t.agentIn)
	if err != nil || info == nil {
		t.send("corgi agent is not running")
		return
	}
	before, _ := readBoard(t.agentIn)
	if _, err := command.Write(t.agentIn, c); err != nil {
		t.send("could not queue that: " + err.Error())
		return
	}
	daemon.Nudge(info)
	waitForDaemonToAct()
	after, err := readBoard(t.agentIn)
	if err == nil && after.Notice != "" && after.NoticeAt.After(before.NoticeAt) {
		t.send("corgi: " + after.Notice)
		return
	}
	if err == nil && c.SessionID != "" {
		for _, s := range after.Sessions {
			if (s.ID == c.SessionID || strings.EqualFold(s.Display, c.SessionID) || strings.EqualFold(s.Label, c.SessionID)) && s.FocusError != "" && s.FocusAt.After(before.UpdatedAt) {
				t.send("corgi: " + s.FocusError)
				return
			}
		}
	}
	t.send(done)
}

func (t *telegramControl) boardText() string {
	board, err := readBoard(t.agentIn)
	if err != nil || len(board.Sessions) == 0 {
		return "no sessions on the board"
	}
	var b strings.Builder
	for _, s := range board.Sessions {
		fmt.Fprintf(&b, "%s %s — %s", statusGlyph(s.Status), s.Display, statusWord(s.Status))
		if s.Detail != "" {
			b.WriteString(" · " + s.Detail)
		}
		if s.Context != nil && s.Context.Percent > 0 {
			fmt.Fprintf(&b, " · ctx %d%%", s.Context.Percent)
		}
		b.WriteString("\n")
	}
	b.WriteString("reply to a notification, or /send <session> <text> · /allow /deny <session>")
	return b.String()
}

func (t *telegramControl) usageText() string {
	rep := buildUsageReport(t.agentIn, time.Now())
	if len(rep.Accounts) == 0 {
		return "no accounts known yet"
	}
	var b strings.Builder
	for _, a := range rep.Accounts {
		if a.Limits == nil {
			fmt.Fprintf(&b, "%s: no /usage snapshot yet\n", a.Profile)
			continue
		}
		fmt.Fprintf(&b, "%s: 5h %d%% (resets %s) · week %d%%", a.Profile, a.Limits.FiveHour.Percent, resetText(a.Limits.FiveHour.ResetsAt), a.Limits.SevenDay.Percent)
		if line := forecastLine(a.Limits, a.Forecast); line != "" {
			b.WriteString(" · " + line)
		}
		b.WriteString("\n")
	}
	if rep.Waits.Count > 0 {
		fmt.Fprintf(&b, "waited on you %d× today, longest %s", rep.Waits.Count, shortDuration(time.Duration(rep.Waits.Longest)*time.Second))
	}
	return strings.TrimSpace(b.String())
}

const telegramHelp = `corgi commands:
/status            what is registered and running
/start <workspace> start a session there
/stop <workspace>  stop it
/sessions          the board: every Claude session and what it is doing
/usage             each account's limits and when they run out
/send <s> <text>   type into a session (reply to its notification does the same)
/allow /always /deny <s>  answer its permission prompt
/focus <s>         bring its window to the front
/ask <question>    the chief: what to look at first, what is blocked, who is on what
/help              this`

func (t *telegramControl) statusText() string {
	status, err := daemon.ReadStatus(t.agentIn)
	if err != nil || status == nil {
		return "no daemon status yet"
	}
	registry, _, regErr := agentRegistry()
	var lines []string
	if regErr == nil {
		running := map[string]bool{}
		for _, ws := range status.Workspaces {
			running[ws.WorkspaceID] = ws.Running
		}
		for _, ws := range registry.Sorted() {
			mark := "·"
			if running[ws.ID] {
				mark = "▶"
			}
			lines = append(lines, mark+" "+ws.ID)
		}
	}
	if len(lines) == 0 {
		return "no registered workspaces"
	}
	return strings.Join(lines, "\n")
}

func (t *telegramControl) control(action, name string) {
	if name == "" {
		t.send("which workspace? /status lists them")
		return
	}
	id, ok := t.resolveWorkspace(name)
	if !ok {
		t.send(fmt.Sprintf("no workspace called %q. /status lists them", name))
		return
	}
	info, err := daemon.ReadInfo(t.agentIn)
	if err != nil || info == nil || !info.Commands {
		t.send("the daemon is not accepting commands right now")
		return
	}
	if _, err := command.Write(t.agentIn, command.Command{
		Action: action, WorkspaceID: id, Source: "telegram",
	}); err != nil {
		t.send("could not queue that: " + err.Error())
		return
	}
	daemon.Nudge(info)
	t.send(fmt.Sprintf("%s %s — asked", action, id))
}

func (t *telegramControl) resolveWorkspace(name string) (string, bool) {
	registry, _, err := agentRegistry()
	if err != nil {
		return "", false
	}
	want := strings.ToLower(strings.TrimSpace(name))
	for _, ws := range registry.Sorted() {
		if strings.ToLower(ws.ID) == want || matchesAlias(ws, want) {
			return ws.ID, true
		}
	}
	return "", false
}

func matchesAlias(ws workspace.Workspace, want string) bool {
	for _, alias := range ws.Aliases {
		if strings.ToLower(alias) == want {
			return true
		}
	}
	return false
}

func (t *telegramControl) send(text string) { t.sendCtx(context.Background(), text) }

// sendCtx posts one message. It takes a context so a daemon shutting down
// does not sit in a ten-second POST nobody is waiting for any more.
func (t *telegramControl) sendCtx(ctx context.Context, text string) {
	payload, err := json.Marshal(map[string]any{
		"chat_id": t.chatID, "text": text, "disable_web_page_preview": true,
	})
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/bot%s/sendMessage", telegramAPIBase, t.token),
		strings.NewReader(string(payload)))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}

// startTelegramControl runs the chat loop until ctx ends. The returned
// channel closes when the loop has actually stopped, so shutdown can wait
// for it instead of leaving a goroutine mid-request.
func startTelegramControl(ctx context.Context, notifyURL, agentDir string) <-chan struct{} {
	done := make(chan struct{})
	control := telegramControlFrom(notifyURL, agentDir)
	if control == nil {
		close(done)
		return done
	}
	utils.Info("📨 telegram control on — /help in the chat")
	go func() {
		defer close(done)
		control.run(ctx)
	}()
	return done
}

// firstTimeInChat reports whether this chat has been greeted, and marks it.
func (t *telegramControl) firstTimeInChat() bool {
	if t.agentIn == "" {
		return true
	}
	dir := filepath.Join(t.agentIn, "telegram")
	marker := filepath.Join(dir, "greeted-"+t.chatID)
	if _, err := os.Stat(marker); err == nil {
		return false
	}
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(marker, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600)
	return true
}
