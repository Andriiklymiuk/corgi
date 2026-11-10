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

type TimelineItem struct {
	At   time.Time `json:"at"`
	Kind string    `json:"kind"`
	Text string    `json:"text"`
	N    int       `json:"n,omitempty"`
	OK   *bool     `json:"ok,omitempty"`
	URL  string    `json:"url,omitempty"`
}

func timelineFor(dir string, s sessions.Session, limit int) []TimelineItem {
	var items []TimelineItem
	if !s.StartedAt.IsZero() {
		items = append(items, TimelineItem{At: s.StartedAt, Kind: "start", Text: "session started in " + firstNonEmpty(s.Label, s.Cwd)})
	}
	items = append(items, transcriptTimeline(s)...)
	items = append(items, checksTimeline(s)...)
	if s.PR != "" {
		items = append(items, pullTimeline(dir, s)...)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].At.Before(items[j].At) })
	if limit > 0 && len(items) > limit {
		items = items[len(items)-limit:]
	}
	return items
}

type toolBurst struct {
	items []TimelineItem
	burst *TimelineItem
}

func (t *toolBurst) flush() {
	if t.burst != nil {
		t.items = append(t.items, *t.burst)
		t.burst = nil
	}
}

func (t *toolBurst) said(e transcript.Entry, kind string) {
	t.flush()
	if text := strings.TrimSpace(e.Text); text != "" {
		t.items = append(t.items, TimelineItem{At: e.At, Kind: kind, Text: clipTitle(strings.ReplaceAll(text, "\n", " "), 160)})
	}
}

func (t *toolBurst) tool(e transcript.Entry) {
	subject := strings.TrimSpace(e.Tool + " " + e.Subject)
	if t.burst == nil {
		t.burst = &TimelineItem{At: e.At, Kind: "tools", Text: subject, N: 0}
	}
	t.burst.N++
	if t.burst.N > 1 {
		t.burst.Text = clipTitle(t.burst.Text, 120)
		if !strings.Contains(t.burst.Text, "…") && len(t.burst.Text) < 100 {
			t.burst.Text += " · " + subject
		}
	}
}

func transcriptTimeline(s sessions.Session) []TimelineItem {
	path := transcriptPathFor(s)
	if path == "" || !transcript.Exists(path) {
		return nil
	}
	entries, _, err := transcript.Read(path, 0, transcript.MaxEntries)
	if err != nil {
		return nil
	}
	var t toolBurst
	for _, e := range entries {
		switch e.Kind {
		case "user":
			t.said(e, "prompt")
		case "assistant":
			t.said(e, "said")
		case "tool":
			t.tool(e)
		}
	}
	t.flush()
	return t.items
}

func checksTimeline(s sessions.Session) []TimelineItem {
	var items []TimelineItem
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
	return items
}

func pullStateItem(dir string, s sessions.Session) TimelineItem {
	st, ok := watch.LoadPullLog(dir).Get(s.PR)
	if !ok {
		return TimelineItem{At: s.LastActivity, Kind: "pr", Text: "pull request opened", URL: s.PR}
	}
	switch st.State {
	case "merged":
		return TimelineItem{At: st.At, Kind: "merged", Text: "merged", URL: s.PR}
	case "closed":
		return TimelineItem{At: st.At, Kind: "closed", Text: "closed without merging", URL: s.PR}
	}
	text := "pull request " + st.State
	if line := st.Line(); line != "" {
		text += " · " + line
	}
	return TimelineItem{At: st.At, Kind: "pr", Text: text, URL: s.PR}
}

var pullEventWords = map[watch.Kind][2]string{
	watch.KindPRReview:        {"review", "review"},
	watch.KindPRComment:       {"comment", "comment"},
	watch.KindCIFailed:        {"ci", "build went red"},
	watch.KindReviewRequested: {"review", "review asked"},
}

func pullEventItem(e watch.Event) (TimelineItem, bool) {
	words, ok := pullEventWords[e.Kind]
	if !ok {
		return TimelineItem{}, false
	}
	text := words[1]
	if e.Author != "" {
		text += " by " + e.Author
	}
	if b := strings.TrimSpace(e.Body); b != "" {
		text += ": " + clipTitle(strings.ReplaceAll(b, "\n", " "), 120)
	}
	return TimelineItem{At: e.At, Kind: words[0], Text: text, URL: e.URL}, true
}

func pullTimeline(dir string, s sessions.Session) []TimelineItem {
	items := []TimelineItem{pullStateItem(dir, s)}
	ref := watch.PullRef(s.PR)
	if ref == "" {
		return items
	}
	hands := watch.LoadHands(dir)
	for _, e := range watch.RecentEvents(dir, 400) {
		if !strings.EqualFold(e.Ref, ref) {
			continue
		}
		item, ok := pullEventItem(e)
		if !ok {
			continue
		}
		items = append(items, item)
		if h, ok := hands.Get(e.Key); ok && h.To == s.ID {
			items = append(items, TimelineItem{At: h.At, Kind: "handed", Text: "handed to the session" + byWord(h.By)})
		}
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
	if !streamAllowedFor(session.Label) {
		writeLaunchError(w, http.StatusForbidden, fmt.Sprintf("reading %s's story from a phone is off on the laptop: corgi agent stream enable --workspace %s", session.Label, session.Label))
		return
	}
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("max"))
	if limit <= 0 || limit > 400 {
		limit = 200
	}
	items := timelineFor(dir, session, limit)
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
