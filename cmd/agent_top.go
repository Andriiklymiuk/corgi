package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// The board in the terminal, live, like top: every session with its
// standing, what it is doing, its spend; the ones that need you first.
// j/k move, a allows, d denies, i interrupts, o focuses, q quits. For the
// laptop that has no menu bar — a Linux box, a server over ssh — and for
// anyone who lives in a terminal anyway. Reads sessions.json once a second;
// every key goes through the daemon's spool like every other surface.

// topRow is one session as the screen shows it.
type topRow struct {
	S        sessions.Session
	Standing string
	Doing    string
	Spend    string
}

// topRows orders sessions for the screen: needs you, then working, then
// the rest, each newest first.
func topRows(st sessions.State) []topRow {
	rank := func(s sessions.Session) int {
		switch s.Status {
		case sessions.StatusNeedsInput:
			return 0
		case sessions.StatusWorking:
			return 1
		case sessions.StatusLimited:
			return 2
		case sessions.StatusGone:
			return 9
		}
		return 3
	}
	list := append([]sessions.Session(nil), st.Sessions...)
	sort.SliceStable(list, func(i, j int) bool {
		if rank(list[i]) != rank(list[j]) {
			return rank(list[i]) < rank(list[j])
		}
		return list[i].LastActivity.After(list[j].LastActivity)
	})
	rows := make([]topRow, 0, len(list))
	for _, s := range list {
		if s.Status == sessions.StatusGone {
			continue
		}
		word := string(s.Status)
		if s.Standing != nil && s.Standing.Word != "" {
			word = s.Standing.Word
		}
		doing := s.Detail
		if s.Pending != nil {
			doing = "allow " + strings.TrimSpace(s.Pending.Tool+" "+s.Pending.Subject) + "?"
		} else if doing == "" && s.Summary != "" {
			doing = s.Summary
		}
		rows = append(rows, topRow{S: s, Standing: word, Doing: doing, Spend: sessions.SpendLine(s.Spend)})
	}
	return rows
}

// topScreen draws the board for a terminal width and height; cursor is the
// selected row.
func topScreen(st sessions.State, rows []topRow, cursor, width, height int, at time.Time) string {
	if width < 40 {
		width = 40
	}
	var b strings.Builder
	head := fmt.Sprintf(" corgi · %d sessions · %d need you · %d working · %s", len(rows), st.NeedsInput, st.Working, at.Format("15:04:05"))
	b.WriteString(clipLine(head, width) + "\r\n")
	b.WriteString(clipLine(fmt.Sprintf(" %-3s %-22s %-18s %-9s %s", "", "SESSION", "STANDING", "SPEND", "DOING"), width) + "\r\n")
	lines := height - 4
	if lines < 1 {
		lines = 1
	}
	start := 0
	if cursor >= lines {
		start = cursor - lines + 1
	}
	for i := start; i < len(rows) && i < start+lines; i++ {
		r := rows[i]
		mark := "  "
		if i == cursor {
			mark = "▸ "
		}
		name := r.S.Display
		if name == "" {
			name = r.S.Label
		}
		line := fmt.Sprintf(" %-3s %-22s %-18s %-9s %s", mark, clipLine(name, 22), clipLine(r.Standing, 18), clipLine(r.Spend, 9), r.Doing)
		b.WriteString(clipLine(line, width) + "\r\n")
	}
	for i := len(rows); i < lines; i++ {
		b.WriteString("\r\n")
	}
	b.WriteString(clipLine(" j/k move · a allow · d deny · i interrupt · o open · q quit", width))
	return b.String()
}

func clipLine(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

var agentTopCmd = &cobra.Command{
	Use:   "top",
	Short: "The board in the terminal, live: every session, its standing, what it is doing",
	Long: `Like top, for your agents: the sessions with their standing, what each is
doing and what it has spent, the ones that need you first, refreshed every
second. Keys: j/k move, a allow, d deny, i interrupt, o open, q quit — each
goes through the daemon like a press on the phone would.

  corgi agent top`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			exitWithError("agent_top", fmt.Errorf("top needs a terminal; corgi agent sessions --json prints the board"), 2)
		}
		dir := mustAgentDir()
		old, err := term.MakeRaw(int(os.Stdin.Fd()))
		if err != nil {
			exitWithError("agent_top", err, 1)
		}
		defer term.Restore(int(os.Stdin.Fd()), old)
		fmt.Print("\x1b[?1049h\x1b[?25l")
		defer fmt.Print("\x1b[?25h\x1b[?1049l")

		keys := make(chan byte, 8)
		go func() {
			buf := make([]byte, 1)
			for {
				n, err := os.Stdin.Read(buf)
				if err != nil || n == 0 {
					close(keys)
					return
				}
				keys <- buf[0]
			}
		}()
		cursor := 0
		note := ""
		draw := func() {
			rep, _ := readBoard(dir)
			rows := topRows(rep.State)
			if cursor >= len(rows) {
				cursor = max(0, len(rows)-1)
			}
			w, h, err := term.GetSize(int(os.Stdout.Fd()))
			if err != nil {
				w, h = 100, 30
			}
			screen := topScreen(rep.State, rows, cursor, w, h, time.Now())
			if note != "" {
				screen += "\r\n" + clipLine(" "+note, w)
			}
			fmt.Print("\x1b[H\x1b[2J" + screen)
		}
		act := func(rows []topRow, c command.Command, said string) {
			if cursor >= len(rows) {
				return
			}
			c.SessionID = rows[cursor].S.ID
			c.Source = "cli"
			if _, err := command.Write(dir, c); err != nil {
				note = err.Error()
				return
			}
			nudgeDaemon(dir)
			note = said + " " + firstNonEmpty(rows[cursor].S.Display, rows[cursor].S.Label)
		}
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		draw()
		for {
			select {
			case <-ticker.C:
				draw()
			case k, ok := <-keys:
				if !ok {
					return
				}
				rep, _ := readBoard(dir)
				rows := topRows(rep.State)
				switch k {
				case 'q', 3:
					return
				case 'j':
					if cursor < len(rows)-1 {
						cursor++
					}
				case 'k':
					if cursor > 0 {
						cursor--
					}
				case 'a':
					act(rows, command.Command{Action: command.ActionAnswer, Answer: "allow"}, "allowed")
				case 'd':
					act(rows, command.Command{Action: command.ActionAnswer, Answer: "deny"}, "denied")
				case 'i':
					act(rows, command.Command{Action: command.ActionInterrupt}, "interrupting")
				case 'o':
					act(rows, command.Command{Action: command.ActionFocus}, "opening")
				}
				draw()
			}
		}
	},
}

func init() {
	agentCmd.AddCommand(agentTopCmd)
}
