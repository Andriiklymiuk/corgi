package cmd

import (
	"net/http"
	"strings"
	"time"
)

// The morning brief, for a phone: what the daily digest says (sessions,
// waits, limits, where each account stands) and what was done since
// yesterday, per workspace — the same words `corgi agent digest` and
// `corgi agent standup` print. The daemon pushes the digest once a day
// at digestAt; the push opens this.
func launchBriefHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodGet {
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET the brief")
		return
	}
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now()
	since := now.Add(-24 * time.Hour)
	if h := strings.TrimSpace(r.URL.Query().Get("hours")); h != "" {
		if d, err := time.ParseDuration(h + "h"); err == nil && d > 0 && d <= 24*14*time.Hour {
			since = now.Add(-d)
		}
	}
	entries := collectStandup(since)
	if entries == nil {
		entries = []standupEntry{}
	}
	writeLaunchJSON(w, map[string]any{
		"at":         now,
		"since":      since,
		"digest":     digestText(dir, now),
		"standup":    strings.TrimSpace(formatStandup(entries, since)),
		"workspaces": entries,
	})
}
