package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// ModelTokens is one model's share of a day.
type ModelTokens struct {
	Model  string `json:"model"`
	Tokens int64  `json:"tokens"`
}

// DayStats is what Claude Code's own stats cache says about one day under
// one account: how busy, and on which models.
type DayStats struct {
	Date      string        `json:"date"`
	Messages  int           `json:"messages"`
	Sessions  int           `json:"sessions"`
	ToolCalls int           `json:"toolCalls"`
	Models    []ModelTokens `json:"models,omitempty"`
}

// ReadDayStats reads <configDir>/stats-cache.json for the given local day
// ("2026-09-08"). ok is false when the account has no cache or no entry for
// that day. The cache is Claude Code's, recomputed when it feels like it,
// so the numbers lag the transcripts by hours.
func ReadDayStats(configDir, date string) (DayStats, bool) {
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return DayStats{}, false
		}
		configDir = filepath.Join(home, ".claude")
	}
	data, err := os.ReadFile(filepath.Join(configDir, "stats-cache.json"))
	if err != nil {
		return DayStats{}, false
	}
	var file struct {
		Daily []struct {
			Date      string `json:"date"`
			Messages  int    `json:"messageCount"`
			Sessions  int    `json:"sessionCount"`
			ToolCalls int    `json:"toolCallCount"`
		} `json:"dailyActivity"`
		Tokens []struct {
			Date   string           `json:"date"`
			Models map[string]int64 `json:"tokensByModel"`
		} `json:"dailyModelTokens"`
	}
	if json.Unmarshal(data, &file) != nil {
		return DayStats{}, false
	}
	out := DayStats{Date: date}
	found := false
	for _, d := range file.Daily {
		if d.Date == date {
			out.Messages, out.Sessions, out.ToolCalls = d.Messages, d.Sessions, d.ToolCalls
			found = true
		}
	}
	for _, t := range file.Tokens {
		if t.Date != date {
			continue
		}
		for m, n := range t.Models {
			out.Models = append(out.Models, ModelTokens{Model: m, Tokens: n})
		}
		found = true
	}
	sort.Slice(out.Models, func(i, j int) bool { return out.Models[i].Tokens > out.Models[j].Tokens })
	return out, found
}

// Today is the local date the stats cache keys on.
func Today(now time.Time) string { return now.Local().Format("2006-01-02") }
