package pairing

import (
	"crypto/ecdh"
	"crypto/rand"
	"path/filepath"
	"testing"
	"time"
)

// The phone and the machine each hold one half; the key they derive is the
// same, a sealed body opens where it was sent and not elsewhere, and a copy
// replayed later is refused.
func TestSealOpensOnlyWhereAndWhenItWasSent(t *testing.T) {
	server, err := LoadOrCreateServerKey(filepath.Join(t.TempDir(), "e2e.key"))
	if err != nil {
		t.Fatal(err)
	}
	phone, _ := ecdh.X25519().GenerateKey(rand.Reader)
	onMachine, err := SharedKey(server, phone.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	onPhone, err := SharedKeyOnDevice(phone, server.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	if string(onMachine) != string(onPhone) || len(onMachine) != 32 {
		t.Fatal("both sides derive the one key")
	}
	now := time.Now()
	sealed, err := Seal(onPhone, "POST", "/launch/answer", []byte(`{"session":"s1","answer":"allow"}`), now)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := Open(onMachine, "POST", "/launch/answer", sealed, now.Add(3*time.Second))
	if err != nil || string(plain) != `{"session":"s1","answer":"allow"}` {
		t.Fatalf("opens where it was sent: %q %v", plain, err)
	}
	if _, err := Open(onMachine, "POST", "/launch/send", sealed, now); err == nil {
		t.Fatal("an allow must not open as a send")
	}
	if _, err := Open(onMachine, "POST", "/launch/answer", sealed, now.Add(3*time.Minute)); err == nil {
		t.Fatal("a copy replayed later is refused")
	}
	other, _ := ecdh.X25519().GenerateKey(rand.Reader)
	otherKey, _ := SharedKey(server, other.PublicKey())
	if _, err := Open(otherKey, "POST", "/launch/answer", sealed, now); err == nil {
		t.Fatal("another device's key does not open it")
	}
	if _, err := Open(onMachine, "POST", "/launch/answer", []byte(`{"session":"s1"}`), now); err == nil {
		t.Fatal("plaintext is not an envelope")
	}
}

// The machine's key is minted once and read back the same; a device that
// offers a public key is recorded with it, one that does not stays plain.
func TestPairingRecordsTheDeviceKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "e2e.key")
	a, _ := LoadOrCreateServerKey(path)
	b, _ := LoadOrCreateServerKey(path)
	if PublicKeyString(a.PublicKey()) != PublicKeyString(b.PublicKey()) {
		t.Fatal("the same key twice")
	}
	store := filepath.Join(dir, "devices.json")
	phone, _ := ecdh.X25519().GenerateKey(rand.Reader)
	session, code, _ := NewSession()
	token, err := PairWithKey(store, session, code, "phone", PublicKeyString(phone.PublicKey()))
	if err != nil {
		t.Fatal(err)
	}
	s, _ := Load(store)
	d, ok := s.AuthorizeDevice(token)
	if !ok || !d.Encrypted() || d.PubKey != PublicKeyString(phone.PublicKey()) {
		t.Fatalf("the device carries its key: %+v", d)
	}
	session2, code2, _ := NewSession()
	token2, err := Pair(store, session2, code2, "browser")
	if err != nil {
		t.Fatal(err)
	}
	s, _ = Load(store)
	if d, _ := s.AuthorizeDevice(token2); d.Encrypted() {
		t.Fatal("no key offered, none recorded")
	}
	session3, code3, _ := NewSession()
	if _, err := PairWithKey(store, session3, code3, "bad", "not-a-key"); err == nil {
		t.Fatal("a key that is not one is refused")
	}
}
