package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type ModelTokens struct {
	Model  string `json:"model"`
	Tokens int64  `json:"tokens"`
}

type DayStats struct {
	Date        string           `json:"date"`
	Messages    int              `json:"messages"`
	Sessions    int              `json:"sessions"`
	ToolCalls   int              `json:"toolCalls"`
	Models      []ModelTokens    `json:"models,omitempty"`
	Tokens      map[string]int64 `json:"tokens,omitempty"`
	TokensTotal int64            `json:"tokensTotal,omitempty"`
}

type statsFile struct {
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

func readStatsFile(configDir string) (statsFile, bool) {
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return statsFile{}, false
		}
		configDir = filepath.Join(home, ".claude")
	}
	data, err := os.ReadFile(filepath.Join(configDir, "stats-cache.json"))
	if err != nil {
		return statsFile{}, false
	}
	var file statsFile
	if json.Unmarshal(data, &file) != nil {
		return statsFile{}, false
	}
	return file, true
}

func ReadDayStats(configDir, date string) (DayStats, bool) {
	file, ok := readStatsFile(configDir)
	if !ok {
		return DayStats{}, false
	}
	return file.day(date)
}

func ReadDaysStats(configDir string, dates []string) map[string]DayStats {
	out := map[string]DayStats{}
	file, ok := readStatsFile(configDir)
	if !ok {
		return out
	}
	for _, date := range dates {
		if day, found := file.day(date); found {
			out[date] = day
		}
	}
	return out
}

func (file statsFile) day(date string) (DayStats, bool) {
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

func Today(now time.Time) string { return now.Local().Format("2006-01-02") }
