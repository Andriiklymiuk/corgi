package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Limits is the account's rate-limit picture as Claude Code last fetched it —
// what /usage shows. Claude Code caches it in <configDir>/.claude.json under
// cachedUsageUtilization; corgi only reads that cache, so the numbers are as
// fresh as the last session that asked.
type Limits struct {
	FetchedAt time.Time `json:"fetchedAt"`
	FiveHour  Window    `json:"fiveHour"`
	SevenDay  Window    `json:"sevenDay"`
}

// Window is one rolling limit: how much of it is used, and when it resets.
type Window struct {
	Percent  int       `json:"percent"`
	ResetsAt time.Time `json:"resetsAt,omitempty"`
}

// ReadLimits reads the cached snapshot for one Claude config dir ("" is
// ~/.claude). ok is false when no session under that account has fetched
// usage yet.
func ReadLimits(configDir string) (Limits, bool) {
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Limits{}, false
		}
		configDir = filepath.Join(home, ".claude")
	}
	// Claude Code keeps the default account's file at ~/.claude.json, and a
	// custom config dir's inside that dir.
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

func parseLimits(data []byte) (Limits, bool) {
	var file struct {
		Cached struct {
			FetchedAtMs int64 `json:"fetchedAtMs"`
			Utilization struct {
				FiveHour *rawWindow `json:"five_hour"`
				SevenDay *rawWindow `json:"seven_day"`
			} `json:"utilization"`
		} `json:"cachedUsageUtilization"`
	}
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
