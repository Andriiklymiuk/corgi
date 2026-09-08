package usage

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Sample is one reading of an account's limits, kept so the slope says
// where the numbers are heading. Only readings Claude Code actually fetched
// are kept: the daemon polls every minute, /usage refreshes when a session
// asks, and two polls of one fetch are one fact.
type Sample struct {
	At        time.Time `json:"at"`
	FetchedAt time.Time `json:"fetchedAt"`
	FiveHour  int       `json:"fiveHour"`
	SevenDay  int       `json:"sevenDay"`
}

const (
	// samplesKeep bounds the file: a day of one-a-minute readings and change.
	samplesKeep = 2000
	samplesTrim = 2600
	// forecastSpan is how far back the slope looks. Longer flattens a fresh
	// burst of work; shorter reacts to it.
	forecastSpan = 90 * time.Minute
	// forecastMinSpread is the least time two readings must be apart before
	// a rate between them means anything.
	forecastMinSpread = 8 * time.Minute
)

// SamplesPath is where one account's readings live under the agent dir.
func SamplesPath(agentDir, profile string) string {
	name := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, profile)
	if name == "" {
		name = "default"
	}
	return filepath.Join(agentDir, "usage", name+".jsonl")
}

// RecordSample appends l to the account's file when it is a reading not yet
// on file. Returns whether it wrote.
func RecordSample(agentDir, profile string, l Limits, now time.Time) (bool, error) {
	path := SamplesPath(agentDir, profile)
	last, count := lastSample(path)
	if count > 0 && !l.FetchedAt.After(last.FetchedAt) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}
	s := Sample{At: now.UTC(), FetchedAt: l.FetchedAt.UTC(), FiveHour: l.FiveHour.Percent, SevenDay: l.SevenDay.Percent}
	data, err := json.Marshal(s)
	if err != nil {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return false, err
	}
	_, err = f.Write(append(data, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return false, err
	}
	if count+1 > samplesTrim {
		trimSamples(path)
	}
	return true, nil
}

func lastSample(path string) (Sample, int) {
	all := LoadSamples(path, time.Time{})
	if len(all) == 0 {
		return Sample{}, 0
	}
	return all[len(all)-1], len(all)
}

func trimSamples(path string) {
	all := LoadSamples(path, time.Time{})
	if len(all) <= samplesKeep {
		return
	}
	all = all[len(all)-samplesKeep:]
	var buf strings.Builder
	for _, s := range all {
		data, err := json.Marshal(s)
		if err != nil {
			continue
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}
	_ = os.WriteFile(path, []byte(buf.String()), 0o600)
}

// LoadSamples reads the readings at path fetched after since (zero: all),
// oldest first. A missing file is no readings.
func LoadSamples(path string, since time.Time) []Sample {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []Sample
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var s Sample
		if json.Unmarshal(sc.Bytes(), &s) != nil || s.FetchedAt.IsZero() {
			continue
		}
		if !since.IsZero() && s.FetchedAt.Before(since) {
			continue
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FetchedAt.Before(out[j].FetchedAt) })
	return out
}

// Forecast is where each limit is heading at the current pace.
type Forecast struct {
	FiveHour *WindowForecast `json:"fiveHour,omitempty"`
	SevenDay *WindowForecast `json:"sevenDay,omitempty"`
}

// WindowForecast is one limit's slope. ExhaustAt is when it reaches 100% at
// this pace, absent when the pace is flat or falling. Safe says whether the
// reset comes first: true means "keep going", false means "this window will
// run out before it resets".
type WindowForecast struct {
	PercentPerHour float64   `json:"percentPerHour"`
	ExhaustAt      time.Time `json:"exhaustAt,omitempty"`
	Safe           bool      `json:"safe"`
	Samples        int       `json:"samples"`
}

// ForecastFrom fits a line through the recent readings of each window and
// projects it. nil when there is not enough to say anything.
func ForecastFrom(samples []Sample, l Limits, now time.Time) *Forecast {
	recent := samples[:0:0]
	for _, s := range samples {
		if now.Sub(s.FetchedAt) <= forecastSpan {
			recent = append(recent, s)
		}
	}
	five := windowForecast(recent, func(s Sample) int { return s.FiveHour }, l.FiveHour, now)
	seven := windowForecast(recent, func(s Sample) int { return s.SevenDay }, l.SevenDay, now)
	if five == nil && seven == nil {
		return nil
	}
	return &Forecast{FiveHour: five, SevenDay: seven}
}

func windowForecast(samples []Sample, pick func(Sample) int, w Window, now time.Time) *WindowForecast {
	if len(samples) < 2 {
		return nil
	}
	first, last := samples[0], samples[len(samples)-1]
	if last.FetchedAt.Sub(first.FetchedAt) < forecastMinSpread {
		return nil
	}
	// Least squares over hours since the first reading; a reset inside the
	// span shows as a drop, and the fit then says "falling", which is true.
	var sx, sy, sxx, sxy float64
	n := float64(len(samples))
	for _, s := range samples {
		x := s.FetchedAt.Sub(first.FetchedAt).Hours()
		y := float64(pick(s))
		sx, sy, sxx, sxy = sx+x, sy+y, sxx+x*x, sxy+x*y
	}
	den := n*sxx - sx*sx
	if den == 0 {
		return nil
	}
	rate := (n*sxy - sx*sy) / den
	out := &WindowForecast{PercentPerHour: math.Round(rate*10) / 10, Samples: len(samples), Safe: true}
	if rate <= 0.05 {
		return out
	}
	left := float64(100 - w.Percent)
	if left < 0 {
		left = 0
	}
	out.ExhaustAt = now.Add(time.Duration(left / rate * float64(time.Hour)))
	if !w.ResetsAt.IsZero() {
		out.Safe = out.ExhaustAt.After(w.ResetsAt)
	} else {
		out.Safe = out.ExhaustAt.After(now.Add(5 * time.Hour))
	}
	return out
}
