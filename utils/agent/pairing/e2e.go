package pairing

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const E2EVersion = 1

const E2EHeader = "X-Corgi-E2E"

const e2eSkew = 2 * time.Minute

type Envelope struct {
	V int    `json:"v"`
	T int64  `json:"t"`
	N string `json:"n"`
	C string `json:"c"`
}

var ErrNotEncrypted = errors.New("this device pairs end-to-end encrypted: plaintext is refused")

func ServerKeyPath(agentDir string) string { return filepath.Join(agentDir, "e2e.key") }

func LoadOrCreateServerKey(path string) (*ecdh.PrivateKey, error) {
	curve := ecdh.X25519()
	raw, err := os.ReadFile(path)
	if err == nil {
		b, derr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
		if derr == nil {
			if k, kerr := curve.NewPrivateKey(b); kerr == nil {
				return k, nil
			}
		}
		return nil, fmt.Errorf("%s is not a key; delete it to mint a new one (every phone then pairs again)", path)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	k, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(k.Bytes())+"\n"), 0o600); err != nil {
		return nil, err
	}
	return k, nil
}

func PublicKeyString(k *ecdh.PublicKey) string { return base64.StdEncoding.EncodeToString(k.Bytes()) }

func ParsePublicKey(s string) (*ecdh.PublicKey, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("%w: the public key is not base64", ErrBadRequest)
	}
	k, err := ecdh.X25519().NewPublicKey(b)
	if err != nil {
		return nil, fmt.Errorf("%w: the public key is not an X25519 key", ErrBadRequest)
	}
	return k, nil
}

func SharedKey(server *ecdh.PrivateKey, device *ecdh.PublicKey) ([]byte, error) {
	secret, err := server.ECDH(device)
	if err != nil {
		return nil, err
	}
	return deriveKey(secret, server.PublicKey(), device)
}

func SharedKeyOnDevice(device *ecdh.PrivateKey, server *ecdh.PublicKey) ([]byte, error) {
	secret, err := device.ECDH(server)
	if err != nil {
		return nil, err
	}
	return deriveKey(secret, server, device.PublicKey())
}

func deriveKey(secret []byte, server, device *ecdh.PublicKey) ([]byte, error) {
	info := append(append([]byte{}, server.Bytes()...), device.Bytes()...)
	return hkdf.Key(sha256.New, secret, []byte("corgi-e2e-v1"), string(info), 32)
}

func Seal(key []byte, method, path string, body []byte, now time.Time) ([]byte, error) {
	gcm, err := gcmFor(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	t := now.UnixMilli()
	ct := gcm.Seal(nil, nonce, body, aad(method, path, t))
	return json.Marshal(Envelope{V: E2EVersion, T: t, N: base64.StdEncoding.EncodeToString(nonce), C: base64.StdEncoding.EncodeToString(ct)})
}

func Open(key []byte, method, path string, envelope []byte, now time.Time) ([]byte, error) {
	var env Envelope
	if err := json.Unmarshal(envelope, &env); err != nil || env.V != E2EVersion {
		return nil, fmt.Errorf("not an encrypted message")
	}
	if d := now.Sub(time.UnixMilli(env.T)); d > e2eSkew || d < -e2eSkew {
		return nil, fmt.Errorf("the message is too old (or the clocks disagree by more than %s)", e2eSkew)
	}
	nonce, err := base64.StdEncoding.DecodeString(env.N)
	if err != nil {
		return nil, fmt.Errorf("not an encrypted message")
	}
	ct, err := base64.StdEncoding.DecodeString(env.C)
	if err != nil {
		return nil, fmt.Errorf("not an encrypted message")
	}
	gcm, err := gcmFor(key)
	if err != nil {
		return nil, err
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, fmt.Errorf("not an encrypted message")
	}
	plain, err := gcm.Open(nil, nonce, ct, aad(method, path, env.T))
	if err != nil {
		return nil, fmt.Errorf("the message does not open with this device's key")
	}
	return plain, nil
}

func gcmFor(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Binds method, path and time so a sealed allow cannot replay as a deny, nor later.
func aad(method, path string, t int64) []byte {
	return []byte("corgi-e2e-v1|" + strings.ToUpper(method) + "|" + path + "|" + strconv.FormatInt(t, 10))
}
