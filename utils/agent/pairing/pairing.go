// Package pairing issues per-device tokens for corgi's MCP HTTP endpoint.
//
// A QR holding the server's bearer token would be a credential for the whole
// machine (the endpoint runs shell and queries databases), visible to anyone
// who sees the screen and unrevocable per device. Instead a short-lived,
// single-use code buys a per-device token, stored hashed and revocable alone.
package pairing

import (
	"andriiklymiuk/corgi/utils/atomicfile"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

// ErrBadRequest marks failures caused by the caller's own input — a wrong or
// expired code, a bad device name. Only these are safe to report back over the
// unauthenticated pairing endpoint; anything else names local paths.
var ErrBadRequest = errors.New("pairing request rejected")

// CodeTTL has to survive a code being read off a terminal, sent to a phone and
// opened there; the single-use rule and MaxAttempts are what bound guessing.
const CodeTTL = 10 * time.Minute

// MaxAttempts is how many wrong codes are tolerated before pairing closes.
// The code is high-entropy, so this is about shutting down noise, not about
// making a guess unlikely.
const MaxAttempts = 10

// TokenPrefix marks a device token in logs and config files.
const TokenPrefix = "corgi_dev_"

// codeAlphabet is Crockford base32 without I, L, O, U — unambiguous when read
// off a screen and typed by hand, which is the fallback when a camera fails.
const codeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// codeLength gives ~100 bits of entropy. The code rides in the QR / link
// fragment and is read by the pair page, so length costs the user nothing;
// hand-typing it is only the fallback when a camera fails.
const codeLength = 20

// Device is one paired client. Deliberately no "last seen": recording it means
// a read-modify-write on every authenticated request, and a concurrent
// `devices revoke` could then be undone by a touch that loaded before it.
type Device struct {
	Name      string    `json:"name"`
	TokenHash string    `json:"tokenHash"`
	CreatedAt time.Time `json:"createdAt"`
}

// Store is the set of paired devices.
type Store struct {
	Version int      `json:"version"`
	Devices []Device `json:"devices"`
}

const storeVersion = 1

// StorePath is where paired devices are recorded.
func StorePath(agentDir string) string { return filepath.Join(agentDir, "devices.json") }

// Load reads the device store. A missing file is an empty store.
//
// The file records token hashes, not tokens, but it still names every device
// with access — so a group- or world-readable one is refused, the same way ssh
// refuses a loose private key.
func Load(path string) (*Store, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return &Store{Version: storeVersion}, nil
	}
	if err != nil {
		return nil, err
	}
	// Skipped on Windows, where Go reports 0666 for every file: the check would
	// reject the store on every read, and every paired device would get a 401
	// with nothing the user could do about it.
	if runtime.GOOS != "windows" {
		if mode := info.Mode().Perm(); mode&0o077 != 0 {
			return nil, fmt.Errorf("%s is readable by other users (mode %04o) — run: chmod 600 %s",
				path, mode, path)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Store
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	if s.Version == 0 {
		s.Version = storeVersion
	}
	return &s, nil
}

// StoreState describes whether device tokens are in play.
type StoreState int

const (
	// StoreEmpty means no device has been paired.
	StoreEmpty StoreState = iota
	// StoreHasDevices means at least one device is paired.
	StoreHasDevices
	// StoreUnreadable means the store exists but could not be read — bad
	// permissions, or corrupt JSON.
	StoreUnreadable
)

// InspectStore reports the store's state.
//
// The unreadable case is distinguished deliberately. Collapsing it into "no
// devices" would let a chmod or a truncated file turn an authenticated endpoint
// back into an open one, which is the worst possible direction for that mistake
// to fail in.
func InspectStore(path string) StoreState {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return StoreEmpty
	}
	store, err := Load(path)
	if err != nil {
		return StoreUnreadable
	}
	if len(store.Devices) == 0 {
		return StoreEmpty
	}
	return StoreHasDevices
}

// Save writes the store with owner-only permissions, tmp-write then rename so a
// crash cannot truncate it.
func Save(path string, s *Store) error {
	s.Version = storeVersion
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	sort.Slice(s.Devices, func(i, j int) bool { return s.Devices[i].Name < s.Devices[j].Name })
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(path, data, 0o600)
}

// Find returns the device with the given name.
func (s *Store) Find(name string) (Device, bool) {
	for _, d := range s.Devices {
		if strings.EqualFold(d.Name, name) {
			return d, true
		}
	}
	return Device{}, false
}

// Revoke removes one device. Reports whether anything was removed.
func (s *Store) Revoke(name string) bool {
	for i := range s.Devices {
		if strings.EqualFold(s.Devices[i].Name, name) {
			s.Devices = append(s.Devices[:i], s.Devices[i+1:]...)
			return true
		}
	}
	return false
}

// Authorize reports whether token belongs to a paired device, and which.
//
// Every stored hash is compared even after a match, so the time taken does not
// reveal how far down the list a token sits.
func (s *Store) Authorize(token string) (string, bool) {
	if token == "" {
		return "", false
	}
	want := HashToken(token)
	matched := ""
	for _, d := range s.Devices {
		if subtle.ConstantTimeCompare([]byte(d.TokenHash), []byte(want)) == 1 {
			matched = d.Name
		}
	}
	return matched, matched != ""
}

// HashToken is what the store holds. A readable store is then not a usable
// credential, only a list of which devices exist.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// NewDeviceToken returns a fresh device token.
func NewDeviceToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("could not generate a device token: %w", err)
	}
	return TokenPrefix + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

// NewCode returns a fresh pairing code.
func NewCode() (string, error) {
	b := make([]byte, codeLength)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("could not generate a pairing code: %w", err)
	}
	out := make([]byte, codeLength)
	for i, v := range b {
		out[i] = codeAlphabet[int(v)%len(codeAlphabet)]
	}
	return string(out), nil
}

// NormalizeCode makes a typed code comparable: case and separators are noise.
func NormalizeCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(code)) {
		if strings.ContainsRune(codeAlphabet, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Session is one open pairing window. Zero value is closed.
type Session struct {
	mu       sync.Mutex
	code     string
	expires  time.Time
	attempts int
	used     bool
	now      func() time.Time // test seam
}

// NewSession opens a pairing window with a fresh code.
func NewSession() (*Session, string, error) {
	code, err := NewCode()
	if err != nil {
		return nil, "", err
	}
	s := &Session{now: time.Now}
	s.code = code
	s.expires = s.now().Add(CodeTTL)
	return s, code, nil
}

// Code returns the session's pairing code, for display on the machine itself.
func (s *Session) Code() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.code
}

// Open reports whether the window is still accepting attempts.
func (s *Session) Open() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.openLocked()
}

func (s *Session) openLocked() bool {
	return !s.used && s.attempts < MaxAttempts && s.now().Before(s.expires)
}

// Redeem consumes the code. A correct code can be redeemed exactly once; the
// window then closes, so a code observed in transit cannot be replayed.
func (s *Session) Redeem(offered string) error {
	if s == nil {
		return fmt.Errorf("%w: pairing is not open", ErrBadRequest)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	switch {
	case s.used:
		return fmt.Errorf("%w: that pairing code has already been used", ErrBadRequest)
	case s.attempts >= MaxAttempts:
		return fmt.Errorf("%w: too many attempts — restart corgi mcp to pair", ErrBadRequest)
	case !s.now().Before(s.expires):
		return fmt.Errorf("%w: that pairing code has expired — restart corgi mcp to pair", ErrBadRequest)
	}

	s.attempts++
	if subtle.ConstantTimeCompare([]byte(NormalizeCode(offered)), []byte(s.code)) != 1 {
		return fmt.Errorf("%w: that pairing code is not correct", ErrBadRequest)
	}
	s.used = true
	return nil
}

// Close ends the window early.
func (s *Session) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.used = true
}

// Pair validates the code and records a new device, returning its token.
// The token is returned once and never stored in the clear.
func Pair(storePath string, session *Session, code, deviceName string) (string, error) {
	deviceName = strings.TrimSpace(deviceName)
	if deviceName == "" {
		return "", fmt.Errorf("%w: a device name is required", ErrBadRequest)
	}
	if len(deviceName) > 64 {
		return "", fmt.Errorf("%w: device name is too long", ErrBadRequest)
	}
	// The name is stored and later printed by `corgi mcp devices list`. A
	// control character (ANSI escape, newline) in it would rewrite that
	// terminal output, so reject anything unprintable rather than sanitize on
	// display in every reader.
	for _, r := range deviceName {
		if r == '\t' || !unicode.IsGraphic(r) {
			return "", fmt.Errorf("%w: device name must be printable text", ErrBadRequest)
		}
	}
	if err := session.Redeem(code); err != nil {
		return "", err
	}

	store, err := Load(storePath)
	if err != nil {
		return "", err
	}
	token, err := NewDeviceToken()
	if err != nil {
		return "", err
	}
	// Re-pairing under an existing name replaces that device's token, which is
	// what someone reinstalling the app expects — and it invalidates the old
	// one, which is what they want if the phone was lost.
	store.Revoke(deviceName)
	store.Devices = append(store.Devices, Device{
		Name:      deviceName,
		TokenHash: HashToken(token),
		CreatedAt: time.Now().UTC(),
	})
	if err := Save(storePath, store); err != nil {
		return "", err
	}
	return token, nil
}
