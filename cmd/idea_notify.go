package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/idea"
	"github.com/spf13/cobra"
)

// Comments, answers and page changes ring on the desktop and the notifyUrl
// (Telegram for most): each one as it lands, or one summary every few
// minutes, or not at all.

const (
	ideaNotifyOnEach  = "on"
	ideaNotifyBatch   = "batch"
	ideaNotifyOff     = "off"
	ideaNotifyDefault = 5 * time.Minute
	ideaNoticesName   = "notices.jsonl"
)

var (
	ideaNotifyLine      = regexp.MustCompile(`(?m)^ideaNotify:.*$`)
	ideaNotifyEveryLine = regexp.MustCompile(`(?m)^ideaNotifyEvery:.*$`)
	ideaNoticesMu       sync.Mutex
)

// ideaNotifyMode is how idea pages ring, and how often a batch goes out.
func ideaNotifyMode(dir string) (string, time.Duration) {
	user, err := config.LoadUser(agentUserConfigPath(dir))
	if err != nil || user == nil {
		return ideaNotifyOnEach, ideaNotifyDefault
	}
	every := ideaNotifyDefault
	if d, err := parseLinkFor(user.IdeaNotifyEvery); err == nil && d >= time.Minute {
		every = d
	}
	switch strings.ToLower(strings.TrimSpace(user.IdeaNotify)) {
	case ideaNotifyOff:
		return ideaNotifyOff, every
	case ideaNotifyBatch:
		return ideaNotifyBatch, every
	}
	return ideaNotifyOnEach, every
}

func ideaNotifyOn(dir string) bool {
	mode, _ := ideaNotifyMode(dir)
	return mode != ideaNotifyOff
}

type ideaNotice struct {
	Slug   string    `json:"slug"`
	Page   string    `json:"page"`
	What   string    `json:"what"`
	Detail string    `json:"detail,omitempty"`
	At     time.Time `json:"at"`
}

// ideaNotify tells the person about a change on an idea page, with the
// page's link: now, in the next batch, or not at all.
var ideaNotify = func(dir string, m idea.Meta, what, detail string) {
	mode, _ := ideaNotifyMode(dir)
	n := ideaNotice{Slug: m.Slug, Page: firstNonEmpty(m.Name, m.Title), What: what, Detail: strings.TrimSpace(detail), At: time.Now()}
	switch mode {
	case ideaNotifyOff:
		return
	case ideaNotifyBatch:
		queueIdeaNotice(dir, n)
		return
	}
	sendIdeaNotice(dir, "💡 "+n.Page+": "+what, n.Detail, ideaLink(dir, m, true), ideaLink(dir, m, false))
}

var sendIdeaNotice = func(dir, title, body, localLink, link string) {
	utils.NotifyWithLink(title, body, localLink)
	if user, err := config.LoadUser(agentUserConfigPath(dir)); err == nil && user != nil && user.NotifyUrl != "" {
		if send := webhookLinkNotifier(user.NotifyUrl, nil); send != nil {
			send(title, body, link)
		}
	}
}

func ideaNoticesPath(dir string) string { return filepath.Join(idea.Root(dir), ideaNoticesName) }

// queueIdeaNotice keeps a notice for the next summary. Any corgi process
// may queue; the dashboard sends.
func queueIdeaNotice(dir string, n ideaNotice) {
	ideaNoticesMu.Lock()
	defer ideaNoticesMu.Unlock()
	data, err := json.Marshal(n)
	if err != nil {
		return
	}
	_ = os.MkdirAll(idea.Root(dir), 0o700)
	f, err := os.OpenFile(ideaNoticesPath(dir), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(data, '\n'))
}

func readIdeaNotices(path string) []ideaNotice {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []ideaNotice
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var n ideaNotice
		if json.Unmarshal(sc.Bytes(), &n) == nil && n.Slug != "" {
			out = append(out, n)
		}
	}
	return out
}

// flushIdeaNotices sends the queued notices as one summary once the oldest
// has waited its turn - at once when batching was turned off meanwhile.
func flushIdeaNotices(dir string, now time.Time) {
	ideaNoticesMu.Lock()
	queued := readIdeaNotices(ideaNoticesPath(dir))
	mode, every := ideaNotifyMode(dir)
	if len(queued) == 0 || (mode == ideaNotifyBatch && now.Sub(queued[0].At) < every) {
		ideaNoticesMu.Unlock()
		return
	}
	_ = os.Remove(ideaNoticesPath(dir))
	ideaNoticesMu.Unlock()
	if mode == ideaNotifyOff {
		return
	}
	title, body, slug := ideaSummary(queued)
	local, link := "", ""
	if slug != "" {
		if id, err := idea.Load(dir, slug); err == nil {
			local, link = ideaLink(dir, id.Meta, true), ideaLink(dir, id.Meta, false)
		}
	} else if l, p := ideaLinkBases(); l != "" || p != "" {
		local, link = firstNonEmpty(l, p)+"/idea/", firstNonEmpty(p, l)+"/idea/"
	}
	sendIdeaNotice(dir, title, body, local, link)
}

// ideaSummary is a batch as one notification: a line per page, newest
// page first; slug is set when it is all one page, for its link.
func ideaSummary(ns []ideaNotice) (title, body, slug string) {
	type page struct {
		name  string
		whats []string
		last  time.Time
	}
	pages := map[string]*page{}
	var order []string
	for _, n := range ns {
		p := pages[n.Slug]
		if p == nil {
			p = &page{name: n.Page}
			pages[n.Slug] = p
			order = append(order, n.Slug)
		}
		p.whats = append(p.whats, n.What)
		p.last = n.At
	}
	sort.SliceStable(order, func(i, j int) bool { return pages[order[i]].last.After(pages[order[j]].last) })
	var lines []string
	for i, s := range order {
		if i == 6 {
			lines = append(lines, fmt.Sprintf("and %d more page(s)", len(order)-6))
			break
		}
		p := pages[s]
		lines = append(lines, p.name+": "+summarizeWhats(p.whats))
	}
	title = fmt.Sprintf("💡 %d update(s) on %d page(s)", len(ns), len(order))
	if len(order) == 1 {
		slug = order[0]
		title = fmt.Sprintf("💡 %s: %d update(s)", pages[slug].name, len(ns))
	}
	return title, strings.Join(lines, "\n"), slug
}

// summarizeWhats folds repeats: "comment by max ×3, answered 2 comment(s)".
func summarizeWhats(whats []string) string {
	count := map[string]int{}
	var order []string
	for _, w := range whats {
		if count[w] == 0 {
			order = append(order, w)
		}
		count[w]++
	}
	var parts []string
	for _, w := range order {
		if count[w] > 1 {
			w = fmt.Sprintf("%s ×%d", w, count[w])
		}
		parts = append(parts, w)
	}
	return clipTitle(strings.Join(parts, ", "), 300)
}

var ideaNotifyCmd = &cobra.Command{
	Use:   "notify [on|batch|off] [--every 10m]",
	Short: "How comments, answers and page changes ring: each at once, one summary every few minutes, or not at all",
	Long: `  corgi idea notify                 what is set now
  corgi idea notify on              each comment and answer rings as it lands (the default)
  corgi idea notify batch           one summary every 5 minutes: "Checkout: comment by max ×3, answered 2 comment(s)"
  corgi idea notify batch --every 15m
  corgi idea notify off             idea pages stay quiet

They ring on the desktop and the notifyUrl (Telegram for most). The
dashboard (corgi agent up) sends the batches; nothing is lost while it is
down, the summary goes out when it is back.`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		path := agentUserConfigPath(dir)
		if cmd.Flags().Changed("every") {
			raw, _ := cmd.Flags().GetString("every")
			d, err := parseLinkFor(raw)
			if err != nil || d < time.Minute {
				exitWithError("idea", fmt.Errorf("--every is a minute or more, like 5m or 1h"), 2)
			}
			if err := writeUserConfigLine(path, ideaNotifyEveryLine, "ideaNotifyEvery: "+raw); err != nil {
				exitWithError("idea", err, 1)
			}
		}
		if len(args) == 1 {
			want := strings.ToLower(args[0])
			if want != ideaNotifyOnEach && want != ideaNotifyBatch && want != ideaNotifyOff {
				exitWithError("idea", fmt.Errorf("on, batch or off"), 2)
			}
			if err := writeUserConfigLine(path, ideaNotifyLine, "ideaNotify: "+want); err != nil {
				exitWithError("idea", err, 1)
			}
		}
		mode, every := ideaNotifyMode(dir)
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"ideaNotify": mode, "every": every.String()})
			return
		}
		switch mode {
		case ideaNotifyBatch:
			fmt.Printf("idea notifications: one summary every %s\n", every)
		default:
			fmt.Printf("idea notifications: %s\n", mode)
		}
	},
}

func init() {
	ideaNotifyCmd.Flags().String("every", "", "with batch: how often the summary goes out (5m by default)")
}
