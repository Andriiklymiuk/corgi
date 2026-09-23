package watch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"andriiklymiuk/corgi/utils/atomicfile"
)

// hooked.json is when a signed webhook last came in, per source. The webhook
// handler alone writes it, on every delivery that passes the signature,
// news or not, so "the hook arrives" shows even when every comment was mine.

func hookedPath(agentDir string) string { return filepath.Join(agentDir, "watch", "hooked.json") }

func LoadHooked(agentDir string) map[string]string {
	out := map[string]string{}
	if agentDir == "" {
		return out
	}
	if raw, err := os.ReadFile(hookedPath(agentDir)); err == nil {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

func MarkHooked(agentDir, source string, now time.Time) error {
	all := LoadHooked(agentDir)
	all[source] = now.UTC().Format(time.RFC3339)
	raw, err := json.Marshal(all)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(hookedPath(agentDir)), 0o700); err != nil {
		return err
	}
	return atomicfile.Write(hookedPath(agentDir), raw, 0o600)
}
