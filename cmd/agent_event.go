package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/proc"
	"andriiklymiuk/corgi/utils/agent/sessions"
)

var eventNames = map[string]string{
	"start":      "SessionStart",
	"prompt":     "UserPromptSubmit",
	"tool":       "PreToolUse",
	"done":       "PostToolUse",
	"fail":       "PostToolUseFailure",
	"permission": "PermissionRequest",
	"stop":       "Stop",
	"end":        "SessionEnd",
}

var agentEventCmd = &cobra.Command{
	Use:   "event <start|prompt|tool|done|fail|permission|stop|end>",
	Short: "Report one event from an agent that is not Claude Code, so it sits on the board",
	Long: `The board tracks Claude Code through its hooks. Another agent CLI joins the
same board by calling this on its own events — from whatever hook, notify or
plugin mechanism it has:

  corgi agent event start --agent codex --session $ID
  corgi agent event prompt --agent codex --session $ID
  corgi agent event tool --agent codex --session $ID --tool shell --input '{"command":"go test ./..."}'
  corgi agent event permission --agent codex --session $ID --tool shell --input '{"command":"rm -rf build"}'
  corgi agent event stop --agent codex --session $ID
  corgi agent event end --agent codex --session $ID

Without --session the id is the calling process — fine for an agent that runs
one conversation per process. With - the event is read from stdin as JSON
({session_id, cwd, tool_name, tool_input}), the shape Claude Code's hooks use.
The session shows on every surface with the agent's name; a permission with a
risk word colours Allow like any other. Never prints, never fails: the agent
is unaffected whatever corgi's state is.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name, ok := eventNames[strings.ToLower(strings.TrimSpace(args[0]))]
		if !ok {
			exitWithError("agent_event", fmt.Errorf("the event is start, prompt, tool, done, fail, permission, stop or end, not %q", args[0]), 2)
		}
		f := cmd.Flags()
		agent, _ := f.GetString("agent")
		session, _ := f.GetString("session")
		cwd, _ := f.GetString("cwd")
		tool, _ := f.GetString("tool")
		input, _ := f.GetString("input")
		message, _ := f.GetString("message")
		fromStdin, _ := f.GetBool("stdin")
		if fromStdin {
			var in struct {
				SessionID string          `json:"session_id"`
				Cwd       string          `json:"cwd"`
				Tool      string          `json:"tool_name"`
				ToolInput json.RawMessage `json:"tool_input"`
				Message   string          `json:"message"`
			}
			if data, err := io.ReadAll(io.LimitReader(os.Stdin, 64<<10)); err == nil && json.Unmarshal(data, &in) == nil {
				session = firstNonEmpty(session, in.SessionID)
				cwd = firstNonEmpty(cwd, in.Cwd)
				tool = firstNonEmpty(tool, in.Tool)
				if input == "" && len(in.ToolInput) > 0 {
					input = string(in.ToolInput)
				}
				message = firstNonEmpty(message, in.Message)
			}
		}
		ev, ok := agentEvent(name, agent, session, cwd, tool, input, message, os.Getenv, os.Getppid())
		if !ok {
			return
		}
		deliverEvent(ev)
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"event": ev.Name, "session": ev.SessionID, "agent": ev.Agent})
		}
	},
}

func agentEvent(name, agent, session, cwd, tool, input, message string, getenv func(string) string, parent int) (sessions.Event, bool) {
	chain := proc.Ancestors(parent)
	if proc.HasCorgi(chain) {
		return sessions.Event{}, false
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	ev := sessions.Event{
		Name: name, Cwd: cwd, Agent: strings.ToLower(strings.TrimSpace(agent)),
		Tool: tool, Message: truncateLine(message, 160),
		ConfigDir: getenv("CLAUDE_CONFIG_DIR"), Window: getenv("CORGI_VSCODE_WINDOW"),
		Ticket: getenv("CORGI_TICKET"), TicketKey: getenv("CORGI_TICKET_KEY"), Bot: getenv("CORGI_BOT"),
		TermProgram: getenv("TERM_PROGRAM"),
		TermSession: firstNonEmpty(getenv("ITERM_SESSION_ID"), getenv("TERM_SESSION_ID")),
		TmuxPane:    getenv("TMUX_PANE"),
		At:          time.Now().UTC(),
	}
	if tool != "" {
		raw := json.RawMessage(input)
		if !json.Valid(raw) {
			raw, _ = json.Marshal(map[string]string{"command": input})
		}
		ev.Subject = subjectOf(tool, raw)
		ev.Risk = riskOf(tool, raw)
	}
	switch name {
	case "SessionStart", "UserPromptSubmit", "Stop":
		ev.Branch = sessions.Branch(cwd)
	}
	ev.Ancestors = proc.PIDs(chain)
	ev.Names = proc.Names(chain)
	if owner, ok := proc.Owner(chain); ok {
		ev.ClaudePID = owner.PID
		ev.TTY = owner.TTY
		if session == "" {
			session = fmt.Sprintf("%s-%d", firstNonEmpty(ev.Agent, "agent"), owner.PID)
		}
	}
	if session == "" {
		session = fmt.Sprintf("%s-%d", firstNonEmpty(ev.Agent, "agent"), parent)
	}
	ev.SessionID = session
	return ev, true
}

func init() {
	f := agentEventCmd.Flags()
	f.String("agent", "", "The agent's name — codex, gemini, opencode — shown on the board")
	f.String("session", "", "The agent's own session id; default: one per calling process")
	f.String("cwd", "", "The directory the session works in; default: the current one")
	f.String("tool", "", "The tool, for tool, done, fail and permission")
	f.String("input", "", "The tool's input as JSON ({\"command\":…}, {\"file_path\":…}), or a bare command line")
	f.String("message", "", "A line to carry, for fail")
	f.Bool("stdin", false, "Read {session_id, cwd, tool_name, tool_input} from stdin, the shape Claude Code's hooks use")
	agentCmd.AddCommand(agentEventCmd)
}
