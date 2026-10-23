package sessions

import (
	"encoding/json"
	"time"
)

type LeftItem struct {
	ID        string    `json:"id"`
	Workspace string    `json:"workspace"`
	Ref       string    `json:"ref,omitempty"`
	Title     string    `json:"title,omitempty"`
	URL       string    `json:"url,omitempty"`
	Text      string    `json:"text"`
	Kind      string    `json:"kind"`
	At        time.Time `json:"at"`
	DoneAt    time.Time `json:"doneAt,omitzero"`
	DoneBy    string    `json:"doneBy,omitempty"`
}

type StalledPull struct {
	Workspace string    `json:"workspace,omitempty"`
	Ref       string    `json:"ref"`
	Link      string    `json:"link"`
	Why       string    `json:"why"`
	Action    string    `json:"action"`
	Prompt    string    `json:"prompt,omitempty"`
	Since     time.Time `json:"since"`
}

type DaySpend struct {
	Day  string  `json:"day"`
	USD  float64 `json:"usd"`
	Runs int     `json:"runs"`
}

type RunSpend struct {
	WeekUSD       float64    `json:"weekUSD"`
	WeekRuns      int        `json:"weekRuns"`
	NothingUSD    float64    `json:"nothingUSD"`
	NothingRuns   int        `json:"nothingRuns"`
	MergedPulls   int        `json:"mergedPulls"`
	PerMergedPull float64    `json:"perMergedPull,omitempty"`
	FailedRuns    int        `json:"failedRuns"`
	SkippedChecks int        `json:"skippedChecks,omitempty"`
	Days          []DaySpend `json:"days,omitempty"`
}

type RunCheck struct {
	Headline string   `json:"headline"`
	Days     int      `json:"days"`
	Runs     int      `json:"runs"`
	Lines    []string `json:"lines,omitempty"`
}

type Work struct {
	Left    []LeftItem    `json:"left,omitempty"`
	Stalled []StalledPull `json:"stalled,omitempty"`
	Spend   *RunSpend     `json:"runSpend,omitempty"`
	Check   *RunCheck     `json:"selfCheck,omitempty"`
}

func (r *Registry) SetWork(w Work) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	was, _ := json.Marshal(r.work)
	now, _ := json.Marshal(w)
	if string(was) == string(now) {
		return false
	}
	r.work = w
	r.touch()
	return true
}
