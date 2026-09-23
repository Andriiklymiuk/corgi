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

var ErrBadRequest = errors.New("pairing request rejected")

const CodeTTL = 10 * time.Minute

const MaxAttempts = 10

const TokenPrefix = "corgi_dev_"

const codeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

const codeLength = 20

type Device struct {
	Name      string    `json:"name"`
	TokenHash string    `json:"tokenHash"`
	CreatedAt time.Time `json:"createdAt"`
	PubKey    string    `json:"pubKey,omitempty"`
	Role      string    `json:"role,omitempty"`
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
	Family    string    `json:"family,omitempty"`
}

const RoleViewer = "viewer"

// RolePeer is another laptop: it may only pulse and join, never read the board.
const RolePeer = "peer"

func (d Device) Encrypted() bool { return strings.TrimSpace(d.PubKey) != "" }

func (d Device) Viewer() bool { return d.Role == RoleViewer }

func (d Device) Peer() bool { return d.Role == RolePeer }

func (d Device) Expired(now time.Time) bool {
	return !d.ExpiresAt.IsZero() && !now.Before(d.ExpiresAt)
}

type Store struct {
	Version int      `json:"version"`
	Devices []Device `json:"devices"`
}

const storeVersion = 1

func StorePath(agentDir string) string { return filepath.Join(agentDir, "devices.json") }

func Load(path string) (*Store, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return &Store{Version: storeVersion}, nil
	}
	if err != nil {
		return nil, err
	}
	if runtime.GOOS != "windows" {
		if mode := info.Mode().Perm(); mode&0o077 != 0 {
			return nil, fmt.Errorf("%s is readable by other users (mode %04o) - run: chmod 600 %s",
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

type StoreState int

const (
	StoreEmpty StoreState = iota
	StoreHasDevices
	StoreUnreadable
)

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

func (s *Store) Find(name string) (Device, bool) {
	for _, d := range s.Devices {
		if strings.EqualFold(d.Name, name) {
			return d, true
		}
	}
	return Device{}, false
}

func (s *Store) Revoke(name string) bool {
	for i := range s.Devices {
		if strings.EqualFold(s.Devices[i].Name, name) {
			s.Devices = append(s.Devices[:i], s.Devices[i+1:]...)
			return true
		}
	}
	return false
}

func (s *Store) Authorize(token string) (string, bool) {
	d, ok := s.AuthorizeDevice(token)
	return d.Name, ok
}

func (s *Store) AuthorizeDevice(token string) (Device, bool) {
	d, ok := s.FindByToken(token)
	if !ok || d.Expired(time.Now()) {
		return Device{}, false
	}
	return d, true
}

func (s *Store) FindByToken(token string) (Device, bool) {
	if token == "" {
		return Device{}, false
	}
	want := HashToken(token)
	var matched Device
	found := false
	for _, d := range s.Devices {
		if subtle.ConstantTimeCompare([]byte(d.TokenHash), []byte(want)) == 1 {
			matched, found = d, true
		}
	}
	return matched, found
}

func (s *Store) RevokeExpired(now time.Time) int {
	kept := s.Devices[:0]
	dropped := 0
	for _, d := range s.Devices {
		if d.Expired(now) {
			dropped++
			continue
		}
		kept = append(kept, d)
	}
	s.Devices = kept
	return dropped
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func NewDeviceToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("could not generate a device token: %w", err)
	}
	return TokenPrefix + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

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

func NormalizeCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(code)) {
		if strings.ContainsRune(codeAlphabet, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

type Session struct {
	mu       sync.Mutex
	code     string
	expires  time.Time
	attempts int
	used     bool
	now      func() time.Time
}

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

const MaxLaunchTTL = 24 * time.Hour

var ErrBadLaunchCode = errors.New("a launch code is 20 characters from 0-9 A-Z (no I, L, O, U): mint one with `corgi agent pair --mint`")

func NewSessionWithCode(code string, ttl time.Duration) (*Session, error) {
	code = NormalizeCode(code)
	if len(code) != codeLength {
		return nil, ErrBadLaunchCode
	}
	if ttl <= 0 {
		ttl = CodeTTL
	}
	if ttl > MaxLaunchTTL {
		ttl = MaxLaunchTTL
	}
	s := &Session{now: time.Now}
	s.code = code
	s.expires = s.now().Add(ttl)
	return s, nil
}

func (s *Session) ExpiresAt() time.Time {
	if s == nil {
		return time.Time{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.expires
}

func (s *Session) Code() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.code
}

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
		return fmt.Errorf("%w: too many attempts - restart corgi mcp to pair", ErrBadRequest)
	case !s.now().Before(s.expires):
		return fmt.Errorf("%w: that pairing code has expired - restart corgi mcp to pair", ErrBadRequest)
	}

	s.attempts++
	if subtle.ConstantTimeCompare([]byte(NormalizeCode(offered)), []byte(s.code)) != 1 {
		return fmt.Errorf("%w: that pairing code is not correct", ErrBadRequest)
	}
	s.used = true
	return nil
}

func (s *Session) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.used = true
}

func Pair(storePath string, session *Session, code, deviceName string) (string, error) {
	return PairWithKey(storePath, session, code, deviceName, "")
}

func PairWithKey(storePath string, session *Session, code, deviceName, pubKey string) (string, error) {
	return PairWithRole(storePath, session, code, deviceName, pubKey, "")
}

func PairWithRole(storePath string, session *Session, code, deviceName, pubKey, role string) (string, error) {
	if role != "" && role != RoleViewer && role != RolePeer {
		return "", fmt.Errorf("%w: role is viewer, peer or nothing", ErrBadRequest)
	}
	deviceName = strings.TrimSpace(deviceName)
	if deviceName == "" {
		return "", fmt.Errorf("%w: a device name is required", ErrBadRequest)
	}
	if len(deviceName) > 64 {
		return "", fmt.Errorf("%w: device name is too long", ErrBadRequest)
	}
	for _, r := range deviceName {
		if r == '\t' || !unicode.IsGraphic(r) {
			return "", fmt.Errorf("%w: device name must be printable text", ErrBadRequest)
		}
	}
	pk, err := ParsePublicKey(pubKey)
	if err != nil {
		return "", err
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
	store.Revoke(deviceName)
	d := Device{
		Name:      deviceName,
		TokenHash: HashToken(token),
		CreatedAt: time.Now().UTC(),
	}
	if pk != nil {
		d.PubKey = PublicKeyString(pk)
	}
	d.Role = role
	store.Devices = append(store.Devices, d)
	if err := Save(storePath, store); err != nil {
		return "", err
	}
	return token, nil
}

func PairLocal(storePath, deviceName string) (string, error) {
	deviceName = strings.TrimSpace(deviceName)
	if deviceName == "" {
		return "", fmt.Errorf("%w: a device name is required", ErrBadRequest)
	}
	if len(deviceName) > 64 {
		return "", fmt.Errorf("%w: device name is too long", ErrBadRequest)
	}
	for _, r := range deviceName {
		if r == '\t' || !unicode.IsGraphic(r) {
			return "", fmt.Errorf("%w: device name must be printable text", ErrBadRequest)
		}
	}
	store, err := Load(storePath)
	if err != nil {
		return "", err
	}
	token, err := NewDeviceToken()
	if err != nil {
		return "", err
	}
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
