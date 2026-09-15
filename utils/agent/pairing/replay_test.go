package pairing

import (
	"testing"
	"time"
)

func TestAReplayGuardRemembersANonceForTheSkewWindow(t *testing.T) {
	g := NewReplayGuard()
	now := time.Now()
	if g.Seen("abc", now) {
		t.Fatal("first delivery is new")
	}
	if !g.Seen("abc", now.Add(time.Second)) {
		t.Fatal("the same nonce a second later is a replay")
	}
	if g.Seen("", now) || g.Seen("", now) {
		t.Fatal("an empty nonce is never remembered")
	}
	if g.Seen("abc", now.Add(3*e2eSkew)) {
		t.Fatal("a nonce older than the window is dropped, not matched")
	}
}

func TestEnvelopeNonceReadsASealedMessage(t *testing.T) {
	key := make([]byte, 32)
	sealed, err := Seal(key, "POST", "/launch/send", []byte(`{}`), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if EnvelopeNonce(sealed) == "" {
		t.Fatal("a sealed message carries its nonce")
	}
	if EnvelopeNonce([]byte(`not json`)) != "" {
		t.Fatal("anything else has none")
	}
}
