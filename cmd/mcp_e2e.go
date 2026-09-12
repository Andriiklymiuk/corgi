package cmd

import (
	"bytes"
	"crypto/subtle"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils/agent/pairing"
)

// launchAuth is bearerAuth for the launcher's data endpoints, plus the
// end-to-end layer: a device that paired with a key sends and receives every
// body sealed (pairing.Seal / Open), and is refused plaintext. The server
// token and key-less devices pass through untouched. /mcp itself is not
// wrapped: its stream is the MCP transport's own.
func launchAuth(token string, next http.Handler, deviceStorePath string) http.Handler {
	if token == "" && deviceStorePath == "" {
		return next
	}
	want := []byte(bearerPrefix + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("Authorization"))
		if token != "" && subtle.ConstantTimeCompare(got, want) == 1 {
			next.ServeHTTP(w, r)
			return
		}
		device, ok := authorizedDeviceFull(deviceStorePath, r.Header.Get("Authorization"))
		if !ok {
			w.Header().Set("Content-Type", mimeJSON)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		if !device.Encrypted() {
			if r.Header.Get(pairing.E2EHeader) != "" {
				writeLaunchError(w, http.StatusBadRequest, "this device did not pair with a key; pair again to talk encrypted")
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		key, err := e2eKeyFor(deviceStorePath, device)
		if err != nil {
			writeLaunchError(w, http.StatusInternalServerError, "the machine's encryption key could not be read")
			return
		}
		if r.Header.Get(pairing.E2EHeader) == "" {
			writeLaunchError(w, http.StatusForbidden, pairing.ErrNotEncrypted.Error())
			return
		}
		serveSealed(w, r, next, key)
	})
}

// authorizedDeviceFull is authorizedDevice with the device itself.
func authorizedDeviceFull(storePath, header string) (pairing.Device, bool) {
	if storePath == "" {
		return pairing.Device{}, false
	}
	offered, ok := strings.CutPrefix(header, bearerPrefix)
	if !ok || !strings.HasPrefix(offered, pairing.TokenPrefix) {
		return pairing.Device{}, false
	}
	store, err := pairing.Load(storePath)
	if err != nil {
		return pairing.Device{}, false
	}
	return store.AuthorizeDevice(offered)
}

// e2eKeys caches each device's derived key: one X25519 per pairing, not per
// request. Keyed by the device's public key, so a re-pair gets a new one.
var e2eKeys sync.Map

func e2eKeyFor(storePath string, d pairing.Device) ([]byte, error) {
	if k, ok := e2eKeys.Load(d.PubKey); ok {
		return k.([]byte), nil
	}
	server, err := pairing.LoadOrCreateServerKey(pairing.ServerKeyPath(filepath.Dir(storePath)))
	if err != nil {
		return nil, err
	}
	pk, err := pairing.ParsePublicKey(d.PubKey)
	if err != nil || pk == nil {
		return nil, err
	}
	key, err := pairing.SharedKey(server, pk)
	if err != nil {
		return nil, err
	}
	e2eKeys.Store(d.PubKey, key)
	return key, nil
}

// sealedBodyLimit is how much sealed request a path may carry: a picture
// for a session is the one big thing a phone sends.
func sealedBodyLimit(path string) int64 {
	if path == "/launch/upload" {
		return maxUploadSealed
	}
	return 1 << 20
}

// serveSealed opens the request body, runs the handler against a buffer,
// and seals what it wrote. Errors the handler wrote travel sealed too: a
// sniffer learns the status code and nothing else.
func serveSealed(w http.ResponseWriter, r *http.Request, next http.Handler, key []byte) {
	method, path := r.Method, r.URL.Path
	if r.Body != nil && r.ContentLength != 0 {
		raw, err := io.ReadAll(io.LimitReader(r.Body, sealedBodyLimit(path)))
		if err != nil {
			writeLaunchError(w, http.StatusBadRequest, "could not read the request")
			return
		}
		plain, err := pairing.Open(key, method, path, raw, time.Now())
		if err != nil {
			writeLaunchError(w, http.StatusBadRequest, err.Error())
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(plain))
		r.ContentLength = int64(len(plain))
	}
	buf := &sealedWriter{header: http.Header{}, status: http.StatusOK}
	next.ServeHTTP(buf, r)
	sealed, err := pairing.Seal(key, method, path, buf.body.Bytes(), time.Now())
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, "could not seal the answer")
		return
	}
	for k, v := range buf.header {
		if strings.EqualFold(k, "Content-Length") {
			continue
		}
		w.Header()[k] = v
	}
	w.Header().Set("Content-Type", mimeJSON)
	w.Header().Set(pairing.E2EHeader, "1")
	w.WriteHeader(buf.status)
	_, _ = w.Write(sealed)
}

// sealedWriter is the handler's ResponseWriter while its answer is being
// gathered for sealing.
type sealedWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (s *sealedWriter) Header() http.Header         { return s.header }
func (s *sealedWriter) WriteHeader(code int)        { s.status = code }
func (s *sealedWriter) Write(b []byte) (int, error) { return s.body.Write(b) }
