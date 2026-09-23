package cmd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/atomicfile"
)

// A codex with hooks gets the same lifecycle hooks Claude Code has, in
// ~/.codex/hooks.json: every prompt, tool, permission and stop reaches the
// board, with the terminal it runs in, so a codex session can be focused,
// answered and typed into like a claude one. Codex runs a hook only after
// the person trusts it in /hooks; corgi never writes that trust itself. An
// older codex has one hook, `notify`, run after every turn; track enable
// falls back to it.

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

func codexHooksPath() string {
	if home := strings.TrimSpace(os.Getenv("CODEX_HOME")); home != "" {
		return filepath.Join(home, "hooks.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex", "hooks.json")
}

var codexHooksFeatureRe = regexp.MustCompile(`(?m)^hooks\s+\S+\s+true\s*$`)

// codexSupportsHooks asks codex itself: `codex features list` has a hooks row
// that is true once the installed codex runs hooks.
var codexSupportsHooks = func() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "codex", "features", "list").Output()
	return err == nil && codexHooksFeatureRe.Match(out)
}

var codexTrackedEvents = []struct {
	Event   string
	Matcher string
	Context bool
	Budget  bool
}{
	{Event: "SessionStart", Matcher: "startup|resume|clear", Context: true},
	{Event: "UserPromptSubmit"},
	{Event: "PreToolUse"},
	{Event: "PermissionRequest"},
	{Event: "PostToolUse"},
	{Event: "Stop", Budget: true},
	{Event: "SessionEnd"},
}

const hookEmitCodex = hookEmit + " --agent codex"

// enableCodexHooks writes corgi's hooks into codex's hooks.json, replacing
// earlier ones of ours and leaving everyone else's alone.
func enableCodexHooks(path, bin string) error {
	settings, err := readUserSettings(path)
	if err != nil {
		return err
	}
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	for _, ev := range codexTrackedEvents {
		emitTimeout := 5
		if ev.Event == "SessionEnd" {
			emitTimeout = 3
		}
		handlers := []any{map[string]any{"type": "command", "command": hookCommand(bin, hookEmitCodex), "timeout": emitTimeout}}
		if ev.Context {
			handlers = append(handlers, map[string]any{"type": "command", "command": hookCommand(bin, hookContext), "timeout": 5})
		}
		if ev.Budget {
			handlers = append(handlers, map[string]any{"type": "command", "command": hookCommand(bin, hookBudget), "timeout": 15})
		}
		entry := map[string]any{"hooks": handlers}
		if ev.Matcher != "" {
			entry["matcher"] = ev.Matcher
		}
		hooks[ev.Event] = append(stripTrackingHooks(hooks[ev.Event]), entry)
	}
	settings["hooks"] = hooks
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeJSONObject(path, settings)
}

// trackCodex hooks codex up the best way the installed one allows: hooks
// when it runs them (and the older notify line of ours goes, so a turn is
// not reported twice), notify when it does not.
func trackCodex(bin string) {
	if codexSupportsHooks() {
		if err := enableCodexHooks(codexHooksPath(), bin); err != nil {
			utils.Infof("codex: could not write %s: %v\n", codexHooksPath(), err)
			return
		}
		if removed, err := disableCodexNotify(codexConfigPath()); err == nil && removed {
			utils.Infof("✓ codex's notify line is replaced by its hooks (%s)\n", codexConfigPath())
		}
		utils.Infof("✓ codex sessions are tracked like claude's: prompts, tools, permissions, stops (%s)\n", codexHooksPath())
		utils.Info("codex runs a hook only once you trust it: open codex, type /hooks and trust the corgi ones")
		return
	}
	switch changed, theirs, err := enableCodexNotify(codexConfigPath(), bin); {
	case err != nil:
		utils.Infof("codex: could not write %s: %v\n", codexConfigPath(), err)
	case theirs != "":
		utils.Infof("codex: %s already has a notify of its own; codex runs one, so its turns stay off the board (%s)\n", codexConfigPath(), theirs)
	case changed:
		utils.Infof("✓ codex turns land on the board too (%s) - a codex with hooks gets the full set; `codex update` then track enable again\n", codexConfigPath())
	}
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

var codexAutoReviewRe = regexp.MustCompile(`(?m)^\s*approvals_reviewer\s*=\s*"auto_review"`)

// codexDecidesItself says a codex permission request is answered without a
// person: the session bypasses approvals, or its config hands them to
// codex's auto reviewer (the project's .codex/config.toml, then the user's).
func codexDecidesItself(permissionMode, cwd string) bool {
	if permissionMode == "bypassPermissions" {
		return true
	}
	paths := []string{codexConfigPath()}
	if cwd != "" {
		if root := sessions.RepoRoot(cwd); root != "" {
			paths = append([]string{filepath.Join(root, ".codex", "config.toml")}, paths...)
		}
		paths = append([]string{filepath.Join(cwd, ".codex", "config.toml")}, paths...)
	}
	for _, p := range paths {
		if raw, err := os.ReadFile(p); err == nil && codexAutoReviewRe.Match(codexTopLevel(raw)) {
			return true
		}
	}
	return false
}

// codexTopLevel is the part of a codex config.toml before its first table;
// a key under [profiles.x] is not the default.
func codexTopLevel(raw []byte) []byte {
	if i := regexp.MustCompile(`(?m)^\s*\[`).FindIndex(raw); i != nil {
		return raw[:i[0]]
	}
	return raw
}
