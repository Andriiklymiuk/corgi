package watch

import (
	"encoding/json"
	"os"
	"path/filepath"

	"andriiklymiuk/corgi/utils/atomicfile"
)

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
