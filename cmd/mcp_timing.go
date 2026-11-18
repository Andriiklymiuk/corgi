package cmd

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	timingKept      = 256
	timingsFileName = "timings.json"
)

var timingSaveEvery = 30 * time.Second

type routeTiming struct {
	Path  string  `json:"path"`
	Count uint64  `json:"count"`
	P50   float64 `json:"p50Ms"`
	P95   float64 `json:"p95Ms"`
	Max   float64 `json:"maxMs"`
}

type routeSamples struct {
	count uint64
	last  []time.Duration
	next  int
}

type launchTimings struct {
	mu     sync.Mutex
	routes map[string]*routeSamples
	saved  time.Time
}

var launchTimingLog = &launchTimings{routes: map[string]*routeSamples{}}

var launchTimingsDir string

func (t *launchTimings) record(path string, took time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.routes[path]
	if s == nil {
		s = &routeSamples{}
		t.routes[path] = s
	}
	s.count++
	if len(s.last) < timingKept {
		s.last = append(s.last, took)
	} else {
		s.last[s.next] = took
		s.next = (s.next + 1) % timingKept
	}
}

func (t *launchTimings) snapshot() []routeTiming {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]routeTiming, 0, len(t.routes))
	for path, s := range t.routes {
		sorted := append([]time.Duration(nil), s.last...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
		out = append(out, routeTiming{
			Path:  path,
			Count: s.count,
			P50:   durationMs(sorted[len(sorted)/2]),
			P95:   durationMs(sorted[(len(sorted)*95)/100]),
			Max:   durationMs(sorted[len(sorted)-1]),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].P95 != out[j].P95 {
			return out[i].P95 > out[j].P95
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func (t *launchTimings) dueToSave(now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if now.Sub(t.saved) < timingSaveEvery {
		return false
	}
	t.saved = now
	return true
}

func durationMs(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}

func saveLaunchTimings(dir string, rows []routeTiming) {
	if dir == "" {
		return
	}
	body, err := json.MarshalIndent(map[string]any{"at": time.Now().UTC(), "routes": rows}, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, timingsFileName), body, 0o600)
}

func timedLaunchRoute(path string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		launchTimingLog.record(path, time.Since(start))
		if launchTimingLog.dueToSave(start) {
			saveLaunchTimings(launchTimingsDir, launchTimingLog.snapshot())
		}
	})
}

func launchTimingsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set(headerContentType, mimeJSON)
	_ = json.NewEncoder(w).Encode(map[string]any{"routes": launchTimingLog.snapshot()})
}
