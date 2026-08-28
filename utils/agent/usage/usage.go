// Package usage sums the token counts Claude Code already records in its
// transcripts, so a workspace can answer what it has been costing. Nothing is
// sent anywhere: the files are local and only their usage numbers are read.
package usage

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Totals struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cacheRead"`
	CacheWrite int64 `json:"cacheWrite"`
	Turns      int64 `json:"turns"`
}

// Total is what a person means by "how much did this cost": everything that
// counts against the window, cache reads included.
func (t Totals) Total() int64 { return t.Input + t.Output + t.CacheRead + t.CacheWrite }

func (t *Totals) add(o Totals) {
	t.Input += o.Input
	t.Output += o.Output
	t.CacheRead += o.CacheRead
	t.CacheWrite += o.CacheWrite
	t.Turns += o.Turns
}

// Report is one workspace's usage over two windows.
type Report struct {
	Today Totals `json:"today"`
	Week  Totals `json:"week"`
}

// maxLineBytes bounds the scanner: a transcript line holds a whole turn, and a
// long one must not stop the sum.
const maxLineBytes = 8 << 20

// ForDir sums the usage in every transcript for absPath under the account's
// config dir. Best-effort: unreadable or malformed files contribute nothing.
func ForDir(absPath, configDir, projectDirName string, now time.Time) Report {
	base := configDir
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Report{}
		}
		base = filepath.Join(home, ".claude")
	}
	dir := filepath.Join(base, "projects", projectDirName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Report{}
	}

	dayAgo := now.Add(-24 * time.Hour)
	weekAgo := now.Add(-7 * 24 * time.Hour)
	var rep Report
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		// A transcript last written before the window cannot hold a turn
		// inside it, and skipping the read is what keeps this cheap.
		if err != nil || info.ModTime().Before(weekAgo) {
			continue
		}
		day, week := sumFile(filepath.Join(dir, e.Name()), dayAgo, weekAgo)
		rep.Today.add(day)
		rep.Week.add(week)
	}
	return rep
}

func sumFile(path string, dayAgo, weekAgo time.Time) (day, week Totals) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), maxLineBytes)
	for sc.Scan() {
		var row struct {
			Timestamp time.Time `json:"timestamp"`
			Message   struct {
				Usage *rawUsage `json:"usage"`
			} `json:"message"`
			Usage *rawUsage `json:"usage"`
		}
		if json.Unmarshal(sc.Bytes(), &row) != nil {
			continue
		}
		u := row.Message.Usage
		if u == nil {
			u = row.Usage
		}
		if u == nil || row.Timestamp.Before(weekAgo) {
			continue
		}
		t := u.totals()
		week.add(t)
		if !row.Timestamp.Before(dayAgo) {
			day.add(t)
		}
	}
	return day, week
}

type rawUsage struct {
	Input      int64 `json:"input_tokens"`
	Output     int64 `json:"output_tokens"`
	CacheRead  int64 `json:"cache_read_input_tokens"`
	CacheWrite int64 `json:"cache_creation_input_tokens"`
}

func (u rawUsage) totals() Totals {
	return Totals{
		Input: u.Input, Output: u.Output,
		CacheRead: u.CacheRead, CacheWrite: u.CacheWrite,
		Turns: 1,
	}
}
