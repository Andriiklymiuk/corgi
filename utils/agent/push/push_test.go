package push

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Tokens are kept per device, replaced on re-register, dropped when Expo
// says the device is gone; a send is one request for every phone.
func TestTokensAreKeptPerDeviceAndDroppedWhenGone(t *testing.T) {
	dir := t.TempDir()
	s := Load(dir)
	if err := s.Set("phone", "not a token"); err == nil {
		t.Fatal("only Expo tokens")
	}
	if err := s.Set("phone", "ExponentPushToken[aaa]"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("phone", "ExponentPushToken[bbb]"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("ipad", "ExponentPushToken[ccc]"); err != nil {
		t.Fatal(err)
	}
	if got := Load(dir).List(); len(got) != 2 || got[0].Token != "ExponentPushToken[bbb]" {
		t.Fatalf("one token per device, newest wins: %+v", got)
	}

	var sent []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&sent)
		_, _ = w.Write([]byte(`{"data":[{"status":"ok"},{"status":"error","message":"gone","details":{"error":"DeviceNotRegistered"}}]}`))
	}))
	defer srv.Close()
	ExpoEndpoint = srv.URL
	if err := s.Send(context.Background(), Message{Title: "corgi · api", Body: "permission: Bash go test", Category: "permission", Data: map[string]string{"session": "s1"}, Thread: "s1"}); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 2 || sent[0]["categoryId"] != "permission" || sent[0]["channelId"] != "permission" || sent[0]["threadId"] != "s1" {
		t.Fatalf("sent: %+v", sent)
	}
	host, _ := os.Hostname()
	if data, _ := sent[0]["data"].(map[string]any); data["session"] != "s1" || data["laptop"] != host {
		t.Fatalf("the phone needs the session and which laptop asked: %+v", sent[0]["data"])
	}
	if got := Load(dir).List(); len(got) != 1 || got[0].Device != "phone" {
		t.Fatalf("the unregistered device is dropped: %+v", got)
	}
	if err := s.Remove("phone"); err != nil || len(Load(dir).List()) != 0 {
		t.Fatal("revoke drops the token")
	}
	if err := s.Send(context.Background(), Message{Title: "x"}); err != nil {
		t.Fatal("no tokens, nothing sent, no error")
	}
}

// A phone says what it wants to hear: a permission always gets through;
// in quiet hours only what needs a person does; with "only needs" the
// rest never does.
func TestAPhoneHearsWhatItAskedFor(t *testing.T) {
	q, err := ParseQuiet("23:00-07:00")
	if err != nil {
		t.Fatal(err)
	}
	at := func(h int) time.Time { return time.Date(2026, 9, 13, h, 30, 0, 0, time.Local) }
	if !q.Contains(at(23)) || !q.Contains(at(2)) || q.Contains(at(9)) {
		t.Fatal("a window across midnight")
	}
	if _, err := ParseQuiet("25:00-07:00"); err == nil {
		t.Fatal("a bad hour is refused")
	}
	night := Token{Quiet: "23:00-07:00"}
	perm := Message{Category: "permission"}
	news := Message{Category: "inbox", Body: "merged https://x", Data: map[string]string{}}
	needs := Message{Category: "inbox", Body: "build went red", Data: map[string]string{"needs": "1"}}
	if !night.wants(perm, at(2)) || night.wants(news, at(2)) || !night.wants(needs, at(2)) || !night.wants(news, at(10)) {
		t.Fatal("quiet hours")
	}
	only := Token{Only: "needs"}
	if !only.wants(perm, at(10)) || only.wants(news, at(10)) || !only.wants(needs, at(10)) {
		t.Fatal("only what needs me")
	}
	s := &Store{path: filepath.Join(t.TempDir(), "push.json")}
	if err := s.SetWith("iPhone", "ExponentPushToken[abc]", Prefs{Quiet: "22:00-06:00", Only: "needs"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetWith("iPhone", "ExponentPushToken[abc]", Prefs{Only: "everything"}); err == nil {
		t.Fatal("only takes needs or nothing")
	}
	if got := s.List(); len(got) != 1 || got[0].Quiet != "22:00-06:00" || got[0].Only != "needs" {
		t.Fatalf("kept: %+v", got)
	}
}
