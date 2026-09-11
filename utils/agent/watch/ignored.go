package watch

import (
	"encoding/json"
	"os"
	"path/filepath"

	"andriiklymiuk/corgi/utils/atomicfile"
)

// The ignored list is a person's decision, and two processes act on it: the
// phone writes it, the daemon reads it. It lives in its own file so nobody's
// in-memory copy of state.json can write over it, and it is read fresh every
// time so the daemon sees an ignore it did not make.
type ignoredFile struct {
	Ignored []string `json:"ignored"`
}

func ignoredPath(agentDir string) string {
	return filepath.Join(agentDir, "watch", "ignored.json")
}

func readIgnored(agentDir string) []string {
	var f ignoredFile
	if data, err := os.ReadFile(ignoredPath(agentDir)); err == nil {
		_ = json.Unmarshal(data, &f)
	}
	return f.Ignored
}

func writeIgnored(agentDir string, keys []string) error {
	if len(keys) > seenKeep {
		keys = keys[len(keys)-seenKeep:]
	}
	data, err := json.MarshalIndent(ignoredFile{Ignored: keys}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(ignoredPath(agentDir)), 0o700); err != nil {
		return err
	}
	return atomicfile.Write(ignoredPath(agentDir), data, 0o600)
}

// Ignore drops an event from the inbox for good and stops the unattended
// mode picking it up. A person's decision, never corgi's.
func (s *State) Ignore(key string) error {
	if key == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := readIgnored(s.agentDir)
	if containsString(keys, key) {
		return nil
	}
	return writeIgnored(s.agentDir, append(keys, key))
}

// Unignore puts it back, which is what undoing a run has to do.
func (s *State) Unignore(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := readIgnored(s.agentDir)
	kept := keys[:0]
	for _, k := range keys {
		if k != key {
			kept = append(kept, k)
		}
	}
	if len(kept) == len(keys) {
		return nil
	}
	return writeIgnored(s.agentDir, kept)
}

func (s *State) IsIgnored(key string) bool {
	return containsString(readIgnored(s.agentDir), key)
}

// moveIgnoredOut carries a list an older corgi kept inside state.json into
// its own file, once. The field stays readable so the move can happen, and
// is cleared so the next save does not write it back.
func (s *State) moveIgnoredOut() {
	if len(s.Ignored) == 0 {
		return
	}
	keys := readIgnored(s.agentDir)
	for _, k := range s.Ignored {
		if !containsString(keys, k) {
			keys = append(keys, k)
		}
	}
	if writeIgnored(s.agentDir, keys) == nil {
		s.Ignored = nil
	}
}
