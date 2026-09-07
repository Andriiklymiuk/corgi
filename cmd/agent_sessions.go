package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/sessions"

	"github.com/spf13/cobra"
)

// The board, from the command line. Everything here reads sessions.json or
// drops a command into the daemon's spool: a Stream Deck plugin does exactly
// the same, which is why these exist — a plugin that shells out to `corgi
// agent focus` needs no private protocol.

var agentSessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "Show every tracked Claude Code session as the board of keys",
	Long: `Prints the session board the daemon keeps: one line per key, with the
session's label, status, what it is doing, its account and where its terminal
lives. --json prints the same board as sessions.json, plus the file's path, so
a plugin can find and watch it. --watch redraws on every change.

Needs ` + "`corgi agent track enable`" + ` once, and the daemon running.`,
	Run: runAgentSessions,
}

var agentFocusCmd = &cobra.Command{
	Use:   "focus <session>",
	Short: "Bring a session's window to the front and reveal its terminal tab",
	Long: `Asks the daemon to focus a session: by id (or a prefix of it), by label, or
by key number as ` + "`corgi agent sessions`" + ` prints it. On macOS the editor window
holding the session's folder comes forward; with the corgi VS Code extension
installed the exact terminal tab or the Claude Code panel is revealed too.`,
	Args: cobra.ExactArgs(1),
	Run: func(_ *cobra.Command, args []string) {
		sendBoardCommand(command.Command{Action: command.ActionFocus, SessionID: args[0], Source: "cli"},
			fmt.Sprintf("asked the daemon to focus %s", args[0]))
	},
}

var agentPinCmd = &cobra.Command{
	Use:   "pin <key>",
	Short: "Reserve a key for the session on it (--off to release)",
	Long: `A pinned key keeps its session: it never moves when others end, and if the
session exits the key stays reserved and dimmed until unpinned.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		off, _ := cmd.Flags().GetBool("off")
		index, err := keyIndex(args[0])
		if err != nil {
			exitWithError("agent_pin", err, 2)
		}
		what := "pinned"
		if off {
			what = "unpinned"
		}
		sendBoardCommand(command.Command{Action: command.ActionPin, Index: index, Pinned: !off, Source: "cli"},
			fmt.Sprintf("key %s %s", args[0], what))
	},
}

var agentPageCmd = &cobra.Command{
	Use:   "page [next|prev]",
	Short: "Turn the board's overflow page when more sessions run than keys",
	Args:  cobra.MaximumNArgs(1),
	Run: func(_ *cobra.Command, args []string) {
		direction := 1
		if len(args) > 0 {
			switch strings.ToLower(args[0]) {
			case "next", "+", "+1":
			case "prev", "previous", "back", "-", "-1":
				direction = -1
			default:
				exitWithError("agent_page", fmt.Errorf("page takes next or prev, not %q", args[0]), 2)
			}
		}
		sendBoardCommand(command.Command{Action: command.ActionPage, Direction: direction, Source: "cli"}, "page turned")
	},
}

var agentRescanCmd = &cobra.Command{
	Use:   "rescan",
	Short: "Look for running Claude sessions no hook has reported",
	Run: func(_ *cobra.Command, _ []string) {
		sendBoardCommand(command.Command{Action: command.ActionRescan, Source: "cli"}, "rescan requested — `corgi agent sessions` in a moment")
	},
}

var agentWindowsCmd = &cobra.Command{
	Use:   "windows",
	Short: "List the editor windows the corgi VS Code extension has connected",
	Run:   runAgentWindows,
}

// keyIndex reads a 1-based key number as printed on the board.
func keyIndex(arg string) (int, error) {
	n, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(arg), "#"))
	if err != nil || n < 1 {
		return 0, fmt.Errorf("a key is a number from 1 up, as `corgi agent sessions` prints it, not %q", arg)
	}
	return n - 1, nil
}

// sendBoardCommand drops one command in the spool and nudges the daemon,
// which must be running: without it nothing would read the request.
func sendBoardCommand(c command.Command, done string) {
	dir := mustAgentDir()
	info, err := daemon.ReadInfo(dir)
	if err != nil {
		exitWithError("agent_daemon", err, 1)
	}
	if info == nil {
		exitWithError("agent_daemon", fmt.Errorf("corgi agent is not running — `corgi agent serve`, or `corgi agent install` to start it at login"), 1)
	}
	if _, err := command.Write(dir, c); err != nil {
		exitWithError("agent_command", err, 1)
	}
	daemon.Nudge(info)
	if utils.JSONOutput {
		utils.PrintJSON(map[string]any{"ok": true, "action": c.Action})
		return
	}
	utils.Info(done)
}

// boardReport is `corgi agent sessions --json`: the published board plus
// where it lives and whether anyone is maintaining it.
type boardReport struct {
	Path    string `json:"path"`
	Running bool   `json:"daemonRunning"`
	sessions.State
}

func readBoard(dir string) (boardReport, error) {
	rep := boardReport{Path: daemon.SessionsPath(dir)}
	if info, err := daemon.ReadInfo(dir); err == nil && info != nil {
		rep.Running = true
	}
	data, err := os.ReadFile(rep.Path)
	if os.IsNotExist(err) {
		rep.Size = sessions.DefaultSize
		return rep, nil
	}
	if err != nil {
		return rep, err
	}
	if err := json.Unmarshal(data, &rep.State); err != nil {
		return rep, fmt.Errorf("%s: %v", rep.Path, err)
	}
	return rep, nil
}

func runAgentSessions(cmd *cobra.Command, _ []string) {
	dir := mustAgentDir()
	watch, _ := cmd.Flags().GetBool("watch")
	rep, err := readBoard(dir)
	if err != nil {
		exitWithError("agent_sessions", err, 1)
	}
	if utils.JSONOutput {
		utils.PrintJSON(rep)
		return
	}
	printBoard(rep, time.Now())
	if !watch {
		return
	}
	last := rep.UpdatedAt
	for {
		time.Sleep(500 * time.Millisecond)
		next, err := readBoard(dir)
		if err != nil || next.UpdatedAt.Equal(last) {
			continue
		}
		last = next.UpdatedAt
		fmt.Print("\033[H\033[2J")
		printBoard(next, time.Now())
	}
}

func printBoard(rep boardReport, now time.Time) {
	if !rep.Running {
		fmt.Println("corgi agent is not running — the board below is the last one it published.")
		fmt.Println("`corgi agent serve` starts it now, `corgi agent install` at login.")
	}
	if len(rep.Sessions) == 0 {
		fmt.Printf("no Claude sessions tracked (%d keys)\n", rep.Size)
		fmt.Println("`corgi agent track enable` installs the hooks; sessions appear from their next event")
		return
	}
	extra := ""
	if rep.Overflow > 0 {
		extra = fmt.Sprintf(", %d more than fit", rep.Overflow)
	}
	fmt.Printf("%d session(s) on %d keys%s\n", len(rep.Sessions), rep.Size, extra)
	for _, sl := range rep.Slots {
		fmt.Println(formatSlot(sl))
	}
}

func formatSlot(sl sessions.Slot) string {
	key := fmt.Sprintf("%2d", sl.Index+1)
	switch {
	case sl.Pager:
		return fmt.Sprintf("%s  +%d  (press to page)", key, sl.Overflow)
	case sl.Empty:
		return key + "  ·"
	}
	pin := " "
	if sl.Pinned {
		pin = "📌"
	}
	line := fmt.Sprintf("%s %s %s %-18s %-10s", key, pin, statusGlyph(sl.Status), clipTitle(sl.Label, 18), statusWord(sl.Status))
	if sl.Detail != "" {
		line += " " + clipTitle(sl.Detail, 24)
	}
	line += "  " + sl.Profile + " · " + string(sl.Host)
	if sl.ElapsedS > 0 {
		line += " · " + shortDuration(time.Duration(sl.ElapsedS)*time.Second)
	}
	if sl.FocusError != "" {
		line += "  ⚠ " + sl.FocusError
	}
	return line
}

func statusGlyph(s sessions.Status) string {
	switch s {
	case sessions.StatusWorking:
		return "●"
	case sessions.StatusNeedsInput:
		return "▲"
	case sessions.StatusDone:
		return "✓"
	case sessions.StatusGone:
		return "✕"
	case sessions.StatusStale:
		return "◌"
	}
	return "?"
}

// statusWord is the one word a key shows under the label.
func statusWord(s sessions.Status) string {
	switch s {
	case sessions.StatusWorking:
		return "WORKING"
	case sessions.StatusNeedsInput:
		return "NEEDS YOU"
	case sessions.StatusDone:
		return "DONE"
	case sessions.StatusStale:
		return "IDLE"
	case sessions.StatusGone:
		return "CLOSED"
	}
	return "?"
}

func shortDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

func runAgentWindows(_ *cobra.Command, _ []string) {
	dir := mustAgentDir()
	rep, err := readBoard(dir)
	if err != nil {
		exitWithError("agent_windows", err, 1)
	}
	if utils.JSONOutput {
		utils.PrintJSON(rep.Windows)
		return
	}
	if len(rep.Windows) == 0 {
		fmt.Println("no editor windows connected — install the corgi VS Code extension, or reopen a window")
		return
	}
	for _, w := range rep.Windows {
		fmt.Printf("%s  %s  ext-host %d\n", w.ID, w.App, w.ExtHostPID)
		for _, f := range w.Folders {
			fmt.Printf("    %s\n", f)
		}
		for _, t := range w.Terminals {
			fmt.Printf("    terminal %-16s shell %d\n", t.Name, t.ShellPID)
		}
	}
}

func init() {
	agentSessionsCmd.Flags().Bool("watch", false, "Redraw the board whenever it changes")
	agentPinCmd.Flags().Bool("off", false, "Release the key instead")
	agentCmd.AddCommand(agentSessionsCmd, agentFocusCmd, agentPinCmd, agentPageCmd, agentRescanCmd, agentWindowsCmd)
}
