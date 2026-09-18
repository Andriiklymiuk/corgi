package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Ledger struct {
	mu    sync.Mutex
	path  string
	days  map[string]*ledgerDay
	dirty bool
}

type ledgerDay struct {
	IDs       []string         `json:"ids,omitempty"`
	Prompts   int              `json:"prompts"`
	ToolCalls int              `json:"toolCalls"`
	Tokens    map[string]int64 `json:"tokens,omitempty"`
}

const ledgerKeep = 60

func LedgerPath(agentDir string) string { return filepath.Join(agentDir, "days.json") }

func OpenLedger(agentDir string) *Ledger {
	l := &Ledger{path: LedgerPath(agentDir), days: map[string]*ledgerDay{}}
	if data, err := os.ReadFile(l.path); err == nil {
		_ = json.Unmarshal(data, &l.days)
	}
	for date, d := range l.days {
		if d == nil {
			delete(l.days, date)
		}
	}
	return l
}

func (l *Ledger) Note(event, sessionID string, foreign bool, at time.Time) {
	if l == nil || sessionID == "" || len(sessionID) > 4 && sessionID[:4] == "pid:" {
		return
	}
	if at.IsZero() {
		at = time.Now()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	date := Today(at)
	d := l.days[date]
	if d == nil {
		d = &ledgerDay{}
		l.days[date] = d
	}
	seen := false
	for _, id := range d.IDs {
		if id == sessionID {
			seen = true
			break
		}
	}
	if !seen {
		d.IDs = append(d.IDs, sessionID)
		l.dirty = true
	}
	switch event {
	case "UserPromptSubmit":
		d.Prompts++
		l.dirty = true
	case "PostToolUse", "PostToolUseFailure":
		d.ToolCalls++
		l.dirty = true
	case "PreToolUse":
		if foreign {
			d.ToolCalls++
			l.dirty = true
		}
	}
}

func (l *Ledger) AddTokens(workspace string, n int64, at time.Time) int64 {
	if l == nil || n <= 0 {
		return 0
	}
	if workspace == "" {
		workspace = "?"
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	date := Today(at)
	d := l.days[date]
	if d == nil {
		d = &ledgerDay{}
		l.days[date] = d
	}
	if d.Tokens == nil {
		d.Tokens = map[string]int64{}
	}
	d.Tokens[workspace] += n
	l.dirty = true
	return d.Tokens[workspace]
}

func (l *Ledger) TokensToday(workspace string, at time.Time) int64 {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if d := l.days[Today(at)]; d != nil {
		return d.Tokens[workspace]
	}
	return 0
}

func (l *Ledger) Flush() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.dirty {
		return nil
	}
	cutoff := Today(time.Now().AddDate(0, 0, -ledgerKeep))
	for date := range l.days {
		if date < cutoff {
			delete(l.days, date)
		}
	}
	data, err := json.Marshal(l.days)
	if err != nil {
		return err
	}
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, l.path); err != nil {
		return err
	}
	l.dirty = false
	return nil
}

func (l *Ledger) Day(date string) DayStats {
	if l == nil {
		return DayStats{Date: date}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.days[date].stats(date)
}

func (d *ledgerDay) stats(date string) DayStats {
	if d == nil {
		return DayStats{Date: date}
	}
	st := DayStats{Date: date, Sessions: len(d.IDs), Messages: d.Prompts, ToolCalls: d.ToolCalls}
	if len(d.Tokens) > 0 {
		st.Tokens = map[string]int64{}
		for ws, n := range d.Tokens {
			st.Tokens[ws] = n
			st.TokensTotal += n
		}
	}
	return st
}

func ReadLedgerDays(agentDir string, dates []string) map[string]DayStats {
	out := map[string]DayStats{}
	data, err := os.ReadFile(LedgerPath(agentDir))
	if err != nil {
		return out
	}
	var days map[string]*ledgerDay
	if json.Unmarshal(data, &days) != nil {
		return out
	}
	for _, date := range dates {
		if d := days[date]; d != nil {
			out[date] = d.stats(date)
		}
	}
	return out
}

func (l *Ledger) Dates() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, 0, len(l.days))
	for date := range l.days {
		out = append(out, date)
	}
	sort.Strings(out)
	return out
}
