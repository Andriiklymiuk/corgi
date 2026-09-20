package cmd

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"andriiklymiuk/corgi/utils/atomicfile"
)

// Codex has one hook: `notify`, run after every turn with a JSON argument.
// track enable points it at corgi when codex is installed, so its sessions
// land on the board like Claude Code's; track disable takes it out again.

const codexNotifyLine = `notify = ["%s", "agent", "event", "stop", "--agent", "codex", "--notify"]`

var codexNotifyRe = regexp.MustCompile(`(?m)^notify\s*=.*$`)

func codexConfigPath() string {
	if home := strings.TrimSpace(os.Getenv("CODEX_HOME")); home != "" {
		return filepath.Join(home, "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex", "config.toml")
}

func codexInstalled() bool {
	_, err := exec.LookPath("codex")
	return err == nil
}

// enableCodexNotify writes corgi's notify line into codex's config: ours
// replaces an earlier one of ours; someone else's notify is left alone and
// reported, since codex runs only one.
func enableCodexNotify(path, bin string) (changed bool, theirs string, err error) {
	if path == "" {
		return false, "", nil
	}
	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, "", err
	}
	text := string(raw)
	line := strings.ReplaceAll(strings.ReplaceAll(codexNotifyLine, "%s", bin), `\`, `\\`)
	if m := codexNotifyRe.FindString(text); m != "" {
		if strings.Contains(m, `"agent", "event"`) {
			if m == line {
				return false, "", nil
			}
			return true, "", writeCodexConfig(path, codexNotifyRe.ReplaceAllLiteralString(text, line))
		}
		return false, m, nil
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	// Top-level keys must sit before any [table]; put ours at the top.
	return true, "", writeCodexConfig(path, line+"\n"+text)
}

func disableCodexNotify(path string) (bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	text := string(raw)
	m := codexNotifyRe.FindString(text)
	if m == "" || !strings.Contains(m, `"agent", "event"`) {
		return false, nil
	}
	out := strings.Replace(text, m+"\n", "", 1)
	if out == text {
		out = strings.Replace(text, m, "", 1)
	}
	return true, writeCodexConfig(path, out)
}

func writeCodexConfig(path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return atomicfile.Write(path, []byte(text), 0o600)
}
