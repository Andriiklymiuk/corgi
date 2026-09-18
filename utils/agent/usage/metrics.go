package usage

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type Wait struct {
	At      time.Time `json:"at"`
	Kind    string    `json:"kind"`
	Label   string    `json:"label"`
	Profile string    `json:"profile,omitempty"`
	Seconds int       `json:"seconds"`
}

const waitsKeep = 5000

func WaitsPath(agentDir string) string { return filepath.Join(agentDir, "waits.jsonl") }

func RecordWait(agentDir string, w Wait) error {
	path := WaitsPath(agentDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(w)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func LoadWaits(agentDir string, since time.Time) []Wait {
	f, err := os.Open(WaitsPath(agentDir))
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []Wait
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var w Wait
		if json.Unmarshal(sc.Bytes(), &w) != nil || w.At.Before(since) {
			continue
		}
		out = append(out, w)
	}
	if len(out) > waitsKeep {
		out = out[len(out)-waitsKeep:]
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

type WaitSummary struct {
	Count        int    `json:"count"`
	Median       int    `json:"medianS"`
	Longest      int    `json:"longestS"`
	LongestLabel string `json:"longestLabel,omitempty"`
	Total        int    `json:"totalS"`
}

func Summarize(waits []Wait, kind string) WaitSummary {
	var secs []int
	var out WaitSummary
	for _, w := range waits {
		if w.Kind != kind {
			continue
		}
		secs = append(secs, w.Seconds)
		out.Total += w.Seconds
		if w.Seconds > out.Longest {
			out.Longest, out.LongestLabel = w.Seconds, w.Label
		}
	}
	out.Count = len(secs)
	if len(secs) > 0 {
		sort.Ints(secs)
		out.Median = secs[len(secs)/2]
	}
	return out
}
