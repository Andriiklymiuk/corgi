package usage

import (
	"bufio"
	"encoding/json"
	"io"
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

func (t Totals) Total() int64 { return t.Input + t.Output + t.CacheRead + t.CacheWrite }

func (t Totals) Plus(o Totals) Totals {
	t.add(o)
	return t
}

func (t *Totals) add(o Totals) {
	t.Input += o.Input
	t.Output += o.Output
	t.CacheRead += o.CacheRead
	t.CacheWrite += o.CacheWrite
	t.Turns += o.Turns
}

type Report struct {
	Today Totals `json:"today"`
	Week  Totals `json:"week"`
}

const maxLineBytes = 8 << 20

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

func ProjectDirName(dir string) string {
	return strings.NewReplacer("/", "-", ".", "-", "\\", "-", ":", "-").Replace(dir)
}

func TranscriptPath(configDir, cwd, sessionID string) string {
	base := configDir
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".claude")
	}
	return filepath.Join(base, "projects", ProjectDirName(cwd), sessionID+".jsonl")
}

func SumFrom(path string, offset int64) (Totals, int64) {
	f, err := os.Open(path)
	if err != nil {
		return Totals{}, offset
	}
	defer f.Close()
	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return Totals{}, offset
		}
	}
	var t Totals
	r := bufio.NewReaderSize(f, 64<<10)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return t, offset
		}
		offset += int64(len(line))
		var row struct {
			Message struct {
				Usage *rawUsage `json:"usage"`
			} `json:"message"`
			Usage *rawUsage `json:"usage"`
		}
		if json.Unmarshal(line, &row) != nil {
			continue
		}
		u := row.Message.Usage
		if u == nil {
			u = row.Usage
		}
		if u != nil {
			t.add(u.totals())
		}
	}
}

func ForSession(configDir, cwd, sessionID string) (Totals, bool) {
	path := TranscriptPath(configDir, cwd, sessionID)
	if path == "" {
		return Totals{}, false
	}
	if _, err := os.Stat(path); err != nil {
		return Totals{}, false
	}
	_, all := sumFile(path, time.Time{}, time.Time{})
	return all, true
}
