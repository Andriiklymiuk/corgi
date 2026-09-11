package push

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
