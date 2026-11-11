package cmd

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/sessions"
)

//go:embed telegram_card.html
var telegramCardHTML string

const (
	telegramCardStateLimit = 1024
	telegramCardQueryLimit = 64
	telegramCardAwayWindow = 12 * time.Hour
)

var telegramCardWatchEvery = 3 * time.Second

type telegramCardRow struct {
	Name    string  `json:"n"`
	Word    string  `json:"w,omitempty"`
	Detail  string  `json:"d,omitempty"`
	Percent *int    `json:"p,omitempty"`
	Buttons [][]any `json:"b,omitempty"`
	URL     string  `json:"u,omitempty"`
	Copy    string  `json:"c,omitempty"`
}

type telegramCard struct {
	Title    string            `json:"t"`
	Subtitle string            `json:"s,omitempty"`
	Refresh  string            `json:"q,omitempty"`
	Rows     []telegramCardRow `json:"r,omitempty"`
	Note     string            `json:"e,omitempty"`
}

type telegramWidgetQuery struct {
	ID   json.RawMessage `json:"id"`
	Data string          `json:"data"`
	Chat *struct {
		ID int64 `json:"id"`
	} `json:"chat"`
	MessageID int64 `json:"message_id"`
}

func shortenForCard(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit-1]) + "…"
}

func (c telegramCard) fitted() string {
	hidden := 0
	for {
		card := c
		if hidden > 0 {
			card.Note = strings.TrimSpace(fmt.Sprintf("%s +%d more", c.Note, hidden))
		}
		data, err := json.Marshal(card)
		if err != nil {
			return `{"t":"corgi"}`
		}
		if len(data) <= telegramCardStateLimit || len(c.Rows) == 0 {
			return string(data)
		}
		c.Rows = c.Rows[:len(c.Rows)-1]
		hidden++
	}
}

func answerButtons(prefix, sessionID string) [][]any {
	if len(prefix)+len(":always:")+len(sessionID) > telegramCardQueryLimit {
		return nil
	}
	return [][]any{
		{"Deny", prefix + ":deny:" + sessionID, 0},
		{"Always", prefix + ":always:" + sessionID, 0},
		{"Allow", prefix + ":allow:" + sessionID, 1},
	}
}

func answerWord(answer string) string {
	switch answer {
	case "allow":
		return "allowed"
	case "always":
		return "always allowed"
	default:
		return "denied"
	}
}

func sessionNameForCard(s sessions.Session) string {
	return shortenForCard(firstNonEmpty(s.Display, s.Label, s.ID), 28)
}

func permissionCard(s sessions.Session, answered string) telegramCard {
	card := telegramCard{Title: sessionNameForCard(s), Refresh: "p:" + s.ID}
	if len(card.Refresh) > telegramCardQueryLimit {
		card.Refresh = ""
	}
	row := telegramCardRow{Name: firstNonEmpty(s.Tool, "permission"), Detail: shortenForCard(s.Detail, 160)}
	if s.Pending != nil {
		row.Name = firstNonEmpty(s.Pending.Tool, row.Name)
		row.Detail = shortenForCard(firstNonEmpty(s.Pending.Subject, s.Detail), 160)
		if s.Pending.Risk != "" {
			row.Word = "risk " + s.Pending.Risk
		}
	}
	switch {
	case answered != "":
		card.Subtitle = answerWord(answered)
		row.Word = answerWord(answered)
	case s.Status == sessions.StatusNeedsInput && s.Pending != nil:
		card.Subtitle = "needs you"
		row.Buttons = answerButtons("a", s.ID)
	default:
		card.Subtitle = statusWord(s.Status)
		row.Word = "answered"
	}
	card.Rows = []telegramCardRow{row}
	return card
}

func boardCard(board boardReport, now time.Time) telegramCard {
	card := telegramCard{Title: "Sessions", Subtitle: now.Local().Format("15:04"), Refresh: "b"}
	for _, s := range board.Sessions {
		if s.Status == sessions.StatusGone {
			continue
		}
		row := telegramCardRow{Name: sessionNameForCard(s), Word: statusWord(s.Status), Detail: shortenForCard(s.Detail, 60)}
		if s.Status == sessions.StatusNeedsInput && s.Pending != nil {
			row.Detail = shortenForCard(strings.TrimSpace(s.Pending.Tool+" "+s.Pending.Subject), 60)
			row.Buttons = answerButtons("A", s.ID)
		}
		card.Rows = append(card.Rows, row)
	}
	if len(card.Rows) == 0 {
		card.Note = "no sessions on the board"
	}
	return card
}

func usageCard(rep usageReport) telegramCard {
	card := telegramCard{Title: "Usage", Subtitle: rep.At.Local().Format("15:04"), Refresh: "u"}
	for _, a := range rep.Accounts {
		if a.Limits == nil {
			card.Rows = append(card.Rows, telegramCardRow{Name: a.Profile, Word: "no /usage snapshot yet"})
			continue
		}
		fiveHour, week := a.Limits.FiveHour.Percent, a.Limits.SevenDay.Percent
		card.Rows = append(card.Rows,
			telegramCardRow{Name: a.Profile + " · 5h", Word: fmt.Sprintf("%d%% · resets %s", fiveHour, resetText(a.Limits.FiveHour.ResetsAt)), Percent: &fiveHour},
			telegramCardRow{Name: a.Profile + " · week", Word: fmt.Sprintf("%d%%", week), Percent: &week})
	}
	if len(card.Rows) == 0 {
		card.Note = "no accounts known yet"
	}
	return card
}

func levelsCard(reports []factoryReport) telegramCard {
	card := telegramCard{Title: "Levels", Subtitle: "per workspace", Refresh: "l"}
	for _, r := range reports {
		filled := r.Level * 20
		row := telegramCardRow{Name: shortenForCard(r.Workspace, 28), Word: fmt.Sprintf("%d · %s", r.Level, r.Name), Percent: &filled}
		if r.Level < len(factoryLadder) {
			row.Copy = fmt.Sprintf("corgi agent level %d --workspace %s", r.Level+1, r.Workspace)
		}
		card.Rows = append(card.Rows, row)
	}
	if len(card.Rows) == 0 {
		card.Note = "no registered workspaces"
	}
	return card
}

func awayCard(rep awayReport) telegramCard {
	card := telegramCard{Title: "While you were away", Subtitle: "since " + rep.Since.Local().Format("Mon 15:04"), Refresh: "m"}
	add := func(word string, lines []awayLine) {
		for _, l := range lines {
			row := telegramCardRow{Name: shortenForCard(l.Ref, 28), Word: word, Detail: shortenForCard(l.What, 60)}
			if strings.HasPrefix(l.URL, "https://") && len(l.URL) <= 256 {
				row.URL = l.URL
			}
			card.Rows = append(card.Rows, row)
		}
	}
	add("opened", rep.Opened)
	add("failed", rep.Failed)
	add("running", rep.Waiting)
	add("arrived", rep.Arrived)
	if len(card.Rows) == 0 {
		card.Note = "nothing arrived"
	}
	if len(rep.Deferred) > 0 {
		card.Note = strings.TrimSpace(fmt.Sprintf("%s %d waiting for a free slot", card.Note, len(rep.Deferred)))
	}
	return card
}

func (t *telegramControl) cardsMarker() string {
	return filepath.Join(t.agentIn, "telegram", "cards-"+t.chatID)
}

func (t *telegramControl) cardsOn() bool {
	if t.agentIn == "" {
		return false
	}
	_, err := os.Stat(t.cardsMarker())
	return err == nil
}

func (t *telegramControl) setCards(on bool) error {
	if !on {
		err := os.Remove(t.cardsMarker())
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(t.cardsMarker()), 0o700); err != nil {
		return err
	}
	return os.WriteFile(t.cardsMarker(), []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600)
}

func (t *telegramControl) callTelegram(ctx context.Context, method string, payload map[string]any) bool {
	body, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/bot%s/%s", telegramAPIBase, t.token, method), bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var answer struct {
		OK bool `json:"ok"`
	}
	return json.NewDecoder(resp.Body).Decode(&answer) == nil && answer.OK
}

func (t *telegramControl) sendCard(ctx context.Context, card telegramCard) bool {
	return t.callTelegram(ctx, "sendWidget", map[string]any{
		"chat_id": t.chatID, "html": telegramCardHTML, "chat_data": card.fitted(),
		"allow_queries": true, "disable_notification": true,
	})
}

func (t *telegramControl) cardByName(name string) (telegramCard, bool) {
	switch strings.ToLower(name) {
	case "board", "sessions", "b":
		board, _ := readBoard(t.agentIn)
		return boardCard(board, time.Now()), true
	case "usage", "limits", "u":
		return usageCard(buildUsageReport(t.agentIn, time.Now())), true
	case "levels", "level", "l":
		reports, _ := factoryLevelsOfAll(t.agentIn)
		return levelsCard(reports), true
	case "morning", "away", "m":
		return awayCard(buildAwayReport(t.agentIn, time.Now().Add(-telegramCardAwayWindow))), true
	}
	return telegramCard{}, false
}

const telegramCardsHelp = `/cards on|off      permission prompts arrive as a card with Allow / Always / Deny
/card board        every session, live, with the buttons on the ones that wait
/card usage        each account's windows as bars
/card levels       each workspace's level and the command for the next one
/card morning      what was opened, failed or arrived in the last 12 hours`

func (t *telegramControl) cardCommand(arg string) {
	switch strings.ToLower(arg) {
	case "on", "off":
		if err := t.setCards(arg == "on"); err != nil {
			t.send("could not switch cards: " + err.Error())
			return
		}
		if arg == "on" {
			t.send("cards on - a session that waits on you sends one. /card board for the live board")
			return
		}
		t.send("cards off - text only")
	case "":
		state := "off"
		if t.cardsOn() {
			state = "on"
		}
		t.send("cards are " + state + "\n" + telegramCardsHelp)
	default:
		card, known := t.cardByName(arg)
		if !known {
			t.send(telegramCardsHelp)
			return
		}
		if !t.sendCard(context.Background(), card) {
			t.send("Telegram refused the card (an older app, or a chat that is not private). The text commands still work: /sessions /usage")
		}
	}
}

func (t *telegramControl) answerFromCard(answer, sessionID string, onBoard bool) telegramCard {
	board, err := readBoard(t.agentIn)
	if err != nil {
		return telegramCard{Title: "corgi", Note: "the board could not be read"}
	}
	var session *sessions.Session
	for i := range board.Sessions {
		if board.Sessions[i].ID == sessionID {
			session = &board.Sessions[i]
		}
	}
	if session == nil {
		return telegramCard{Title: "corgi", Note: "that session is gone"}
	}
	if answer != "allow" && answer != "always" && answer != "deny" {
		return permissionCard(*session, "")
	}
	if session.Status != sessions.StatusNeedsInput || session.Pending == nil {
		return permissionCard(*session, "")
	}
	info, err := daemon.ReadInfo(t.agentIn)
	if err != nil || info == nil {
		card := permissionCard(*session, "")
		card.Note = "corgi agent is not running"
		return card
	}
	if _, err := command.Write(t.agentIn, command.Command{Action: command.ActionAnswer, SessionID: sessionID, Answer: answer, Source: "telegram"}); err != nil {
		card := permissionCard(*session, "")
		card.Note = "could not queue that: " + err.Error()
		return card
	}
	daemon.Nudge(info)
	if !onBoard {
		return permissionCard(*session, answer)
	}
	card := boardCard(board, time.Now())
	for i := range card.Rows {
		if len(card.Rows[i].Buttons) > 0 && strings.HasSuffix(fmt.Sprint(card.Rows[i].Buttons[0][1]), ":"+sessionID) {
			card.Rows[i].Buttons, card.Rows[i].Word = nil, answerWord(answer)
		}
	}
	return card
}

func (t *telegramControl) cardForQuery(data string) (card telegramCard, persist bool) {
	parts := strings.SplitN(data, ":", 3)
	switch {
	case len(parts) == 3 && (parts[0] == "a" || parts[0] == "A"):
		return t.answerFromCard(parts[1], parts[2], parts[0] == "A"), parts[0] == "a"
	case len(parts) == 2 && parts[0] == "p":
		return t.answerFromCard("", parts[1], false), false
	}
	card, known := t.cardByName(data)
	if !known {
		return telegramCard{Title: "corgi", Note: "this card is from an older corgi"}, false
	}
	return card, false
}

func (t *telegramControl) handleWidgetQuery(ctx context.Context, raw json.RawMessage) {
	var query telegramWidgetQuery
	if json.Unmarshal(raw, &query) != nil {
		return
	}
	id := strings.Trim(string(query.ID), `"`)
	if id == "" {
		return
	}
	answer := map[string]any{"widget_query_id": id}
	if query.Chat != nil && fmt.Sprint(query.Chat.ID) == t.chatID {
		card, persist := t.cardForQuery(query.Data)
		state := card.fitted()
		answer["data"] = state
		if persist && query.MessageID != 0 {
			t.callTelegram(ctx, "editMessageWidgetData", map[string]any{
				"chat_id": t.chatID, "message_id": query.MessageID, "chat_data": state,
			})
		}
	}
	t.callTelegram(ctx, "answerWidgetQuery", answer)
}

func (t *telegramControl) sentCardsPath() string {
	return filepath.Join(t.agentIn, "telegram", "cards-sent-"+t.chatID+".json")
}

func (t *telegramControl) loadSentCards() map[string]bool {
	sent := map[string]bool{}
	if data, err := os.ReadFile(t.sentCardsPath()); err == nil {
		_ = json.Unmarshal(data, &sent)
	}
	return sent
}

func (t *telegramControl) sendPendingCards(ctx context.Context) {
	if !t.cardsOn() {
		return
	}
	board, err := readBoard(t.agentIn)
	if err != nil {
		return
	}
	sent := t.loadSentCards()
	waiting := map[string]bool{}
	changed := false
	for _, s := range board.Sessions {
		if s.Status != sessions.StatusNeedsInput || s.Pending == nil {
			continue
		}
		key := fmt.Sprintf("%s|%d", s.ID, s.Pending.At.Unix())
		waiting[key] = true
		if sent[key] {
			continue
		}
		t.sendCard(ctx, permissionCard(s, ""))
		sent[key] = true
		changed = true
	}
	for key := range sent {
		if !waiting[key] {
			delete(sent, key)
			changed = true
		}
	}
	if !changed {
		return
	}
	if data, err := json.Marshal(sent); err == nil {
		_ = os.WriteFile(t.sentCardsPath(), data, 0o600)
	}
}

func (t *telegramControl) watchForPendingCards(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(telegramCardWatchEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				t.sendPendingCards(ctx)
			}
		}
	}()
	return done
}
