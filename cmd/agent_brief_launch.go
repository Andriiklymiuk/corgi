package cmd

import (
	"net/http"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils/agent/usage"
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

// cardDay is one day of the fortnight on the card, every account summed.
type cardDay struct {
	Date      string `json:"date"`
	Sessions  int    `json:"sessions"`
	Messages  int    `json:"messages"`
	ToolCalls int    `json:"toolCalls"`
}

// dayCard is the day in numbers and nothing else — no workspace, no ticket,
// no prompt, no session title — so the phone can draw it and a person can
// post it without giving anything away.
type dayCard struct {
	At     time.Time         `json:"at"`
	Since  time.Time         `json:"since"`
	Totals todayTotals       `json:"totals"`
	Today  cardDay           `json:"today"`
	Waits  usage.WaitSummary `json:"waits"`
	Days   []cardDay         `json:"days"`
}

const cardDays = 14

func buildDayCard(dir string, now time.Time) dayCard {
	since := startOfDay(now)
	card := dayCard{At: now, Since: since, Totals: countToday(collectStandup(since))}
	card.Waits, card.Days = cardNumbers(dir, now)
	card.Today = card.Days[cardDays-1]
	return card
}

// cardNumbers is the part of the card that costs no git log: today's waits
// (label dropped) and a fortnight of days, every account summed.
func cardNumbers(dir string, now time.Time) (usage.WaitSummary, []cardDay) {
	waits := usage.Summarize(usage.LoadWaits(dir, startOfDay(now)), "wait")
	waits.LongestLabel = ""
	dates := make([]string, 0, cardDays)
	for i := cardDays - 1; i >= 0; i-- {
		dates = append(dates, usage.Today(now.AddDate(0, 0, -i)))
	}
	byDate := map[string]*cardDay{}
	for _, date := range dates {
		byDate[date] = &cardDay{Date: date}
	}
	var configDirs []string
	if board, err := readBoard(dir); err == nil && len(board.Accounts) > 0 {
		for _, a := range board.Accounts {
			configDirs = append(configDirs, a.ConfigDir)
		}
	} else {
		configDirs = append(configDirs, "")
	}
	for _, configDir := range configDirs {
		for date, day := range usage.ReadDaysStats(configDir, dates) {
			d := byDate[date]
			d.Sessions += day.Sessions
			d.Messages += day.Messages
			d.ToolCalls += day.ToolCalls
		}
	}
	days := make([]cardDay, 0, cardDays)
	for _, date := range dates {
		days = append(days, *byDate[date])
	}
	return waits, days
}

// GET /launch/card — the day in numbers for the share card.
func launchCardHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodGet {
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET the card")
		return
	}
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeLaunchJSON(w, buildDayCard(dir, time.Now()))
}
