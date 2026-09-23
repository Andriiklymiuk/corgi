// Package harness is the one place corgi knows how to drive a coding agent
// from the outside: which binary, how to ask it for one unattended run, how
// to read the receipt it prints, how to open it for a person. Claude Code
// is the first; Codex the second; the next one is one more entry in the
// table, not a search for "claude" through the daemon.
package harness

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

const (
	Claude = "claude"
	Codex  = "codex"
)

// Print is one unattended run: a prompt in, a result out.
type Print struct {
	Prompt string
	// System rides along as extra instructions; a harness without a flag
	// for it puts it in front of the prompt.
	System string
	Model  string
	// Resume continues an earlier session by id.
	Resume string
	// SkipPermissions runs with no sandbox and no approvals at all;
	// otherwise edits are accepted and anything riskier is refused.
	SkipPermissions bool
	// Text asks for the plain final message instead of a JSON receipt.
	Text bool
}

// Open is one interactive session for a person: how much it may do, which
// model, what rides along as instructions, what it continues, and its
// first message. Each harness spells it in its own flags.
type Open struct {
	// PermissionMode in claude's words: default, acceptEdits, plan,
	// bypassPermissions; "" leaves the harness to its own default.
	PermissionMode string
	Model          string
	// System is extra instructions (a bot's soul); a harness without a
	// flag for it puts it in front of the prompt.
	System string
	// Resume continues an earlier session by id; Fork continues it as a
	// new session where the harness can.
	Resume string
	Fork   bool
	Prompt string
}

// Receipt is what a run cost and what it said, when the harness told us.
type Receipt struct {
	OK      bool
	CostUSD float64
	Tokens  int64
	Turns   int
	// SessionID is the id to resume this run by, when the harness names it.
	SessionID string
}

type Harness struct {
	Name string
	// Bin is the executable; a config may point it elsewhere.
	Bin string
	// ConfigDirEnv names the env var that moves the agent's home, so one
	// laptop can hold several accounts.
	ConfigDirEnv string
	// CredentialEnv are the env vars that carry an API key or token.
	CredentialEnv []string
	// SessionIDEnv is set inside a session to its own id; corgi reads it
	// to know which session a hook came from.
	SessionIDEnv string
	// PrintArgs builds the argv (after the binary) for one unattended run.
	PrintArgs func(Print) []string
	// Unwrap reads the run's stdout: the final message, and the receipt.
	Unwrap func(raw []byte) ([]byte, Receipt)
	// InteractiveArgs opens the agent for a person: the permission mode
	// (claude's words: default, acceptEdits, plan, bypassPermissions) and
	// whatever else the caller adds.
	InteractiveArgs func(permissionMode string) []string
	// OpenArgs builds the argv for one interactive session.
	OpenArgs func(Open) []string
	// Hooks says whether the agent tells corgi what it is doing (Claude
	// Code hooks); without them a session's board row is coarse.
	Hooks bool
}

var table = map[string]Harness{
	Claude: {
		Name:            Claude,
		Bin:             "claude",
		ConfigDirEnv:    "CLAUDE_CONFIG_DIR",
		CredentialEnv:   []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN"},
		SessionIDEnv:    "CLAUDE_SESSION_ID",
		PrintArgs:       claudePrintArgs,
		Unwrap:          claudeUnwrap,
		InteractiveArgs: claudeInteractiveArgs,
		OpenArgs:        claudeOpenArgs,
		Hooks:           true,
	},
	Codex: {
		Name:            Codex,
		Bin:             "codex",
		ConfigDirEnv:    "CODEX_HOME",
		CredentialEnv:   []string{"OPENAI_API_KEY", "CODEX_API_KEY"},
		SessionIDEnv:    "CODEX_THREAD_ID",
		PrintArgs:       codexPrintArgs,
		Unwrap:          codexUnwrap,
		InteractiveArgs: codexInteractiveArgs,
		OpenArgs:        codexOpenArgs,
	},
}

// A model name belongs to one harness; the other has no such model and
// would refuse the run. A name the other harness owns is dropped, so the
// run goes ahead on the harness's own default.
var (
	claudeModel = regexp.MustCompile(`(?i)^(opus|sonnet|haiku|opusplan|claude-)`)
	codexModel  = regexp.MustCompile(`(?i)^(gpt-|o[0-9]|codex-)`)
)

// Model is the model to ask this harness for: name when it is its own,
// or unknown to both; "" when it belongs to the other harness.
func (h Harness) Model(name string) string {
	name = strings.TrimSpace(name)
	switch h.Name {
	case Claude:
		if codexModel.MatchString(name) {
			return ""
		}
	case Codex:
		if claudeModel.MatchString(name) {
			return ""
		}
	}
	return name
}

// For is the harness a config's kind names; "" and "custom" mean Claude
// Code, with bin pointing wherever the config says.
func For(kind, bin string) Harness {
	name := strings.ToLower(strings.TrimSpace(kind))
	h, ok := table[name]
	if !ok {
		h = table[Claude]
	}
	if b := strings.TrimSpace(bin); b != "" {
		h.Bin = b
	}
	return h
}

func Names() []string {
	out := make([]string, 0, len(table))
	for n := range table {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Known says whether kind names a harness in the table.
func Known(kind string) bool {
	_, ok := table[strings.ToLower(strings.TrimSpace(kind))]
	return ok
}

// Installed says whether the harness's binary is on PATH.
func (h Harness) Installed() bool {
	_, err := exec.LookPath(h.Bin)
	return err == nil
}

// Command is one unattended run, in dir, with env on top of the process's.
func (h Harness) Command(ctx context.Context, dir string, env []string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, h.Bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	return cmd
}

// ---- Claude Code ----------------------------------------------------------

func claudePrintArgs(p Print) []string {
	args := []string{"-p", p.Prompt}
	if p.Text {
		args = append(args, "--output-format", "text")
	} else {
		args = append(args, "--output-format", "json")
	}
	if p.Resume != "" {
		args = append(args, "--resume", p.Resume)
	}
	if p.System != "" {
		args = append(args, "--append-system-prompt", p.System)
	}
	if m := (Harness{Name: Claude}).Model(p.Model); m != "" {
		args = append(args, "--model", m)
	}
	if p.SkipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	} else {
		args = append(args, "--permission-mode", "acceptEdits")
	}
	return args
}

func claudeInteractiveArgs(mode string) []string {
	if mode = strings.TrimSpace(mode); mode != "" {
		return []string{"--permission-mode", mode}
	}
	return nil
}

func claudeOpenArgs(o Open) []string {
	args := claudeInteractiveArgs(o.PermissionMode)
	if m := (Harness{Name: Claude}).Model(o.Model); m != "" {
		args = append(args, "--model", m)
	}
	if o.System != "" {
		args = append(args, "--append-system-prompt", o.System)
	}
	if o.Resume != "" {
		args = append(args, "--resume", o.Resume)
		if o.Fork {
			args = append(args, "--fork-session")
		}
	}
	if o.Prompt != "" {
		args = append(args, o.Prompt)
	}
	return args
}

func claudeUnwrap(raw []byte) ([]byte, Receipt) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return raw, Receipt{}
	}
	var env struct {
		Type      string  `json:"type"`
		Result    string  `json:"result"`
		CostUSD   float64 `json:"total_cost_usd"`
		NumTurns  int     `json:"num_turns"`
		SessionID string  `json:"session_id"`
		Usage     struct {
			Input      int64 `json:"input_tokens"`
			Output     int64 `json:"output_tokens"`
			CacheRead  int64 `json:"cache_read_input_tokens"`
			CacheWrite int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(trimmed, &env) != nil || env.Type != "result" {
		return raw, Receipt{}
	}
	u := env.Usage
	return []byte(env.Result), Receipt{OK: true, CostUSD: env.CostUSD, Turns: env.NumTurns, SessionID: env.SessionID,
		Tokens: u.Input + u.Output + u.CacheRead + u.CacheWrite}
}

// ---- Codex ----------------------------------------------------------------

// codex exec runs one turn without a terminal; --json streams JSONL events
// and the final agent message is the last item.completed. There is no
// system-prompt flag, so the soul rides in front of the prompt.

func codexPrintArgs(p Print) []string {
	args := []string{"exec"}
	if p.Resume != "" {
		args = append(args, "resume", p.Resume)
	}
	args = append(args, "--skip-git-repo-check")
	if !p.Text {
		args = append(args, "--json")
	}
	if m := (Harness{Name: Codex}).Model(p.Model); m != "" {
		args = append(args, "-m", m)
	}
	if p.SkipPermissions {
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
	} else {
		args = append(args, "--full-auto")
	}
	prompt := codexSkillPrompt(p.Prompt)
	if s := strings.TrimSpace(p.System); s != "" {
		prompt = s + "\n\n" + prompt
	}
	return append(args, "--", prompt)
}

var corgiSlashSkill = regexp.MustCompile(`/corgi:([a-z][a-z0-9-]*)`)

func codexSkillPrompt(prompt string) string {
	var names []string
	seen := map[string]bool{}
	out := corgiSlashSkill.ReplaceAllStringFunc(prompt, func(m string) string {
		name := corgiSlashSkill.FindStringSubmatch(m)[1]
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
		return "$" + name
	})
	if len(names) == 0 {
		return prompt
	}
	var files []string
	for _, n := range names {
		files = append(files, "$"+n+" is ~/.codex/skills/"+n+"/SKILL.md")
	}
	return out + "\n\nSkills: " + strings.Join(files, "; ") + " - read it whole, then follow it."
}

func codexInteractiveArgs(mode string) []string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "bypasspermissions":
		return []string{"--dangerously-bypass-approvals-and-sandbox"}
	case "acceptedits":
		return []string{"--full-auto"}
	case "":
		return nil
	default:
		return []string{"-a", "on-request"}
	}
}

// codex resume <id> continues a thread; there is no fork, so a fork is a
// plain resume. Instructions ride in front of the prompt, as in exec.
func codexOpenArgs(o Open) []string {
	var args []string
	if o.Resume != "" {
		args = append(args, "resume", o.Resume)
	}
	args = append(args, codexInteractiveArgs(o.PermissionMode)...)
	if m := (Harness{Name: Codex}).Model(o.Model); m != "" {
		args = append(args, "-m", m)
	}
	prompt := o.Prompt
	if s := strings.TrimSpace(o.System); s != "" {
		if prompt != "" {
			prompt = s + "\n\n" + prompt
		} else {
			prompt = s
		}
	}
	if prompt != "" {
		args = append(args, prompt)
	}
	return args
}

func codexUnwrap(raw []byte) ([]byte, Receipt) {
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	var (
		text string
		rc   Receipt
		seen bool
	)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var ev struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
			Item     struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
			Usage struct {
				Input  int64 `json:"input_tokens"`
				Cached int64 `json:"cached_input_tokens"`
				Output int64 `json:"output_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(line, &ev) != nil {
			continue
		}
		seen = true
		switch ev.Type {
		case "thread.started":
			rc.SessionID = ev.ThreadID
		case "item.completed":
			if ev.Item.Type == "agent_message" && ev.Item.Text != "" {
				text = ev.Item.Text
			}
		case "turn.completed":
			rc.OK = true
			rc.Turns++
			rc.Tokens += ev.Usage.Input + ev.Usage.Output
		}
	}
	if !seen {
		return raw, Receipt{}
	}
	return []byte(text), rc
}
