package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/workspace"
)

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
	t.send("corgi is listening here. /help for what it can do.")
	for {
		if ctx.Err() != nil {
			return
		}
		texts := t.poll(ctx)
		for _, text := range texts {
			t.handle(text)
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

func (t *telegramControl) poll(ctx context.Context) []string {
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?timeout=30&offset=%d",
		t.token, t.offset)
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
			} `json:"message"`
		} `json:"result"`
	}
	if json.NewDecoder(resp.Body).Decode(&payload) != nil || !payload.OK {
		return nil
	}

	var texts []string
	for _, update := range payload.Result {
		if update.UpdateID >= t.offset {
			t.offset = update.UpdateID + 1
		}
		if fmt.Sprint(update.Message.Chat.ID) != t.chatID {
			continue
		}
		if text := strings.TrimSpace(update.Message.Text); text != "" {
			texts = append(texts, text)
		}
	}
	return texts
}

func (t *telegramControl) handle(text string) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
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
	default:
		t.send("unknown command. /help")
	}
}

const telegramHelp = `corgi commands:
/status            what is registered and running
/start <workspace> start a session there
/stop <workspace>  stop it
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

func (t *telegramControl) send(text string) {
	payload, err := json.Marshal(map[string]any{
		"chat_id": t.chatID, "text": text, "disable_web_page_preview": true,
	})
	if err != nil {
		return
	}
	req, err := http.NewRequest(http.MethodPost,
		fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.token),
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

func startTelegramControl(ctx context.Context, notifyURL, agentDir string) {
	control := telegramControlFrom(notifyURL, agentDir)
	if control == nil {
		return
	}
	utils.Info("📨 telegram control on — /help in the chat")
	go control.run(ctx)
}
