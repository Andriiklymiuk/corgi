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

func launchAuth(token string, next http.Handler, deviceStorePath string) http.Handler {
	if token == "" && deviceStorePath == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := identifyLaunch(w, r, token, deviceStorePath)
		if !ok {
			return
		}
		if id.key == nil {
			next.ServeHTTP(w, r)
			return
		}
		serveSealed(w, r, next, id.key)
	})
}

type launchIdentity struct {
	key    []byte
	viewer bool
}

func identifyLaunch(w http.ResponseWriter, r *http.Request, token, deviceStorePath string) (launchIdentity, bool) {
	if token == "" && deviceStorePath == "" {
		return launchIdentity{}, true
	}
	want := []byte(bearerPrefix + token)
	got := []byte(r.Header.Get("Authorization"))
	if token != "" && subtle.ConstantTimeCompare(got, want) == 1 {
		return launchIdentity{}, true
	}
	device, ok := authorizedDeviceFull(deviceStorePath, r.Header.Get("Authorization"))
	if !ok {
		w.Header().Set("Content-Type", mimeJSON)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
		return launchIdentity{}, false
	}
	if device.Viewer() && !viewerMay(r.Method, r.URL.Path) {
		writeLaunchError(w, http.StatusForbidden, "this device only reads the board")
		return launchIdentity{}, false
	}
	if device.Peer() && !peerMay(r.URL.Path) {
		writeLaunchError(w, http.StatusForbidden, "a peer laptop only pulses and joins")
		return launchIdentity{}, false
	}
	if !device.Encrypted() {
		if r.Header.Get(pairing.E2EHeader) != "" {
			writeLaunchError(w, http.StatusBadRequest, "this device did not pair with a key; pair again to talk encrypted")
			return launchIdentity{}, false
		}
		return launchIdentity{viewer: device.Viewer()}, true
	}
	key, err := e2eKeyFor(deviceStorePath, device)
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, "the machine's encryption key could not be read")
		return launchIdentity{}, false
	}
	if r.Header.Get(pairing.E2EHeader) == "" {
		writeLaunchError(w, http.StatusForbidden, pairing.ErrNotEncrypted.Error())
		return launchIdentity{}, false
	}
	return launchIdentity{key: key, viewer: device.Viewer()}, true
}

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

func sealedBodyLimit(path string) int64 {
	if path == "/launch/upload" {
		return maxUploadSealed
	}
	return 1 << 20
}

func serveSealed(w http.ResponseWriter, r *http.Request, next http.Handler, key []byte) {
	method, path := r.Method, r.URL.Path
	if r.Body == nil || r.ContentLength == 0 {
		if method != http.MethodGet && method != http.MethodHead {
			writeLaunchError(w, http.StatusBadRequest, "this device pairs end-to-end encrypted: a sealed body is required")
			return
		}
	} else {
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
		if e2eReplay.Seen(string(key)+pairing.EnvelopeNonce(raw), time.Now()) {
			writeLaunchError(w, http.StatusBadRequest, "this message was already delivered")
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

var e2eReplay = pairing.NewReplayGuard()

type sealedWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (s *sealedWriter) Header() http.Header         { return s.header }
func (s *sealedWriter) WriteHeader(code int)        { s.status = code }
func (s *sealedWriter) Write(b []byte) (int, error) { return s.body.Write(b) }

// A peer laptop holds a token here, but it is not a phone: it may tell this
// laptop what it watches and ask it to pair back, nothing else.
func peerMay(path string) bool {
	return path == "/launch/peers/pulse" || path == "/launch/peers/join"
}

func viewerMay(method, path string) bool {
	if method != http.MethodGet {
		return false
	}
	switch path {
	case "/launch/transcript", "/launch/picture", "/launch/doctor", "/launch/sessions", "/launch/preview", "/launch/diff", "/launch/run":
		return false
	}
	return true
}
