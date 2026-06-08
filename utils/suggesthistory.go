package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// suggestHistoryFileName is the per-developer audit + dedupe state for the
// proactive-suggest job. It lives under the workspace corgi_services/ dir
// (already gitignored), alongside other per-developer runtime state like
// .autopilot.json — not in .corgi/, which holds committed, shared memory.
const suggestHistoryFileName = "suggest-history.json"

// suggestRateLimitCeiling is the hard ceiling on filed tickets per rolling
// week, enforced regardless of any maxPerWeek config — the proactive job must
// never spam the tracker.
const suggestRateLimitCeiling = 3

// SuggestEntry is one recorded suggestion outcome.
//
// Status ∈ filed (a ticket exists, still open) · dismissed (user said no /
// wontfix) · proposed (pending human confirm) · skipped (deduped or
// rate-limited this run, audit only).
type SuggestEntry struct {
	Slug   string    `json:"slug"`
	Title  string    `json:"title"`
	Lens   string    `json:"lens"`
	Status string    `json:"status"`
	Ticket string    `json:"ticket"`
	Ts     time.Time `json:"ts"`
}

// SuggestHistory is the on-disk append-only history of proactive suggestions.
type SuggestHistory struct {
	Version int            `json:"version"`
	Entries []SuggestEntry `json:"entries"`
}

// SuggestHistoryPath returns <workspaceRoot>/corgi_services/suggest-history.json.
func SuggestHistoryPath(workspaceRoot string) string {
	return filepath.Join(workspaceRoot, "corgi_services", suggestHistoryFileName)
}

// LoadSuggestHistory reads the workspace suggest-history.json. A missing file
// is not an error — it returns a fresh, empty history (mirrors LoadUserConfig).
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

// AppendSuggestEntry does a read-modify-write append of one entry: it loads the
// existing history, appends e, and writes the file back atomically (tmp +
// rename), creating corgi_services/ (0o755) if absent. File mode 0o644.
func AppendSuggestEntry(workspaceRoot string, e SuggestEntry) error {
	h, err := LoadSuggestHistory(workspaceRoot)
	if err != nil {
		return err
	}
	h.Entries = append(h.Entries, e)

	path := SuggestHistoryPath(workspaceRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create corgi_services dir: %w", err)
	}
	data, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal suggest history: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("failed to write suggest history: %w", err)
	}
	return os.Rename(tmp, path)
}

// ShouldSkip reports whether a candidate slug is already known and so should be
// deduped. It blocks on: any filed entry (a ticket exists, always blocks);
// a dismissed entry within cooldown; a proposed entry within cooldown (don't
// re-propose the same pending idea). skipped audit entries never block.
// Returns (true, reason) on a hit, else (false, "").
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

// RateLimited reports whether the per-week filing cap is already hit: it counts
// filed entries within the rolling 7 days and compares to maxPerWeek.
// maxPerWeek <= 0 is treated as the default 1; a maxPerWeek above the hard
// ceiling (3) is clamped to 3 regardless of config.
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

// Slugify derives a stable kebab-case key from a suggestion title: lowercase,
// every run of non-alphanumeric characters becomes a single dash, trimmed.
func Slugify(title string) string {
	var b strings.Builder
	lastDash := true // suppress a leading dash
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
