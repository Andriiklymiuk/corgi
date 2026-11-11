package cmd

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/usage"
)

type widgetCall struct {
	Method string
	Body   map[string]any
}

type fakeWidgetTelegram struct {
	mu     sync.Mutex
	calls  []widgetCall
	refuse map[string]bool
}

func newFakeWidgetTelegram(t *testing.T) *fakeWidgetTelegram {
	t.Helper()
	f := &fakeWidgetTelegram{refuse: map[string]bool{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.calls = append(f.calls, widgetCall{Method: method, Body: body})
		if f.refuse[method] {
			_, _ = w.Write([]byte(`{"ok":false}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	t.Cleanup(srv.Close)
	previous := telegramAPIBase
	telegramAPIBase = srv.URL
	t.Cleanup(func() { telegramAPIBase = previous })
	return f
}

func (f *fakeWidgetTelegram) of(method string) []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []map[string]any
	for _, c := range f.calls {
		if c.Method == method {
			out = append(out, c.Body)
		}
	}
	return out
}

func cardControl(t *testing.T) *telegramControl {
	t.Helper()
	return &telegramControl{token: "1:x", chatID: "99", agentIn: t.TempDir(), client: &http.Client{Timeout: time.Second}}
}

func waitingSession() sessions.Session {
	return sessions.Session{ID: "0b8f6c1e-aaaa-bbbb-cccc-1234567890ab", Display: "acme-api", Status: sessions.StatusNeedsInput,
		Pending: &sessions.Pending{Tool: "Bash", Subject: "git push", Risk: "high", At: time.Unix(1700000000, 0)}}
}

func decodeCard(t *testing.T, raw any) telegramCard {
	t.Helper()
	var card telegramCard
	if err := json.Unmarshal([]byte(raw.(string)), &card); err != nil {
		t.Fatalf("card state is not JSON: %v in %v", err, raw)
	}
	return card
}

func TestTelegramCardHTMLFitsTheWidgetLimits(t *testing.T) {
	if len(telegramCardHTML) > 32768 {
		t.Errorf("html is %d bytes, the limit is 32768", len(telegramCardHTML))
	}
	if strings.ContainsRune(telegramCardHTML, 0) {
		t.Error("html must not hold a NUL")
	}
	var packed bytes.Buffer
	writer, _ := zlib.NewWriterLevel(&packed, 9)
	_, _ = writer.Write([]byte(telegramCardHTML))
	_ = writer.Close()
	if packed.Len() > 4096 {
		t.Errorf("html is %d bytes compressed, the limit is 4096", packed.Len())
	}
	for _, banned := range []string{"fetch(", "XMLHttpRequest", "<input", "innerHTML"} {
		if strings.Contains(telegramCardHTML, banned) {
			t.Errorf("html uses %s, which the widget sandbox refuses or we avoid", banned)
		}
	}
}

func TestPermissionCardOffersTheThreeAnswers(t *testing.T) {
	card := permissionCard(waitingSession(), "")
	if card.Title != "acme-api" || card.Subtitle != "needs you" {
		t.Errorf("title %q subtitle %q", card.Title, card.Subtitle)
	}
	row := card.Rows[0]
	if row.Name != "Bash" || row.Detail != "git push" || row.Word != "risk high" {
		t.Errorf("row = %+v", row)
	}
	if len(row.Buttons) != 3 || row.Buttons[2][1] != "a:allow:0b8f6c1e-aaaa-bbbb-cccc-1234567890ab" {
		t.Errorf("buttons = %v", row.Buttons)
	}
	for _, b := range row.Buttons {
		if len(b[1].(string)) > telegramCardQueryLimit {
			t.Errorf("query %q is over the 64 byte limit", b[1])
		}
	}
}

func TestPermissionCardAfterAnAnswerHasNoButtons(t *testing.T) {
	for answer, word := range map[string]string{"allow": "allowed", "always": "always allowed", "deny": "denied"} {
		card := permissionCard(waitingSession(), answer)
		if card.Subtitle != word || len(card.Rows[0].Buttons) != 0 {
			t.Errorf("%s: subtitle %q buttons %v", answer, card.Subtitle, card.Rows[0].Buttons)
		}
	}
	working := waitingSession()
	working.Status, working.Pending = sessions.StatusWorking, nil
	card := permissionCard(working, "")
	if card.Rows[0].Word != "answered" || len(card.Rows[0].Buttons) != 0 {
		t.Errorf("a session that no longer waits must show answered: %+v", card.Rows[0])
	}
}

func TestPermissionCardDropsButtonsForAnIdTooLongToQuery(t *testing.T) {
	long := waitingSession()
	long.ID = strings.Repeat("x", 70)
	card := permissionCard(long, "")
	if len(card.Rows[0].Buttons) != 0 || card.Refresh != "" {
		t.Errorf("a 70 byte id cannot fit a 64 byte query: %+v", card)
	}
}

func TestBoardCardListsLiveSessionsAndButtonsOnTheWaitingOne(t *testing.T) {
	board := boardReport{}
	board.Sessions = []sessions.Session{
		waitingSession(),
		{ID: "w", Display: "web", Status: sessions.StatusWorking, Detail: "Bash npm test"},
		{ID: "g", Display: "old", Status: sessions.StatusGone},
	}
	card := boardCard(board, time.Now())
	if len(card.Rows) != 2 {
		t.Fatalf("gone sessions are left out, got %d rows", len(card.Rows))
	}
	if len(card.Rows[0].Buttons) != 3 || !strings.HasPrefix(card.Rows[0].Buttons[0][1].(string), "A:deny:") {
		t.Errorf("waiting row buttons = %v", card.Rows[0].Buttons)
	}
	if card.Rows[1].Detail != "Bash npm test" || len(card.Rows[1].Buttons) != 0 {
		t.Errorf("working row = %+v", card.Rows[1])
	}
	if empty := boardCard(boardReport{}, time.Now()); empty.Note != "no sessions on the board" {
		t.Errorf("empty note = %q", empty.Note)
	}
}

func TestUsageCardDrawsBothWindows(t *testing.T) {
	rep := usageReport{At: time.Now(), Accounts: []sessions.Account{
		{Profile: "personal", Limits: &usage.Limits{FiveHour: usage.Window{Percent: 41}, SevenDay: usage.Window{Percent: 12}}},
		{Profile: "work"},
	}}
	card := usageCard(rep)
	if len(card.Rows) != 3 || *card.Rows[0].Percent != 41 || *card.Rows[1].Percent != 12 {
		t.Fatalf("rows = %+v", card.Rows)
	}
	if card.Rows[2].Word != "no /usage snapshot yet" {
		t.Errorf("an account without limits says so: %+v", card.Rows[2])
	}
	if usageCard(usageReport{}).Note != "no accounts known yet" {
		t.Error("an empty report needs a note")
	}
}

func TestLevelsCardNamesTheNextCommand(t *testing.T) {
	card := levelsCard([]factoryReport{{Workspace: "api", Level: 3, Name: "It drafts, you review"}, {Workspace: "docs", Level: 5, Name: "Lights out"}})
	if card.Rows[0].Copy != "corgi agent level 4 --workspace api" || *card.Rows[0].Percent != 60 {
		t.Errorf("api row = %+v", card.Rows[0])
	}
	if card.Rows[1].Copy != "" {
		t.Errorf("the top level has no next command: %+v", card.Rows[1])
	}
	if levelsCard(nil).Note != "no registered workspaces" {
		t.Error("no workspaces needs a note")
	}
}

func TestAwayCardGroupsWhatHappenedAndOnlyLinksHTTPS(t *testing.T) {
	rep := awayReport{Since: time.Now().Add(-time.Hour),
		Opened:   []awayLine{{Ref: "ABC-7", What: "opened 1 PR", URL: "https://github.com/acme/api/pull/415"}},
		Failed:   []awayLine{{Ref: "ABC-9", What: "no GITLAB_TOKEN", URL: "http://plain.example"}},
		Deferred: map[string]string{"ABC-11": "acme"}}
	card := awayCard(rep)
	if card.Rows[0].Word != "opened" || card.Rows[0].URL == "" {
		t.Errorf("opened row = %+v", card.Rows[0])
	}
	if card.Rows[1].Word != "failed" || card.Rows[1].URL != "" {
		t.Errorf("a non-https link must not become a button: %+v", card.Rows[1])
	}
	if !strings.Contains(card.Note, "1 waiting for a free slot") {
		t.Errorf("note = %q", card.Note)
	}
	if awayCard(awayReport{}).Note != "nothing arrived" {
		t.Error("an empty night needs a note")
	}
}

func TestFittedCardStaysUnderTheStateLimit(t *testing.T) {
	card := telegramCard{Title: "Sessions", Refresh: "b"}
	for i := 0; i < 40; i++ {
		card.Rows = append(card.Rows, telegramCardRow{Name: "session-with-a-long-name", Word: "working", Detail: strings.Repeat("d", 60)})
	}
	state := card.fitted()
	if len(state) > telegramCardStateLimit {
		t.Fatalf("state is %d bytes", len(state))
	}
	fitted := decodeCard(t, state)
	if len(fitted.Rows) == 0 || !strings.Contains(fitted.Note, "more") {
		t.Errorf("rows %d note %q", len(fitted.Rows), fitted.Note)
	}
}

func TestShortenForCardCutsOnRunes(t *testing.T) {
	if got := shortenForCard("  a   b  ", 10); got != "a b" {
		t.Errorf("got %q", got)
	}
	if got := shortenForCard("привет мир", 6); got != "приве…" {
		t.Errorf("got %q", got)
	}
}

func TestCardsSwitchIsOffUntilTurnedOn(t *testing.T) {
	fake := newFakeTelegram(t)
	control := cardControl(t)
	if control.cardsOn() {
		t.Fatal("cards must start off")
	}
	control.handle("/cards on", "")
	if !control.cardsOn() || !strings.Contains(fake.last(), "cards on") {
		t.Errorf("on: %v %q", control.cardsOn(), fake.last())
	}
	control.handle("/cards", "")
	if !strings.Contains(fake.last(), "cards are on") {
		t.Errorf("status = %q", fake.last())
	}
	control.handle("/cards off", "")
	control.handle("/cards off", "")
	if control.cardsOn() || !strings.Contains(fake.last(), "cards off") {
		t.Errorf("off: %v %q", control.cardsOn(), fake.last())
	}
	control.handle("/card nonsense", "")
	if !strings.Contains(fake.last(), "/card board") {
		t.Errorf("an unknown card lists the cards: %q", fake.last())
	}
	if (&telegramControl{}).cardsOn() {
		t.Error("no agent dir, no cards")
	}
}

func TestCardCommandSendsAWidgetAndFallsBackToText(t *testing.T) {
	fake := newFakeWidgetTelegram(t)
	control := cardControl(t)
	writeBoardState(t, control.agentIn, sessions.State{Sessions: []sessions.Session{waitingSession()}})
	for _, name := range []string{"board", "usage", "levels", "morning"} {
		control.handle("/card "+name, "")
	}
	widgets := fake.of("sendWidget")
	if len(widgets) != 4 {
		t.Fatalf("sent %d widgets", len(widgets))
	}
	if widgets[0]["allow_queries"] != true || widgets[0]["html"] != telegramCardHTML || widgets[0]["chat_id"] != "99" {
		t.Errorf("widget payload = %v", widgets[0])
	}
	if decodeCard(t, widgets[0]["chat_data"]).Title != "Sessions" {
		t.Errorf("first card = %v", widgets[0]["chat_data"])
	}
	fake.refuse["sendWidget"] = true
	control.handle("/card board", "")
	texts := fake.of("sendMessage")
	if len(texts) != 1 || !strings.Contains(texts[0]["text"].(string), "refused the card") {
		t.Errorf("fallback = %v", texts)
	}
}

func TestWidgetQueryAllowQueuesTheAnswerAndRedrawsTheCard(t *testing.T) {
	fake := newFakeWidgetTelegram(t)
	control := cardControl(t)
	session := waitingSession()
	writeBoardState(t, control.agentIn, sessions.State{Sessions: []sessions.Session{session}})
	fakeRunningDaemon(t, control.agentIn, false)

	control.handleWidgetQuery(context.Background(), json.RawMessage(`{"id":"q1","data":"a:allow:`+session.ID+`","chat":{"id":99},"message_id":7}`))

	answers := fake.of("answerWidgetQuery")
	if len(answers) != 1 || answers[0]["widget_query_id"] != "q1" {
		t.Fatalf("answers = %v", answers)
	}
	if card := decodeCard(t, answers[0]["data"]); card.Subtitle != "allowed" || len(card.Rows[0].Buttons) != 0 {
		t.Errorf("card after allow = %+v", card)
	}
	edits := fake.of("editMessageWidgetData")
	if len(edits) != 1 || edits[0]["message_id"] != float64(7) {
		t.Errorf("the card in the chat must be rewritten: %v", edits)
	}
	queued, _ := os.ReadDir(control.agentIn + "/commands")
	if len(queued) != 1 {
		t.Errorf("one answer command must be queued, got %d", len(queued))
	}
}

func TestWidgetQueryFromTheBoardCardKeepsTheBoard(t *testing.T) {
	fake := newFakeWidgetTelegram(t)
	control := cardControl(t)
	session := waitingSession()
	writeBoardState(t, control.agentIn, sessions.State{Sessions: []sessions.Session{session}})
	fakeRunningDaemon(t, control.agentIn, false)

	control.handleWidgetQuery(context.Background(), json.RawMessage(`{"id":2,"data":"A:deny:`+session.ID+`","chat":{"id":99}}`))

	answers := fake.of("answerWidgetQuery")
	if len(answers) != 1 || answers[0]["widget_query_id"] != "2" {
		t.Fatalf("a numeric id is read too: %v", answers)
	}
	card := decodeCard(t, answers[0]["data"])
	if card.Title != "Sessions" || card.Rows[0].Word != "denied" || len(card.Rows[0].Buttons) != 0 {
		t.Errorf("board after deny = %+v", card)
	}
	if len(fake.of("editMessageWidgetData")) != 0 {
		t.Error("the board card refreshes itself, it is not rewritten")
	}
}

func TestWidgetQueryRefusesWhatItCannotDo(t *testing.T) {
	fake := newFakeWidgetTelegram(t)
	control := cardControl(t)
	session := waitingSession()
	writeBoardState(t, control.agentIn, sessions.State{Sessions: []sessions.Session{session}})

	ask := func(data string) telegramCard {
		control.handleWidgetQuery(context.Background(), json.RawMessage(`{"id":"q","data":"`+data+`","chat":{"id":99}}`))
		answers := fake.of("answerWidgetQuery")
		return decodeCard(t, answers[len(answers)-1]["data"])
	}
	if card := ask("a:allow:" + session.ID); card.Note != "corgi agent is not running" || len(card.Rows[0].Buttons) != 3 {
		t.Errorf("no daemon: %+v", card)
	}
	if card := ask("a:allow:nobody"); card.Note != "that session is gone" {
		t.Errorf("unknown session: %+v", card)
	}
	if card := ask("a:rm-rf:" + session.ID); len(card.Rows[0].Buttons) != 3 {
		t.Errorf("an unknown answer changes nothing: %+v", card)
	}
	if card := ask("p:" + session.ID); card.Subtitle != "needs you" {
		t.Errorf("refresh: %+v", card)
	}
	if card := ask("zzz"); !strings.Contains(card.Note, "older corgi") {
		t.Errorf("unknown query: %+v", card)
	}
	if card := ask("u"); card.Title != "Usage" {
		t.Errorf("usage refresh: %+v", card)
	}
	queued, _ := os.ReadDir(control.agentIn + "/commands")
	if len(queued) != 0 {
		t.Errorf("nothing may be queued, got %d", len(queued))
	}
}

func TestWidgetQueryFromAnotherChatGetsNoData(t *testing.T) {
	fake := newFakeWidgetTelegram(t)
	control := cardControl(t)
	control.handleWidgetQuery(context.Background(), json.RawMessage(`{"id":"q","data":"b","chat":{"id":5}}`))
	control.handleWidgetQuery(context.Background(), json.RawMessage(`{"id":"q2","data":"b"}`))
	control.handleWidgetQuery(context.Background(), json.RawMessage(`{"data":"b"}`))
	control.handleWidgetQuery(context.Background(), json.RawMessage(`not json`))
	answers := fake.of("answerWidgetQuery")
	if len(answers) != 2 {
		t.Fatalf("each query with an id is answered once: %v", answers)
	}
	for _, a := range answers {
		if _, has := a["data"]; has {
			t.Errorf("a foreign or forwarded query must get no board data: %v", a)
		}
	}
}

func TestPollRoutesWidgetQueriesAndKeepsTextWorking(t *testing.T) {
	fake := newFakeWidgetTelegram(t)
	control := cardControl(t)
	srvUpdates := `{"ok":true,"result":[{"update_id":4,"widget_query":{"id":"q9","data":"b","chat":{"id":99}}},{"update_id":5,"message":{"text":"/help","chat":{"id":99}}}]}`
	updates := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/getUpdates") {
			_, _ = w.Write([]byte(srvUpdates))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(updates.Close)
	widgetBase := telegramAPIBase
	telegramAPIBase = updates.URL
	texts := control.poll(context.Background())
	telegramAPIBase = widgetBase
	if len(texts) != 1 || texts[0].Text != "/help" || control.offset != 6 {
		t.Errorf("texts %v offset %d", texts, control.offset)
	}
	_ = fake
}

func TestSendPendingCardsSendsEachPromptOnce(t *testing.T) {
	fake := newFakeWidgetTelegram(t)
	control := cardControl(t)
	session := waitingSession()
	writeBoardState(t, control.agentIn, sessions.State{Sessions: []sessions.Session{session}})

	control.sendPendingCards(context.Background())
	if len(fake.of("sendWidget")) != 0 {
		t.Fatal("cards are off: nothing may be sent")
	}
	if err := control.setCards(true); err != nil {
		t.Fatal(err)
	}
	control.sendPendingCards(context.Background())
	control.sendPendingCards(context.Background())
	if got := len(fake.of("sendWidget")); got != 1 {
		t.Fatalf("one prompt, one card, got %d", got)
	}

	session.Pending = &sessions.Pending{Tool: "Bash", Subject: "rm x", At: time.Unix(1700000900, 0)}
	writeBoardState(t, control.agentIn, sessions.State{Sessions: []sessions.Session{session}})
	control.sendPendingCards(context.Background())
	if got := len(fake.of("sendWidget")); got != 2 {
		t.Errorf("a new prompt sends a new card, got %d", got)
	}

	session.Status, session.Pending = sessions.StatusWorking, nil
	writeBoardState(t, control.agentIn, sessions.State{Sessions: []sessions.Session{session}})
	control.sendPendingCards(context.Background())
	if left := control.loadSentCards(); len(left) != 0 {
		t.Errorf("answered prompts are forgotten: %v", left)
	}
}

func TestSendPendingCardsDoesNotRetryARefusedCard(t *testing.T) {
	fake := newFakeWidgetTelegram(t)
	fake.refuse["sendWidget"] = true
	control := cardControl(t)
	writeBoardState(t, control.agentIn, sessions.State{Sessions: []sessions.Session{waitingSession()}})
	_ = control.setCards(true)
	control.sendPendingCards(context.Background())
	control.sendPendingCards(context.Background())
	if got := len(fake.of("sendWidget")); got != 1 {
		t.Errorf("a refused card is not sent again every tick, got %d", got)
	}
}

func TestCardWatcherStopsWithItsContext(t *testing.T) {
	newFakeWidgetTelegram(t)
	previous := telegramCardWatchEvery
	telegramCardWatchEvery = 5 * time.Millisecond
	t.Cleanup(func() { telegramCardWatchEvery = previous })
	control := cardControl(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := control.watchForPendingCards(ctx)
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the watcher must stop when its context ends")
	}
}

func TestCallTelegramReportsFailure(t *testing.T) {
	control := cardControl(t)
	previous := telegramAPIBase
	t.Cleanup(func() { telegramAPIBase = previous })
	telegramAPIBase = "://bad"
	if control.callTelegram(context.Background(), "sendWidget", map[string]any{}) {
		t.Error("a broken base cannot succeed")
	}
	telegramAPIBase = "http://127.0.0.1:1"
	if control.callTelegram(context.Background(), "sendWidget", map[string]any{}) {
		t.Error("an unreachable host cannot succeed")
	}
	if control.callTelegram(context.Background(), "sendWidget", map[string]any{"bad": func() {}}) {
		t.Error("an unmarshalable payload cannot succeed")
	}
}
