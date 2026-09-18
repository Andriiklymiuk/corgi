package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils/atomicfile"
)

func MutePath(agentDir string) string { return filepath.Join(agentDir, "muted-until") }

func MutedUntil(agentDir string) time.Time {
	raw, err := os.ReadFile(MutePath(agentDir))
	if err != nil {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(raw)))
	if err != nil || !t.After(time.Now()) {
		return time.Time{}
	}
	return t
}

func SetMute(agentDir string, until time.Time) error {
	if until.IsZero() {
		err := os.Remove(MutePath(agentDir))
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return atomicfile.Write(MutePath(agentDir), []byte(until.UTC().Format(time.RFC3339)+"\n"), 0o600)
}

func (d *Daemon) muted() bool {
	until := MutedUntil(d.Dir)
	if d.Sessions != nil && d.Sessions.SetMuted(until) {
		d.flushSessions()
	}
	return !until.IsZero()
}
