package cmd

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils/agent/screen"
)

const (
	handsKeyHeader = "X-Corgi-Hands"
	handsLocalPath = "/launch/screen/hands/local"
	handsBarPath   = "/launch/screen/hands/bar"
	handsLocal     = "here"
	handsWait      = 25 * time.Second
	handsJobLimit  = 20 * time.Second
	handsBarGone   = 35 * time.Second
	handsMaxImage  = 3 << 20
)

type handsAction struct {
	Do   string `json:"do"`
	X    int    `json:"x,omitempty"`
	Y    int    `json:"y,omitempty"`
	X2   int    `json:"x2,omitempty"`
	Y2   int    `json:"y2,omitempty"`
	Text string `json:"text,omitempty"`
	Keys string `json:"keys,omitempty"`
	DX   int    `json:"dx,omitempty"`
	DY   int    `json:"dy,omitempty"`
	MS   int    `json:"ms,omitempty"`
}

type handsRequest struct {
	Do      string        `json:"do"`
	Window  string        `json:"window,omitempty"`
	Max     int           `json:"max,omitempty"`
	Actions []handsAction `json:"actions,omitempty"`
	Look    bool          `json:"look,omitempty"`
	App     string        `json:"app,omitempty"`
	URL     string        `json:"url,omitempty"`
	Path    []string      `json:"path,omitempty"`
	Icons   bool          `json:"icons,omitempty"`
}

type handsJob struct {
	ID string `json:"id"`
	handsRequest
	By string `json:"by"`
}

type handsResult struct {
	ID     string `json:"id"`
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
	Text   string `json:"text,omitempty"`
	Image  string `json:"image,omitempty"`
	Mime   string `json:"mime,omitempty"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

type handsHub struct {
	mu       sync.Mutex
	jobs     chan *handsJob
	pending  map[string]chan handsResult
	lastPoll time.Time
	holder   string
	step     int
	lastStep time.Time
	steps    []handsStep
	pausedBy string
	pausedAt time.Time
	waiting  bool
}

var hands = &handsHub{jobs: make(chan *handsJob, 8), pending: map[string]chan handsResult{}}

var handsDoes = map[string]bool{"look": true, "act": true, "windows": true, "open": true, "apps": true, "menus": true, "menu": true, "activate": true}

var viewerDoes = map[string]bool{"windows": true, "apps": true, "menus": true, "menu": true, "activate": true}

var handsActionDoes = map[string]bool{"click": true, "double": true, "right": true, "move": true, "drag": true, "type": true, "key": true, "scroll": true, "wait": true}

func (r handsRequest) validate() error {
	if !handsDoes[r.Do] {
		return errors.New("do is look, act, windows, open, apps, menus, menu or activate")
	}
	if r.Do == "menu" && (len(r.Path) < 2 || len(r.Path) > 6) {
		return errors.New("menu takes a path from the menu bar, like [\"File\", \"Save\"]")
	}
	if r.Do == "activate" && strings.TrimSpace(r.App) == "" {
		return errors.New("activate takes an app name or bundle id")
	}
	if r.Do == "act" {
		if len(r.Actions) == 0 || len(r.Actions) > 40 {
			return errors.New("act takes 1 to 40 actions")
		}
		for _, a := range r.Actions {
			if !handsActionDoes[a.Do] {
				return errors.New("an action is click, double, right, move, drag, type, key, scroll or wait, not " + a.Do)
			}
			if a.Do == "wait" && (a.MS < 0 || a.MS > 5000) {
				return errors.New("wait is 0 to 5000 ms")
			}
			if len(a.Text) > 4000 {
				return errors.New("type at most 4000 characters at a time")
			}
		}
	}
	if r.Do == "open" && strings.TrimSpace(r.App) == "" && strings.TrimSpace(r.URL) == "" {
		return errors.New("open takes an app name or a url")
	}
	if r.Max < 0 || r.Max > 2560 {
		return errors.New("max is up to 2560 pixels")
	}
	return nil
}

func newHandsID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (h *handsHub) barListening(now time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return now.Sub(h.lastPoll) < handsBarGone
}

func (h *handsHub) run(ctx context.Context, req handsRequest, by string) (handsResult, error) {
	if !h.barListening(time.Now()) {
		return handsResult{}, errors.New("Corgi Bar is not running on " + macName() + ", or agents are not allowed there: corgi agent hands allow")
	}
	if !viewerDoes[req.Do] || agentWork(ctx) {
		if err := h.admit(req, by, time.Now()); err != nil {
			return handsResult{}, err
		}
	}
	job := &handsJob{ID: newHandsID(), handsRequest: req, By: by}
	done := make(chan handsResult, 1)
	h.mu.Lock()
	h.pending[job.ID] = done
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.pending, job.ID)
		h.mu.Unlock()
	}()
	select {
	case h.jobs <- job:
	case <-ctx.Done():
		return handsResult{}, ctx.Err()
	case <-time.After(2 * time.Second):
		return handsResult{}, errors.New("Corgi Bar is busy with other agent work")
	}
	select {
	case r := <-done:
		if !r.OK && r.Error == "" {
			r.Error = "Corgi Bar could not do that"
		}
		return r, nil
	case <-ctx.Done():
		return handsResult{}, ctx.Err()
	case <-time.After(handsJobLimit):
		return handsResult{}, errors.New("Corgi Bar did not answer in 20 s")
	}
}

func (h *handsHub) next(ctx context.Context, now time.Time) *handsJob {
	h.mu.Lock()
	h.lastPoll = now
	h.mu.Unlock()
	select {
	case j := <-h.jobs:
		return j
	case <-ctx.Done():
		return nil
	case <-time.After(handsWait):
		return nil
	}
}

func (h *handsHub) finish(r handsResult) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastPoll = time.Now()
	ch, ok := h.pending[r.ID]
	if ok {
		ch <- r
	}
	return ok
}

type agentWorkKey struct{}

func asAgentWork(ctx context.Context) context.Context {
	return context.WithValue(ctx, agentWorkKey{}, true)
}

func agentWork(ctx context.Context) bool {
	on, _ := ctx.Value(agentWorkKey{}).(bool)
	return on
}

func handsKeyOK(r *http.Request) bool {
	dir, err := agentDir()
	if err != nil {
		return false
	}
	key, err := handsKey(dir)
	return err == nil && loopbackRequest(r) && subtle.ConstantTimeCompare([]byte(r.Header.Get(handsKeyHeader)), []byte(key)) == 1
}

func handsKeyPath(dir string) string { return filepath.Join(dir, "screen", "hands.key") }

func handsKey(dir string) (string, error) {
	p := handsKeyPath(dir)
	if raw, err := os.ReadFile(p); err == nil && len(strings.TrimSpace(string(raw))) >= 32 {
		return strings.TrimSpace(string(raw)), nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return "", err
	}
	key := newHandsID() + newHandsID() + newHandsID() + newHandsID()
	if err := os.WriteFile(p, []byte(key), 0o600); err != nil {
		return "", err
	}
	return key, nil
}

func handsAllowed(s *screen.Settings, who string) bool {
	if who == handsLocal {
		return s.LocalAgents
	}
	g, ok := s.Devices[who]
	return ok && g.Agents
}

func loopbackRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback() && r.Header.Get("X-Forwarded-For") == "" && r.Header.Get("Forwarded") == ""
}

func launchHandsLocal(storePath string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setLaunchHeaders(w)
		dir, err := agentDir()
		if err != nil {
			writeLaunchError(w, http.StatusInternalServerError, err.Error())
			return
		}
		key, err := handsKey(dir)
		if err != nil || !loopbackRequest(r) || subtle.ConstantTimeCompare([]byte(r.Header.Get(handsKeyHeader)), []byte(key)) != 1 {
			writeLaunchError(w, http.StatusForbidden, "only agents on this Mac, with its hands key")
			return
		}
		serveHands(w, r, dir, handsLocal, "this Mac")
	})
}

func launchHandsHandler(w http.ResponseWriter, r *http.Request) {
	dir, d, ok := screenDevice(w, r)
	if !ok {
		return
	}
	if !d.Peer() {
		writeLaunchError(w, http.StatusForbidden, "only a peer Mac's agents may use this Mac")
		return
	}
	serveHands(w, r, dir, d.TokenHash, d.Name)
}

func serveHands(w http.ResponseWriter, r *http.Request, dir, who, name string) {
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, postOnlyMessage)
		return
	}
	s, err := screen.Load(dir)
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !handsAllowed(s, who) {
		writeLaunchError(w, http.StatusForbidden, "agents from "+name+" may not use "+macName()+": corgi agent hands allow \""+name+"\" there")
		return
	}
	var req handsRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeLaunchError(w, http.StatusBadRequest, "could not read the request")
		return
	}
	if err := req.validate(); err != nil {
		writeLaunchError(w, http.StatusBadRequest, err.Error())
		return
	}
	entry := screen.Session{Device: who, Name: name, Role: "agent", Event: req.Do, Detail: stepText(req), At: time.Now()}
	out, err := hands.run(asAgentWork(r.Context()), req, name)
	if err != nil {
		entry.Failed = err.Error()
	} else if !out.OK {
		entry.Failed = out.Error
	}
	_ = screen.Log(dir, entry)
	if err != nil {
		writeLaunchError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeLaunchJSON(w, out)
}

func launchHandsNextHandler(w http.ResponseWriter, r *http.Request) {
	dir, ok := screenMac(w, r)
	if !ok {
		return
	}
	serveHandsNext(w, r, dir)
}

func launchHandsBar() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setLaunchHeaders(w)
		dir, err := agentDir()
		if err != nil {
			writeLaunchError(w, http.StatusInternalServerError, err.Error())
			return
		}
		key, err := handsKey(dir)
		if err != nil || !loopbackRequest(r) || subtle.ConstantTimeCompare([]byte(r.Header.Get(handsKeyHeader)), []byte(key)) != 1 {
			writeLaunchError(w, http.StatusForbidden, "only Corgi Bar on this Mac, with its hands key")
			return
		}
		serveHandsNext(w, r, dir)
	})
}

func launchScreenDoHandler(w http.ResponseWriter, r *http.Request) {
	dir, d, ok := screenDevice(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, postOnlyMessage)
		return
	}
	s, err := screen.Load(dir)
	if err != nil || !s.Enabled {
		writeLaunchError(w, http.StatusForbidden, "remote screen is off on "+macName())
		return
	}
	if s.RoleFor(d.TokenHash) != screen.RoleControl {
		writeLaunchError(w, http.StatusForbidden, d.Name+" may watch "+macName()+", not control it")
		return
	}
	var req handsRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		writeLaunchError(w, http.StatusBadRequest, "could not read the request")
		return
	}
	if !viewerDoes[req.Do] {
		writeLaunchError(w, http.StatusBadRequest, "do is windows, apps, menus, menu or activate")
		return
	}
	if err := req.validate(); err != nil {
		writeLaunchError(w, http.StatusBadRequest, err.Error())
		return
	}
	out, err := hands.run(r.Context(), req, d.Name)
	if err != nil {
		writeLaunchError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeLaunchJSON(w, out)
}

func serveHandsNext(w http.ResponseWriter, r *http.Request, dir string) {
	if s, err := screen.Load(dir); err != nil || (!anyHands(s) && !s.Enabled) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method == http.MethodPost {
		var res handsResult
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, handsMaxImage*2)).Decode(&res); err != nil {
			writeLaunchError(w, http.StatusBadRequest, "could not read the result")
			return
		}
		writeLaunchJSON(w, map[string]any{"taken": hands.finish(res)})
		return
	}
	job := hands.next(r.Context(), time.Now())
	if job == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeLaunchJSON(w, job)
}

func anyHands(s *screen.Settings) bool {
	if s.LocalAgents {
		return true
	}
	for _, g := range s.Devices {
		if g.Agents {
			return true
		}
	}
	return false
}
