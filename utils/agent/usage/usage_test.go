package usage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func write(t *testing.T, path string, rows []string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	body := ""
	for _, r := range rows {
		body += r + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func turn(at time.Time, in, out, read, write int) string {
	row := map[string]any{
		"timestamp": at.Format(time.RFC3339Nano),
		"message": map[string]any{"usage": map[string]int{
			"input_tokens": in, "output_tokens": out,
			"cache_read_input_tokens": read, "cache_creation_input_tokens": write,
		}},
	}
	b, _ := json.Marshal(row)
	return string(b)
}

func TestForDirSumsTodayAndWeek(t *testing.T) {
	cfg := t.TempDir()
	now := time.Now()
	write(t, filepath.Join(cfg, "projects", "-dev-app", "a.jsonl"), []string{
		turn(now.Add(-time.Hour), 10, 5, 100, 20),
		turn(now.Add(-3*24*time.Hour), 1, 1, 1, 1),
		turn(now.Add(-30*24*time.Hour), 999, 999, 999, 999),
		"{not json",
		`{"timestamp":"` + now.Format(time.RFC3339) + `","message":{}}`,
	})

	rep := ForDir("/dev/app", cfg, "-dev-app", now)
	if rep.Today.Turns != 1 || rep.Today.Total() != 135 {
		t.Errorf("today = %+v (total %d), want the one recent turn", rep.Today, rep.Today.Total())
	}
	if rep.Week.Turns != 2 || rep.Week.Total() != 139 {
		t.Errorf("week = %+v (total %d), want both turns inside the week", rep.Week, rep.Week.Total())
	}
}

func TestForDirSumsEveryTranscript(t *testing.T) {
	cfg := t.TempDir()
	now := time.Now()
	dir := filepath.Join(cfg, "projects", "-dev-app")
	write(t, filepath.Join(dir, "a.jsonl"), []string{turn(now, 1, 2, 3, 4)})
	write(t, filepath.Join(dir, "b.jsonl"), []string{turn(now, 1, 2, 3, 4)})
	write(t, filepath.Join(dir, "notes.txt"), []string{"ignored"})

	rep := ForDir("/dev/app", cfg, "-dev-app", now)
	if rep.Today.Turns != 2 || rep.Today.Total() != 20 {
		t.Errorf("report = %+v", rep.Today)
	}
}

func TestForDirMissingIsZero(t *testing.T) {
	rep := ForDir("/dev/app", t.TempDir(), "-dev-nope", time.Now())
	if rep.Week.Total() != 0 || rep.Today.Turns != 0 {
		t.Errorf("a missing project dir must be zero, got %+v", rep)
	}
}

func TestForDirSkipsStaleFilesWithoutReading(t *testing.T) {
	cfg := t.TempDir()
	now := time.Now()
	path := filepath.Join(cfg, "projects", "-dev-app", "old.jsonl")
	write(t, path, []string{turn(now, 5, 5, 5, 5)})
	old := now.Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if rep := ForDir("/dev/app", cfg, "-dev-app", now); rep.Week.Total() != 0 {
		t.Errorf("a transcript untouched for a month must be skipped, got %+v", rep.Week)
	}
}

func TestTopLevelUsageAlsoCounts(t *testing.T) {
	cfg := t.TempDir()
	now := time.Now()
	row := fmt.Sprintf(`{"timestamp":"%s","usage":{"input_tokens":7,"output_tokens":3}}`, now.Format(time.RFC3339))
	write(t, filepath.Join(cfg, "projects", "-dev-app", "a.jsonl"), []string{row})
	if rep := ForDir("/dev/app", cfg, "-dev-app", now); rep.Today.Total() != 10 {
		t.Errorf("a top-level usage object must count, got %+v", rep.Today)
	}
}
