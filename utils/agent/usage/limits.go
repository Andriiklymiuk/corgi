package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type Limits struct {
	FetchedAt time.Time `json:"fetchedAt"`
	FiveHour  Window    `json:"fiveHour"`
	SevenDay  Window    `json:"sevenDay"`
}

type Window struct {
	Percent  int       `json:"percent"`
	ResetsAt time.Time `json:"resetsAt,omitempty"`
}

func ReadLimits(configDir string) (Limits, bool) {
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Limits{}, false
		}
		configDir = filepath.Join(home, ".claude")
	}
	candidates := []string{filepath.Join(configDir, ".claude.json")}
	if filepath.Base(configDir) == ".claude" {
		candidates = append(candidates, filepath.Join(filepath.Dir(configDir), ".claude.json"))
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if l, ok := parseLimits(data); ok {
			return l, true
		}
	}
	return Limits{}, false
}

type cacheFile struct {
	Cached cachedUtilization `json:"cachedUsageUtilization"`
}

type cachedUtilization struct {
	FetchedAtMs int64          `json:"fetchedAtMs"`
	Utilization rawUtilization `json:"utilization"`
}

type rawUtilization struct {
	FiveHour *rawWindow `json:"five_hour"`
	SevenDay *rawWindow `json:"seven_day"`
}

func parseLimits(data []byte) (Limits, bool) {
	var file cacheFile
	if json.Unmarshal(data, &file) != nil || file.Cached.FetchedAtMs == 0 {
		return Limits{}, false
	}
	l := Limits{FetchedAt: time.UnixMilli(file.Cached.FetchedAtMs)}
	l.FiveHour = file.Cached.Utilization.FiveHour.window()
	l.SevenDay = file.Cached.Utilization.SevenDay.window()
	return l, true
}

// rawWindow is one window as Claude Code caches it. Older builds wrote
// the utilization as a percent and the reset as an RFC 3339 string;
// since 2.1.2xx it is a fraction of one and unix seconds. Both read.
type rawWindow struct {
	Utilization *float64        `json:"utilization"`
	ResetsAt    json.RawMessage `json:"resets_at"`
}

func (w *rawWindow) window() Window {
	if w == nil {
		return Window{}
	}
	var out Window
	if w.Utilization != nil {
		u := *w.Utilization
		if u <= 1 {
			u *= 100
		}
		out.Percent = int(u + 0.5)
	}
	out.ResetsAt = resetsAtOf(w.ResetsAt)
	return out
}

// resetsAtOf reads a reset as unix seconds (or milliseconds), or as an
// RFC 3339 string; anything else is no reset.
func resetsAtOf(raw json.RawMessage) time.Time {
	var n float64
	if json.Unmarshal(raw, &n) == nil && n > 0 {
		if n > 1e12 {
			return time.UnixMilli(int64(n)).UTC()
		}
		return time.Unix(int64(n), 0).UTC()
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
