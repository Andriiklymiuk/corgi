package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils/agent/pairing"
)

// A browser on the machine running the daemon needs no pairing code: whoever
// can run the command can already read the daemon's files. It still gets a
// real, revocable device of its own rather than a shared key.
func TestLocalPairingMintsARevocableDevice(t *testing.T) {
	store := filepath.Join(t.TempDir(), "devices.json")

	token, err := pairing.PairLocal(store, "this-laptop")
	if err != nil {
		t.Fatal(err)
	}
	if len(token) < 20 {
		t.Fatalf("a device token has to be worth having: %q", token)
	}
	saved, err := pairing.Load(store)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Devices) != 1 || saved.Devices[0].Name != "this-laptop" {
		t.Fatalf("the device must be on record so it can be revoked: %+v", saved.Devices)
	}
	if strings.Contains(string(mustRead(t, store)), token) {
		t.Fatal("the store keeps a hash, never the token itself")
	}

	// Pairing again replaces it, which is what someone re-running this wants.
	second, err := pairing.PairLocal(store, "this-laptop")
	if err != nil {
		t.Fatal(err)
	}
	if second == token {
		t.Fatal("a new pairing is a new key")
	}
	saved, _ = pairing.Load(store)
	if len(saved.Devices) != 1 {
		t.Fatalf("re-pairing replaces rather than piles up: %+v", saved.Devices)
	}

	// A phone paired separately is untouched by any of it.
	if _, err := pairing.PairLocal(store, "my-phone"); err != nil {
		t.Fatal(err)
	}
	saved, _ = pairing.Load(store)
	if len(saved.Devices) != 2 {
		t.Fatalf("other devices keep working: %+v", saved.Devices)
	}

	for _, bad := range []string{"", "   ", strings.Repeat("x", 65), "boom\x1b[2J"} {
		if _, err := pairing.PairLocal(store, bad); err == nil {
			t.Errorf("device name %q must be refused", bad)
		}
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
