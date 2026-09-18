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

type rawWindow struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    string  `json:"resets_at"`
}

func (w *rawWindow) window() Window {
	if w == nil {
		return Window{}
	}
	out := Window{Percent: int(w.Utilization + 0.5)}
	if t, err := time.Parse(time.RFC3339Nano, w.ResetsAt); err == nil {
		out.ResetsAt = t
	}
	return out
}
