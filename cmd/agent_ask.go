package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/watch"

	"github.com/spf13/cobra"
)

// Ask is the chief: one question about everything on the board — "what
// should I look at first?", "what is blocked?", "which session is on the
// cart bug?" — answered by a short Claude Code run on this machine that
// sees the sessions, the inbox, the kanban and the workspaces, and nothing
// else. It runs as corgi's child, so the tracking hook keeps it off the
// board; it costs one small call.

const askTimeout = 90 * time.Second

// askModel is what answers: the cheap one, the context is a few KB.
const askModel = "haiku"

const askSoul = `You are corgi's chief of staff for one developer's Claude Code sessions. You are given the live board as JSON: sessions (status, what each is doing, its ticket, branch, diff, last test run, cost, overlaps), the inbox (what waits on the person), the kanban (tickets by column) and the workspaces. Answer the question in at most five short lines, plain text only — no markdown, no asterisks, no headings; a list is one item per line. Name sessions and tickets exactly as given. When asked what to do first, pick one and say why in a clause. Never invent a session or ticket that is not in the data.`

// runClaudePrint is the seam: run claude -p with a prompt, return its text.
var runClaudePrint = func(ctx context.Context, model, system, prompt string) (string, error) {
	launch, err := resolveClaudeLaunch(mustCwd(), "", nil)
	if err != nil {
		return "", err
	}
	args := []string{"-p", "--output-format", "text", "--model", model, "--append-system-prompt", system}
	cmd := exec.CommandContext(ctx, launch.Bin, args...)
	cmd.Stdin = strings.NewReader(prompt)
	// Not inside the session that asked: a claude started from a Claude
	// Code tool call inherits CLAUDECODE and refuses to nest.
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "CLAUDECODE=") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	for k, v := range launch.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("claude: %s", firstLine(msg))
	}
	return strings.TrimSpace(out.String()), nil
}

// askContext is the board as the chief sees it: enough to answer, small
// enough to be cheap. Bodies of tickets are cut short; souls, prompts and
// transcripts never go in.
func askContext(dir string, now time.Time) map[string]any {
	ctx := map[string]any{}
	if rep, err := readBoard(dir); err == nil {
		list := make([]map[string]any, 0, len(rep.State.Sessions))
		for _, s := range rep.State.Sessions {
			if s.Status == sessions.StatusGone {
				continue
			}
			row := map[string]any{"name": firstNonEmpty(s.Display, s.Label), "status": string(s.Status), "workspace": s.Label}
			add := func(k, v string) {
				if v != "" {
					row[k] = v
				}
			}
			add("doing", s.Detail)
			add("title", s.Title)
			add("ticket", s.Ticket)
			add("branch", s.Branch)
			add("bot", s.Bot)
			add("changes", sessions.ChangesLine(s.Changes))
			add("tests", sessions.TestsLine(s.Tests))
			add("overlap", sessions.OverlapLine(s.Overlap))
			add("spend", sessions.SpendLine(s.Spend))
			if s.Pending != nil {
				row["waitingOn"] = strings.TrimSpace(s.Pending.Tool + " " + s.Pending.Subject)
			}
			if len(s.Drift) > 0 {
				row["drift"] = s.Drift[0]
			}
			if s.OverCap {
				row["overBudget"] = true
			}
			if !s.StatusSince.IsZero() {
				row["since"] = roughAge(now.Sub(s.StatusSince)) + " ago"
			}
			list = append(list, row)
		}
		ctx["sessions"] = list
	}
	state := watch.LoadState(dir)
	inbox := []map[string]any{}
	for _, e := range watch.RecentEvents(dir, 40) {
		if state.IsIgnored(e.Key) {
			continue
		}
		row := map[string]any{"ref": e.Ref, "kind": e.Kind, "title": e.Title, "workspace": e.Workspace, "age": roughAge(now.Sub(e.At)) + " ago"}
		if e.Body != "" {
			row["body"] = clipTitle(e.Body, 160)
		}
		if e.Author != "" {
			row["author"] = e.Author
		}
		inbox = append(inbox, row)
	}
	ctx["inbox"] = inbox
	cards := []map[string]any{}
	for _, c := range gatherKanban(dir, "", now) {
		row := map[string]any{"ref": c.Ref, "column": c.Column, "title": c.Title, "workspace": c.Workspace, "why": c.Why}
		if c.Blocked != "" {
			row["blocked"] = c.Blocked
		}
		if c.Session != nil {
			row["session"] = c.Session.Label
		}
		cards = append(cards, row)
	}
	ctx["kanban"] = cards
	if registry, _, err := agentRegistry(); err == nil {
		names := []string{}
		for _, ws := range registry.Sorted() {
			names = append(names, ws.ID)
		}
		ctx["workspaces"] = names
	}
	ctx["now"] = now.Format("Mon 15:04")
	return ctx
}

// askBoard answers one question about the board.
func askBoard(ctx context.Context, dir, question string) (string, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return "", fmt.Errorf("ask something")
	}
	if len(question) > 2000 {
		return "", fmt.Errorf("that is a long question; keep it under 2000 characters")
	}
	data, err := json.Marshal(askContext(dir, time.Now()))
	if err != nil {
		return "", err
	}
	if len(data) > 60_000 {
		data = data[:60_000]
	}
	prompt := "BOARD:\n" + string(data) + "\n\nQUESTION: " + question
	ctx, cancel := context.WithTimeout(ctx, askTimeout)
	defer cancel()
	answer, err := runClaudePrint(ctx, askModel, askSoul, prompt)
	if err != nil {
		return "", err
	}
	if answer == "" {
		return "", fmt.Errorf("claude answered nothing")
	}
	return answer, nil
}

var agentAskCmd = &cobra.Command{
	Use:   "ask <question>",
	Short: "Ask the chief about the board: what to look at first, what is blocked, who is on what",
	Long: `One question about everything on the board, answered in a few lines by a
short Claude run on this machine (haiku; it sees the sessions, the inbox,
the kanban and the workspace names, nothing else). The phone's Ask box and
Telegram's /ask do the same.

  corgi agent ask "what should I look at first?"
  corgi agent ask "which sessions touch the cart?"`,
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		answer, err := askBoard(context.Background(), mustAgentDir(), strings.Join(args, " "))
		if err != nil {
			exitWithError("agent_ask", err, 1)
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]string{"answer": answer})
			return
		}
		fmt.Println(answer)
	},
}

// launchAskHandler is the phone's Ask box: POST {question} → {answer}.
func launchAskHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "POST {question} to ask about the board")
		return
	}
	var req struct {
		Question string `json:"question"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil {
		writeLaunchError(w, http.StatusBadRequest, "could not read the question")
		return
	}
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	answer, err := askBoard(r.Context(), dir, req.Question)
	if err != nil {
		code := http.StatusBadGateway
		if strings.HasPrefix(err.Error(), "ask ") || strings.HasPrefix(err.Error(), "that is") {
			code = http.StatusBadRequest
		}
		writeLaunchError(w, code, err.Error())
		return
	}
	writeLaunchJSON(w, map[string]string{"answer": answer})
}

func init() {
	agentCmd.AddCommand(agentAskCmd)
}
