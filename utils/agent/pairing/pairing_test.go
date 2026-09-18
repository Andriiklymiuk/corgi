package pairing

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func storeIn(t *testing.T) string {
	t.Helper()
	return StorePath(t.TempDir())
}

func sessionAt(t *testing.T, clock *time.Time) (*Session, string) {
	t.Helper()
	s, code, err := NewSession()
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return *clock }
	s.expires = clock.Add(CodeTTL)
	return s, code
}

func TestPairIssuesATokenAndRecordsTheDevice(t *testing.T) {
	path := storeIn(t)
	now := time.Now()
	session, code := sessionAt(t, &now)

	token, err := Pair(path, session, code, "andrii-iphone")
	if err != nil {
		t.Fatalf("Pair() = %v", err)
	}

	if !strings.HasPrefix(token, TokenPrefix) {
		t.Errorf("token = %q, want the %s prefix", token, TokenPrefix)
	}
	store, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Devices) != 1 || store.Devices[0].Name != "andrii-iphone" {
		t.Fatalf("store = %+v", store.Devices)
	}
	if name, ok := store.Authorize(token); !ok || name != "andrii-iphone" {
		t.Errorf("Authorize() = %q, %v; want the paired device", name, ok)
	}
}

func TestCodeCannotBeReplayed(t *testing.T) {
	path := storeIn(t)
	now := time.Now()
	session, code := sessionAt(t, &now)

	if _, err := Pair(path, session, code, "first"); err != nil {
		t.Fatal(err)
	}

	_, err := Pair(path, session, code, "attacker")
	if err == nil {
		t.Fatal("a used pairing code must not pair a second device")
	}
	if !strings.Contains(err.Error(), "already been used") {
		t.Errorf("error should say why, got %q", err)
	}
	store, _ := Load(path)
	if len(store.Devices) != 1 {
		t.Errorf("devices = %d, want only the first", len(store.Devices))
	}
}

func TestCodeExpires(t *testing.T) {
	path := storeIn(t)
	now := time.Now()
	session, code := sessionAt(t, &now)

	now = now.Add(CodeTTL + time.Second)

	if _, err := Pair(path, session, code, "late"); err == nil {
		t.Fatal("an expired code must be refused: a code left on screen is otherwise a standing invitation")
	}
	if session.Open() {
		t.Error("an expired session should not report itself open")
	}
}

func TestCodeIsValidRightUpToExpiry(t *testing.T) {
	path := storeIn(t)
	now := time.Now()
	session, code := sessionAt(t, &now)

	now = now.Add(CodeTTL - time.Millisecond)

	if _, err := Pair(path, session, code, "just-in-time"); err != nil {
		t.Errorf("a code inside its window must work, got %v", err)
	}
}

func TestWrongCodesCloseTheWindow(t *testing.T) {
	path := storeIn(t)
	now := time.Now()
	session, code := sessionAt(t, &now)

	for i := range MaxAttempts {
		if _, err := Pair(path, session, "WRONGWRONG", "guesser"); err == nil {
			t.Fatalf("attempt %d: a wrong code must be refused", i)
		}
	}

	if session.Open() {
		t.Error("the window must close after repeated wrong codes")
	}
	if _, err := Pair(path, session, code, "legitimate"); err == nil {
		t.Fatal("even the correct code must be refused once the window has closed")
	}
}

func TestNormalizeCodeIgnoresTypingNoise(t *testing.T) {
	now := time.Now()
	session, code := sessionAt(t, &now)

	spaced := strings.ToLower(code[:5] + "-" + code[5:])

	if err := session.Redeem(spaced); err != nil {
		t.Errorf("a code typed with a separator and in lower case should work, got %v", err)
	}
}

func TestNewCodeIsUnambiguousAndUnique(t *testing.T) {
	seen := map[string]bool{}
	for range 200 {
		code, err := NewCode()
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != codeLength {
			t.Fatalf("code %q has length %d, want %d", code, len(code), codeLength)
		}
		if strings.ContainsAny(code, "ILOU") {
			t.Errorf("code %q contains an ambiguous character", code)
		}
		if seen[code] {
			t.Fatalf("duplicate code %q in 200 draws", code)
		}
		seen[code] = true
	}
}

func TestTokensAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for range 200 {
		token, err := NewDeviceToken()
		if err != nil {
			t.Fatal(err)
		}
		if seen[token] {
			t.Fatal("duplicate device token")
		}
		seen[token] = true
	}
}

func TestStoreHoldsHashesNotTokens(t *testing.T) {
	path := storeIn(t)
	now := time.Now()
	session, code := sessionAt(t, &now)

	token, err := Pair(path, session, code, "phone")
	if err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), token) {
		t.Fatal("the device token was written to disk in the clear")
	}
	if !strings.Contains(string(raw), HashToken(token)) {
		t.Error("the store should hold the token's hash")
	}
}

func TestStoreIsOwnerOnly(t *testing.T) {
	path := storeIn(t)
	now := time.Now()
	session, code := sessionAt(t, &now)

	if _, err := Pair(path, session, code, "phone"); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		t.Errorf("mode = %04o; the store names every device with access", mode)
	}
}

func TestLoadRefusesAWorldReadableStore(t *testing.T) {
	path := storeIn(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"devices":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("a store readable by other users must be refused")
	}
}

func TestAuthorizeRejectsUnknownTokens(t *testing.T) {
	path := storeIn(t)
	now := time.Now()
	session, code := sessionAt(t, &now)
	if _, err := Pair(path, session, code, "phone"); err != nil {
		t.Fatal(err)
	}
	store, _ := Load(path)

	for _, bad := range []string{"", "nonsense", TokenPrefix + "wrong", "corgi_mcp_something"} {
		if _, ok := store.Authorize(bad); ok {
			t.Errorf("Authorize(%q) accepted an unknown token", bad)
		}
	}
}

func TestRevokeAffectsOnlyThatDevice(t *testing.T) {
	path := storeIn(t)
	var tokens []string
	for _, name := range []string{"phone", "tablet", "laptop"} {
		now := time.Now()
		session, code := sessionAt(t, &now)
		token, err := Pair(path, session, code, name)
		if err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, token)
	}

	store, _ := Load(path)
	if !store.Revoke("tablet") {
		t.Fatal("Revoke should report success")
	}
	if err := Save(path, store); err != nil {
		t.Fatal(err)
	}

	reloaded, _ := Load(path)
	if _, ok := reloaded.Authorize(tokens[1]); ok {
		t.Error("the revoked device's token still works")
	}
	for _, i := range []int{0, 2} {
		if _, ok := reloaded.Authorize(tokens[i]); !ok {
			t.Errorf("revoking one device invalidated another (%d)", i)
		}
	}
}

func TestRevokeIsCaseInsensitiveAndReportsMisses(t *testing.T) {
	store := &Store{Devices: []Device{{Name: "Phone"}}}

	if !store.Revoke("phone") {
		t.Error("Revoke should match case-insensitively")
	}
	if store.Revoke("phone") {
		t.Error("Revoke should report false when nothing matched")
	}
}

func TestRePairingReplacesTheOldToken(t *testing.T) {
	path := storeIn(t)
	now := time.Now()

	s1, c1 := sessionAt(t, &now)
	old, err := Pair(path, s1, c1, "phone")
	if err != nil {
		t.Fatal(err)
	}
	s2, c2 := sessionAt(t, &now)
	fresh, err := Pair(path, s2, c2, "phone")
	if err != nil {
		t.Fatal(err)
	}

	store, _ := Load(path)
	if len(store.Devices) != 1 {
		t.Fatalf("devices = %d, want 1", len(store.Devices))
	}
	if _, ok := store.Authorize(old); ok {
		t.Error("the previous token for that device must stop working")
	}
	if _, ok := store.Authorize(fresh); !ok {
		t.Error("the new token should work")
	}
}

func TestPairRequiresADeviceName(t *testing.T) {
	path := storeIn(t)
	now := time.Now()

	for _, name := range []string{"", "   "} {
		session, code := sessionAt(t, &now)
		if _, err := Pair(path, session, code, name); err == nil {
			t.Errorf("device name %q should be refused", name)
		}
	}

	session, code := sessionAt(t, &now)
	if _, err := Pair(path, session, code, strings.Repeat("x", 65)); err == nil {
		t.Error("an over-long device name should be refused")
	}
}

func TestABadDeviceNameDoesNotBurnTheCode(t *testing.T) {
	path := storeIn(t)
	now := time.Now()
	session, code := sessionAt(t, &now)

	if _, err := Pair(path, session, code, ""); err == nil {
		t.Fatal("expected the empty name to be refused")
	}
	if _, err := Pair(path, session, code, "phone"); err != nil {
		t.Errorf("the code should still be usable after a rejected name, got %v", err)
	}
}

func TestNilSessionIsClosed(t *testing.T) {
	var s *Session
	if s.Open() {
		t.Error("a nil session must not report itself open")
	}
	if err := s.Redeem("anything"); err == nil {
		t.Error("a nil session must refuse to redeem")
	}
	s.Close()
}

func TestCloseEndsTheWindow(t *testing.T) {
	now := time.Now()
	session, code := sessionAt(t, &now)

	session.Close()

	if session.Open() {
		t.Error("a closed session must not report itself open")
	}
	if err := session.Redeem(code); err == nil {
		t.Error("a closed session must refuse the correct code")
	}
}

func TestLoadMissingStoreIsEmpty(t *testing.T) {
	store, err := Load(storeIn(t))

	if err != nil {
		t.Fatalf("a first run must not error, got %v", err)
	}
	if len(store.Devices) != 0 {
		t.Error("want an empty store")
	}
}

func TestConcurrentRedeemYieldsExactlyOneWinner(t *testing.T) {
	now := time.Now()
	session, code := sessionAt(t, &now)

	results := make(chan error, 8)
	for range 8 {
		go func() { results <- session.Redeem(code) }()
	}

	wins := 0
	for range 8 {
		if err := <-results; err == nil {
			wins++
		}
	}
	if wins != 1 {
		t.Errorf("%d concurrent redemptions succeeded, want exactly 1", wins)
	}
}

func TestCallerFacingErrorsAreTagged(t *testing.T) {
	path := storeIn(t)
	now := time.Now()

	session, code := sessionAt(t, &now)
	if _, err := Pair(path, session, "WRONGWRONG", "phone"); !errors.Is(err, ErrBadRequest) {
		t.Errorf("a wrong code should be caller-facing, got %v", err)
	}

	session2, _ := sessionAt(t, &now)
	if _, err := Pair(path, session2, code, ""); !errors.Is(err, ErrBadRequest) {
		t.Errorf("a missing device name should be caller-facing, got %v", err)
	}

	session3, code3 := sessionAt(t, &now)
	now = now.Add(CodeTTL + time.Second)
	if _, err := Pair(path, session3, code3, "phone"); !errors.Is(err, ErrBadRequest) {
		t.Errorf("an expired code should be caller-facing, got %v", err)
	}
}

func TestPairRejectsAControlCharacterInTheDeviceName(t *testing.T) {
	path := storeIn(t)
	now := time.Now()
	session, code := sessionAt(t, &now)
	_, err := Pair(path, session, code, "phone\x1b[31mred")
	if err == nil {
		t.Fatal("a device name with an escape sequence must be rejected, or it rewrites `devices list` output")
	}
	if !errors.Is(err, ErrBadRequest) {
		t.Errorf("must be a caller-input error, got %v", err)
	}
}

func TestExpiredDeviceDoesNotAuthorize(t *testing.T) {
	token, _ := NewDeviceToken()
	live, _ := NewDeviceToken()
	s := &Store{Devices: []Device{
		{Name: "old", TokenHash: HashToken(token), ExpiresAt: time.Now().Add(-time.Minute)},
		{Name: "live", TokenHash: HashToken(live), ExpiresAt: time.Now().Add(time.Hour)},
		{Name: "forever", TokenHash: HashToken("x")},
	}}
	if _, ok := s.Authorize(token); ok {
		t.Error("an expired device must not authorize")
	}
	if d, ok := s.FindByToken(token); !ok || d.Name != "old" {
		t.Error("FindByToken must still see the expired device")
	}
	if name, ok := s.Authorize(live); !ok || name != "live" {
		t.Error("an unexpired device must authorize")
	}
	if n := s.RevokeExpired(time.Now()); n != 1 || len(s.Devices) != 2 {
		t.Errorf("RevokeExpired dropped %d, kept %d", n, len(s.Devices))
	}
}

func TestLaunchCodeOpensAWindowOnThatCode(t *testing.T) {
	code, err := NewCode()
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSessionWithCode(strings.ToLower(code), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if s.Code() != code {
		t.Fatalf("code %q, want %q", s.Code(), code)
	}
	if left := time.Until(s.ExpiresAt()); left < 59*time.Minute || left > time.Hour {
		t.Fatalf("expires in %s, want about an hour", left)
	}
	if err := s.Redeem(code); err != nil {
		t.Fatal(err)
	}
}

func TestLaunchCodeTTLIsCappedAndDefaulted(t *testing.T) {
	code, _ := NewCode()
	s, _ := NewSessionWithCode(code, 0)
	if left := time.Until(s.ExpiresAt()); left > CodeTTL || left < CodeTTL-time.Minute {
		t.Fatalf("no ttl → %s, want %s", left, CodeTTL)
	}
	s, _ = NewSessionWithCode(code, 100*time.Hour)
	if left := time.Until(s.ExpiresAt()); left > MaxLaunchTTL {
		t.Fatalf("ttl %s not capped at %s", left, MaxLaunchTTL)
	}
}

func TestLaunchCodeMustBeAFullCode(t *testing.T) {
	for _, bad := range []string{"", "SHORT", strings.Repeat("A", 19), strings.Repeat("A", 21)} {
		if _, err := NewSessionWithCode(bad, 0); !errors.Is(err, ErrBadLaunchCode) {
			t.Fatalf("%q accepted: %v", bad, err)
		}
	}
}
