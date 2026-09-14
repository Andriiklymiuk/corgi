package cmd

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/transcript"
	"andriiklymiuk/corgi/utils/agent/watch"
	"github.com/spf13/cobra"
)

// A session's timeline: what happened, in order, on one scroll — the
// prompts, the bursts of tool calls between them, the test runs and the
// gate, the pull request and every review, check and hand-over on it,
// the merge. Joined here from the transcript, the board and the watch's
// files; the phone draws it, the CLI prints it. "What happened while I
// was away" in one screen.

// TimelineItem is one moment.
type TimelineItem struct {
	At   time.Time `json:"at"`
	Kind string    `json:"kind"` // start, prompt, tools, said, tests, gate, pr, review, comment, ci, handed, merged, closed, headless
	Text string    `json:"text"`
	// N is how many tool calls a tools burst holds.
	N int `json:"n,omitempty"`
	// OK is the outcome of a tests or gate item.
	OK *bool `json:"ok,omitempty"`
	// URL is where a pull request item goes.
	URL string `json:"url,omitempty"`
}

// timelineFor joins a session's story from every book the laptop keeps.
func timelineFor(dir string, s sessions.Session, max int) []TimelineItem {
	var items []TimelineItem
	if !s.StartedAt.IsZero() {
		items = append(items, TimelineItem{At: s.StartedAt, Kind: "start", Text: "session started in " + firstNonEmpty(s.Label, s.Cwd)})
	}
	// The transcript: prompts, what was said, and tool calls folded into
	// bursts between them.
	if path := transcriptPathFor(s); path != "" && transcript.Exists(path) {
		entries, _, err := transcript.Read(path, 0, transcript.MaxEntries)
		if err == nil {
			var burst *TimelineItem
			flush := func() {
				if burst != nil {
					items = append(items, *burst)
					burst = nil
				}
			}
			for _, e := range entries {
				switch e.Kind {
				case "user":
					flush()
					if t := strings.TrimSpace(e.Text); t != "" {
						items = append(items, TimelineItem{At: e.At, Kind: "prompt", Text: clipTitle(strings.ReplaceAll(t, "\n", " "), 160)})
					}
				case "assistant":
					flush()
					if t := strings.TrimSpace(e.Text); t != "" {
						items = append(items, TimelineItem{At: e.At, Kind: "said", Text: clipTitle(strings.ReplaceAll(t, "\n", " "), 160)})
					}
				case "tool":
					subject := strings.TrimSpace(e.Tool + " " + e.Subject)
					if burst == nil {
						burst = &TimelineItem{At: e.At, Kind: "tools", Text: subject, N: 0}
					}
					burst.N++
					if burst.N > 1 {
						burst.Text = clipTitle(burst.Text, 120)
						if !strings.Contains(burst.Text, "…") && len(burst.Text) < 100 {
							burst.Text += " · " + subject
						}
					}
				}
			}
			flush()
		}
	}
	if s.Tests != nil && !s.Tests.At.IsZero() {
		ok := s.Tests.OK
		items = append(items, TimelineItem{At: s.Tests.At, Kind: "tests", Text: s.Tests.Cmd, OK: &ok})
	}
	if s.Gate != nil && !s.Gate.At.IsZero() {
		ok := s.Gate.OK
		items = append(items, TimelineItem{At: s.Gate.At, Kind: "gate", Text: firstNonEmpty(s.Gate.Cmd, "done-when"), OK: &ok})
	}
	if s.Headless != nil && !s.Headless.At.IsZero() {
		items = append(items, TimelineItem{At: s.Headless.At, Kind: "headless", Text: fmt.Sprintf("%d headless turn(s) after the terminal was gone", s.Headless.Turns)})
	}
	// The pull request: its standing, and everything the watch saw on it.
	if s.PR != "" {
		ref := watch.PullRef(s.PR)
		if st, ok := watch.LoadPullLog(dir).Get(s.PR); ok {
			kind, text := "pr", "pull request "+st.State
			if line := st.Line(); line != "" {
				text += " · " + line
			}
			if st.State == "merged" {
				kind, text = "merged", "merged"
			} else if st.State == "closed" {
				kind, text = "closed", "closed without merging"
			}
			items = append(items, TimelineItem{At: st.At, Kind: kind, Text: text, URL: s.PR})
		} else {
			items = append(items, TimelineItem{At: s.LastActivity, Kind: "pr", Text: "pull request opened", URL: s.PR})
		}
		hands := watch.LoadHands(dir)
		for _, e := range watch.RecentEvents(dir, 400) {
			if ref == "" || !strings.EqualFold(e.Ref, ref) {
				continue
			}
			kind, text := "comment", ""
			switch e.Kind {
			case watch.KindPRReview:
				kind, text = "review", "review"
			case watch.KindPRComment:
				kind, text = "comment", "comment"
			case watch.KindCIFailed:
				kind, text = "ci", "build went red"
			case watch.KindReviewRequested:
				kind, text = "review", "review asked"
			default:
				continue
			}
			if e.Author != "" {
				text += " by " + e.Author
			}
			if b := strings.TrimSpace(e.Body); b != "" {
				text += ": " + clipTitle(strings.ReplaceAll(b, "\n", " "), 120)
			}
			items = append(items, TimelineItem{At: e.At, Kind: kind, Text: text, URL: e.URL})
			if h, ok := hands.Get(e.Key); ok && h.To == s.ID {
				items = append(items, TimelineItem{At: h.At, Kind: "handed", Text: "handed to the session" + byWord(h.By)})
			}
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].At.Before(items[j].At) })
	if max > 0 && len(items) > max {
		items = items[len(items)-max:]
	}
	return items
}

func byWord(by string) string {
	switch by {
	case "", "daemon":
		return " by the laptop"
	default:
		return " from the " + by
	}
}

// launchTimelineHandler is GET /launch/timeline?session=<id>[&max=N].
func launchTimelineHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodGet {
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET /launch/timeline?session=<id>")
		return
	}
	session, code, msg := launchSessionFor(r.URL.Query().Get("session"))
	if code != 0 {
		writeLaunchError(w, code, msg)
		return
	}
	// The transcript is the code's story: the same gate as reading it.
	if !streamAllowedFor(session.Label) {
		writeLaunchError(w, http.StatusForbidden, fmt.Sprintf("reading %s's story from a phone is off on the laptop: corgi agent stream enable --workspace %s", session.Label, session.Label))
		return
	}
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	max, _ := strconv.Atoi(r.URL.Query().Get("max"))
	if max <= 0 || max > 400 {
		max = 200
	}
	items := timelineFor(dir, session, max)
	if items == nil {
		items = []TimelineItem{}
	}
	writeLaunchJSON(w, map[string]any{"session": session.ID, "items": items})
}

var agentTimelineCmd = &cobra.Command{
	Use:   "timeline <session>",
	Short: "What happened in a session, in order: prompts, tool bursts, tests, the pull request and its reviews",
	Long: `One scroll of a session's story, joined from the transcript, the board and
the watch: every prompt, the tool calls between them folded into bursts, the
test runs and the gate, the pull request with its checks, reviews, comments,
hand-overs and merge. The phone's Timeline draws the same.

  corgi agent timeline api·APP-412`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		session, _, msg := launchSessionFor(args[0])
		if msg != "" {
			exitWithError("agent_timeline", fmt.Errorf("%s", msg), 2)
		}
		items := timelineFor(mustAgentDir(), session, 400)
		if utils.JSONOutput {
			if items == nil {
				items = []TimelineItem{}
			}
			utils.PrintJSON(map[string]any{"session": session.ID, "items": items})
			return
		}
		for _, it := range items {
			mark := ""
			if it.OK != nil {
				if *it.OK {
					mark = " ✓"
				} else {
					mark = " ✗"
				}
			}
			n := ""
			if it.N > 1 {
				n = " ×" + strconv.Itoa(it.N)
			}
			fmt.Printf("%s  %-8s %s%s%s\n", it.At.Local().Format("15:04"), it.Kind, it.Text, n, mark)
		}
	},
}

func init() {
	agentCmd.AddCommand(agentTimelineCmd)
}
