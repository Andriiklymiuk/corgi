package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/sessions"
)

type Attempt struct {
	Ref     string `json:"ref"`
	N       string `json:"n"`
	Session string `json:"session"`
	Label   string `json:"label"`
	Status  string `json:"status"`
	Model   string `json:"model,omitempty"`
	Branch  string `json:"branch,omitempty"`
	Changes string `json:"changes,omitempty"`
	Tests   string `json:"tests,omitempty"`
	Gate    string `json:"gate,omitempty"`
	Spend   string `json:"spend,omitempty"`
	PR      string `json:"pr,omitempty"`
	Summary string `json:"summary,omitempty"`
	Picked  bool   `json:"picked,omitempty"`
}

type AttemptGroup struct {
	Ref      string    `json:"ref"`
	Attempts []Attempt `json:"attempts"`
}

func attemptGroups(list []sessions.Session, ref string) []AttemptGroup {
	byRef := map[string][]Attempt{}
	for _, s := range list {
		r, n, ok := strings.Cut(s.Attempt, "/")
		if !ok || r == "" {
			continue
		}
		if ref != "" && !strings.EqualFold(r, ref) {
			continue
		}
		if s.Status == sessions.StatusGone && !strings.HasPrefix(s.Note, "picked") {
			continue
		}
		a := Attempt{Ref: r, N: n, Session: s.ID, Label: firstNonEmpty(s.Display, s.Label), Status: string(s.Status), Branch: s.Branch,
			Changes: sessions.ChangesLine(s.Changes), Tests: sessions.TestsLine(s.Tests), Spend: sessions.SpendLine(s.Spend), PR: s.PR, Summary: s.Summary,
			Picked: strings.HasPrefix(s.Note, "picked")}
		if s.Gate != nil {
			if s.Gate.OK {
				a.Gate = "done ✓"
			} else {
				a.Gate = "not done: " + s.Gate.Cmd
			}
		}
		byRef[r] = append(byRef[r], a)
	}
	var out []AttemptGroup
	for r, list := range byRef {
		sort.Slice(list, func(i, j int) bool { return list[i].N < list[j].N })
		out = append(out, AttemptGroup{Ref: r, Attempts: list})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref < out[j].Ref })
	return out
}

var agentAttemptsCmd = &cobra.Command{
	Use:   "attempts [ref]",
	Short: "Compare the sessions a fan-out opened on a ticket, and pick one",
	Long: `corgi agent watch work ABC-123 --attempts 3 opens three sessions on the
ticket, a worktree each. This puts them side by side - status, what each
built, whether its tests and the workspace's done-when passed, what it cost,
the pull request it opened - and picks one:

  corgi agent attempts                 every fan-out on the board
  corgi agent attempts ABC-123
  corgi agent attempts pick ABC-123 2  keep attempt 2: the others are interrupted and marked; their worktrees stay until you remove them`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		rep, err := readBoard(dir)
		if err != nil {
			exitWithError("agent_attempts", err, 1)
		}
		ref := ""
		if len(args) == 1 {
			ref = args[0]
		}
		groups := attemptGroups(rep.State.Sessions, ref)
		if utils.JSONOutput {
			if groups == nil {
				groups = []AttemptGroup{}
			}
			utils.PrintJSON(groups)
			return
		}
		if len(groups) == 0 {
			fmt.Println("No fan-out on the board. corgi agent watch work <ref> --attempts 3 opens one.")
			return
		}
		for _, g := range groups {
			fmt.Println(g.Ref)
			for _, a := range g.Attempts {
				mark := " "
				if a.Picked {
					mark = "★"
				}
				parts := []string{a.Status}
				for _, p := range []string{a.Model, a.Changes, a.Tests, a.Gate, a.Spend, a.PR} {
					if p != "" {
						parts = append(parts, p)
					}
				}
				fmt.Printf("  %s %s · %s\n", mark, a.N, strings.Join(parts, " · "))
				if a.Summary != "" {
					fmt.Printf("      %s\n", a.Summary)
				}
			}
		}
	},
}

var agentAttemptsPickCmd = &cobra.Command{
	Use:   "pick <ref> <n>",
	Short: "Keep one attempt: the others are interrupted and marked not picked",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		rep, err := readBoard(dir)
		if err != nil {
			exitWithError("agent_attempts", err, 1)
		}
		groups := attemptGroups(rep.State.Sessions, args[0])
		if len(groups) == 0 {
			exitWithError("agent_attempts", fmt.Errorf("no fan-out on %s", args[0]), 2)
		}
		var kept *Attempt
		for i := range groups[0].Attempts {
			if groups[0].Attempts[i].N == strings.TrimSpace(args[1]) {
				kept = &groups[0].Attempts[i]
			}
		}
		if kept == nil {
			exitWithError("agent_attempts", fmt.Errorf("no attempt %s on %s", args[1], args[0]), 2)
		}
		for _, cmd := range pickCommands(groups[0], kept.N) {
			sendBoardCommand(cmd, "")
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"picked": kept.Session, "ref": groups[0].Ref, "n": kept.N})
			return
		}
		fmt.Printf("kept attempt %s of %s (%s); the others are interrupted and marked\n", kept.N, groups[0].Ref, kept.Label)
	},
}

func pickCommands(g AttemptGroup, n string) []command.Command {
	var out []command.Command
	for _, a := range g.Attempts {
		if a.N == n {
			out = append(out, command.Command{Action: command.ActionNote, SessionID: a.Session, Note: "picked of " + g.Ref, Source: "cli"})
			continue
		}
		if a.Status == string(sessions.StatusWorking) {
			out = append(out, command.Command{Action: command.ActionInterrupt, SessionID: a.Session, Source: "cli"})
		}
		out = append(out, command.Command{Action: command.ActionNote, SessionID: a.Session, Note: "not picked - attempt " + n + " was", Source: "cli"})
	}
	return out
}

func init() {
	agentAttemptsCmd.AddCommand(agentAttemptsPickCmd)
	agentCmd.AddCommand(agentAttemptsCmd)
}

func launchAttemptsHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rep, err := readBoard(dir)
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	switch r.Method {
	case http.MethodGet:
		groups := attemptGroups(rep.State.Sessions, strings.TrimSpace(r.URL.Query().Get("ref")))
		if groups == nil {
			groups = []AttemptGroup{}
		}
		writeLaunchJSON(w, map[string]any{"groups": groups})
	case http.MethodPost:
		var req struct {
			Ref string `json:"ref"`
			N   string `json:"n"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
			writeLaunchError(w, http.StatusBadRequest, "could not read the request")
			return
		}
		groups := attemptGroups(rep.State.Sessions, strings.TrimSpace(req.Ref))
		if len(groups) == 0 {
			writeLaunchError(w, http.StatusNotFound, "no fan-out on "+req.Ref)
			return
		}
		found := false
		for _, a := range groups[0].Attempts {
			if a.N == strings.TrimSpace(req.N) {
				found = true
			}
		}
		if !found {
			writeLaunchError(w, http.StatusNotFound, "no attempt "+req.N+" on "+groups[0].Ref)
			return
		}
		cmds := pickCommands(groups[0], strings.TrimSpace(req.N))
		for i := range cmds {
			cmds[i].Source = "phone"
		}
		launchBoardCommands(w, cmds)
	default:
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET the fan-outs, POST {ref, n} to keep one")
	}
}
