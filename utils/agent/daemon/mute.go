package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils/atomicfile"
)

// A mute is one file with a time in it: until then, nothing rings — no
// desktop toast, no phone push — while the inbox, the board and the rows
// go on as before. A key, the bar or the phone flips it; the CLI too.

// MutePath is where the mute lives.
func MutePath(agentDir string) string { return filepath.Join(agentDir, "muted-until") }

// MutedUntil reads the mute: zero when nothing is muted, or it has passed.
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

// SetMute writes the mute, or removes it for a zero time.
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

// muted says whether the daemon holds its tongue right now, and keeps the
// board's word on it current.
func (d *Daemon) muted() bool {
	until := MutedUntil(d.Dir)
	if d.Sessions != nil && d.Sessions.SetMuted(until) {
		d.flushSessions()
	}
	return !until.IsZero()
}
