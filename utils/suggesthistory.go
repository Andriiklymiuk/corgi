package utils

import (
	"andriiklymiuk/corgi/utils/atomicfile"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const suggestHistoryFileName = "suggest-history.json"

const suggestRateLimitCeiling = 3

type SuggestEntry struct {
	Slug   string    `json:"slug"`
	Title  string    `json:"title"`
	Lens   string    `json:"lens"`
	Status string    `json:"status"`
	Ticket string    `json:"ticket"`
	Ts     time.Time `json:"ts"`
}

type SuggestHistory struct {
	Version int            `json:"version"`
	Entries []SuggestEntry `json:"entries"`
}

func SuggestHistoryPath(workspaceRoot string) string {
	return filepath.Join(CorgiServicesIn(workspaceRoot), suggestHistoryFileName)
}

func LoadSuggestHistory(workspaceRoot string) (*SuggestHistory, error) {
	path := SuggestHistoryPath(workspaceRoot)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &SuggestHistory{Version: 1}, nil
		}
		return nil, fmt.Errorf("failed to read suggest history: %w", err)
	}
	var h SuggestHistory
	if err := json.Unmarshal(data, &h); err != nil {
		return nil, fmt.Errorf("failed to parse suggest history: %w", err)
	}
	if h.Version == 0 {
		h.Version = 1
	}
	return &h, nil
}

func AppendSuggestEntry(workspaceRoot string, e SuggestEntry) error {
	h, err := LoadSuggestHistory(workspaceRoot)
	if err != nil {
		return err
	}
	h.Entries = append(h.Entries, e)

	path := SuggestHistoryPath(workspaceRoot)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create corgi_services dir: %w", err)
	}
	EnsureCorgiServicesIgnore(dir, suggestHistoryFileName)
	data, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal suggest history: %w", err)
	}
	if err := atomicfile.Write(path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write suggest history: %w", err)
	}
	return nil
}

func ShouldSkip(h *SuggestHistory, slug string, now time.Time, cooldown time.Duration) (bool, string) {
	if h == nil {
		return false, ""
	}
	for _, e := range h.Entries {
		if e.Slug != slug {
			continue
		}
		switch e.Status {
		case "filed":
			return true, "filed"
		case "dismissed":
			if now.Sub(e.Ts) <= cooldown {
				return true, "dismissed"
			}
		case "proposed":
			if now.Sub(e.Ts) <= cooldown {
				return true, "proposed"
			}
		}
	}
	return false, ""
}

func RateLimited(h *SuggestHistory, now time.Time, maxPerWeek int) bool {
	limit := maxPerWeek
	if limit <= 0 {
		limit = 1
	}
	if limit > suggestRateLimitCeiling {
		limit = suggestRateLimitCeiling
	}
	if h == nil {
		return false
	}
	week := 7 * 24 * time.Hour
	filed := 0
	for _, e := range h.Entries {
		if e.Status == "filed" && now.Sub(e.Ts) < week {
			filed++
		}
	}
	return filed >= limit
}

func Slugify(title string) string {
	var b strings.Builder
	lastDash := true
	for _, r := range strings.ToLower(title) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
