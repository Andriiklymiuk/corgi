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
	},
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
	if p.Model != "" {
		args = append(args, "--model", p.Model)
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
	if p.Model != "" {
		args = append(args, "-m", p.Model)
	}
	if p.SkipPermissions {
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
	} else {
		args = append(args, "--full-auto")
	}
	prompt := p.Prompt
	if s := strings.TrimSpace(p.System); s != "" {
		prompt = s + "\n\n" + prompt
	}
	return append(args, "--", prompt)
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
