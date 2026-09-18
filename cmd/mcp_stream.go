package cmd

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils/agent/pairing"
)

type streamFrame struct {
	Seq  uint64    `json:"seq"`
	What []string  `json:"what,omitempty"`
	At   time.Time `json:"at"`
}

var allFeeds = []string{"board", "inbox", "kanban", "workspaces", "bots"}

var streamFiles = map[string][]string{
	"sessions.json": {"board", "kanban"},
	"status.json":   {"board", "workspaces"},
	"bots.json":     {"bots"},
}

var watchFiles = []string{"events.jsonl", "ignored.json", "state.json", "states.json", "fixes.json", "picks.json", "pulls.json", "handed.json", "tasks.json", "board.json"}

var streamTick = 250 * time.Millisecond

const streamPing = 20 * time.Second

type changeWatch struct {
	dir   string
	mu    sync.Mutex
	seq   uint64
	subs  map[*streamSub]struct{}
	stop  chan struct{}
	done  chan struct{}
	seen  map[string]fileMark
	files map[string][]string
}

type fileMark struct {
	mod  time.Time
	size int64
}

type streamSub struct {
	mu      sync.Mutex
	pending map[string]bool
	seq     uint64
	signal  chan struct{}
}

var changeWatches sync.Map

func changeWatchFor(dir string) *changeWatch {
	if w, ok := changeWatches.Load(dir); ok {
		return w.(*changeWatch)
	}
	files := map[string][]string{}
	for name, feeds := range streamFiles {
		files[filepath.Join(dir, name)] = feeds
	}
	for _, name := range watchFiles {
		files[filepath.Join(dir, "watch", name)] = []string{"inbox", "kanban"}
	}
	w := &changeWatch{dir: dir, subs: map[*streamSub]struct{}{}, files: files}
	actual, _ := changeWatches.LoadOrStore(dir, w)
	return actual.(*changeWatch)
}

func (w *changeWatch) running() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stop != nil
}

func (w *changeWatch) subscribe() *streamSub {
	s := &streamSub{pending: map[string]bool{}, signal: make(chan struct{}, 1)}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.subs[s] = struct{}{}
	if w.stop == nil {
		w.seen = w.markLocked()
		w.stop, w.done = make(chan struct{}), make(chan struct{})
		go w.loop(w.stop, w.done)
	}
	return s
}

func (w *changeWatch) unsubscribe(s *streamSub) {
	w.mu.Lock()
	delete(w.subs, s)
	var stop, done chan struct{}
	if len(w.subs) == 0 && w.stop != nil {
		stop, done = w.stop, w.done
		w.stop, w.done = nil, nil
	}
	w.mu.Unlock()
	if stop != nil {
		close(stop)
		<-done
	}
}

func (w *changeWatch) markLocked() map[string]fileMark {
	marks := map[string]fileMark{}
	for path := range w.files {
		if info, err := os.Stat(path); err == nil {
			marks[path] = fileMark{mod: info.ModTime(), size: info.Size()}
		}
	}
	return marks
}

func (w *changeWatch) loop(stop, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(streamTick)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			w.sweep()
		}
	}
}

func (w *changeWatch) sweep() {
	w.mu.Lock()
	defer w.mu.Unlock()
	moved := map[string]bool{}
	for path, feeds := range w.files {
		info, err := os.Stat(path)
		was, known := w.seen[path]
		switch {
		case err != nil && known:
			delete(w.seen, path)
		case err != nil:
			continue
		case !known || !info.ModTime().Equal(was.mod) || info.Size() != was.size:
			w.seen[path] = fileMark{mod: info.ModTime(), size: info.Size()}
		default:
			continue
		}
		for _, f := range feeds {
			moved[f] = true
		}
	}
	if len(moved) == 0 {
		return
	}
	w.seq++
	for s := range w.subs {
		s.mu.Lock()
		for f := range moved {
			s.pending[f] = true
		}
		s.seq = w.seq
		s.mu.Unlock()
		select {
		case s.signal <- struct{}{}:
		default:
		}
	}
}

func (s *streamSub) take() (streamFrame, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pending) == 0 {
		return streamFrame{}, false
	}
	frame := streamFrame{Seq: s.seq, What: feedsInOrder(s.pending), At: time.Now()}
	s.pending = map[string]bool{}
	return frame, true
}

func feedsInOrder(set map[string]bool) []string {
	var out []string
	for _, f := range allFeeds {
		if set[f] {
			out = append(out, f)
		}
	}
	return out
}

func launchStream(token, storePath string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setLaunchHeaders(w)
		if r.Method != http.MethodGet {
			writeLaunchError(w, http.StatusMethodNotAllowed, "GET to listen for changes")
			return
		}
		id, ok := identifyLaunch(w, r, token, storePath)
		if !ok {
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeLaunchError(w, http.StatusInternalServerError, "this server cannot stream")
			return
		}
		dir, err := agentDir()
		if err != nil {
			writeLaunchError(w, http.StatusInternalServerError, err.Error())
			return
		}
		watch := changeWatchFor(dir)
		sub := watch.subscribe()
		defer watch.unsubscribe(sub)

		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		if id.key != nil {
			w.Header().Set(pairing.E2EHeader, "1")
		}
		w.WriteHeader(http.StatusOK)

		write := func(event string, frame streamFrame) bool {
			data, err := json.Marshal(frame)
			if err != nil {
				return false
			}
			if id.key != nil {
				if data, err = pairing.Seal(id.key, http.MethodGet, r.URL.Path, data, time.Now()); err != nil {
					return false
				}
			}
			if _, err := w.Write([]byte("event: " + event + "\ndata: " + string(data) + "\n\n")); err != nil {
				return false
			}
			flusher.Flush()
			return true
		}

		watch.mu.Lock()
		now := watch.seq
		watch.mu.Unlock()
		hello := streamFrame{Seq: now, At: time.Now()}
		if after, err := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64); err == nil && after != now {
			hello.What = append([]string(nil), allFeeds...)
		}
		if !write("hello", hello) {
			return
		}
		ping := time.NewTicker(streamPing)
		defer ping.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-sub.signal:
				if frame, ok := sub.take(); ok && !write("change", frame) {
					return
				}
			case <-ping.C:
				if _, err := w.Write([]byte(": ping\n\n")); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	})
}
