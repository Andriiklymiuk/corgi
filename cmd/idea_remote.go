package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/idea"
	"github.com/spf13/cobra"
)

// A page is shared as a link: /idea/<slug>?k=<key> on someone's laptop,
// often through a tunnel. Everything a person does on it in the browser,
// an agent does with the same link - from this machine or another one,
// with corgi or with plain HTTP.

// ideaRemote is a page someone shared, addressed by its link.
type ideaRemote struct {
	Base string // https://host/idea/<slug>
	Slug string
	Key  string
}

// parseIdeaLink reads a page's link; false when s is not one (a slug, a file).
func parseIdeaLink(s string) (ideaRemote, bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		return ideaRemote{}, false
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return ideaRemote{}, false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] != "idea" || !idea.ValidSlug(parts[1]) {
		return ideaRemote{}, false
	}
	return ideaRemote{Base: u.Scheme + "://" + u.Host + "/idea/" + parts[1], Slug: parts[1], Key: u.Query().Get("k")}, true
}

func (r ideaRemote) url(part string) string {
	u := r.Base
	if part != "" {
		u += "/" + part
	}
	if r.Key != "" {
		u += "?k=" + url.QueryEscape(r.Key)
	}
	return u
}

func (r ideaRemote) Link() string { return r.url("") }

var ideaRemoteClient = &http.Client{Timeout: 30 * time.Second}

func (r ideaRemote) do(method, part string, body any) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, r.url(part), rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	// A free ngrok tunnel shows browsers a warning page first; this skips it.
	req.Header.Set("ngrok-skip-browser-warning", "1")
	if body != nil {
		req.Header.Set(headerContentType, "application/json")
	}
	resp, err := ideaRemoteClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach the page: %v", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			return nil, fmt.Errorf("%s", e.Error)
		}
		return nil, fmt.Errorf("the page answered %s", resp.Status)
	}
	return data, nil
}

// followDoc is the part of a page a follower compares between two reads.
type followDoc struct {
	Idea      idea.Meta      `json:"idea"`
	Markdown  string         `json:"markdown"`
	Comments  []idea.Comment `json:"comments"`
	Approvers []string       `json:"approvers"`
	Needed    int            `json:"needed"`
	Approved  bool           `json:"approved"`
}

func (r ideaRemote) doc() (followDoc, error) {
	var d followDoc
	data, err := r.do(http.MethodGet, "doc", nil)
	if err != nil {
		return d, err
	}
	return d, json.Unmarshal(data, &d)
}

func localFollowDoc(dir, slug string) (followDoc, error) {
	id, err := idea.Load(dir, slug)
	if err != nil {
		return followDoc{}, err
	}
	return followDoc{Idea: id.Meta, Markdown: id.Doc, Comments: id.Comments.Comments, Approvers: id.Meta.Approvers(), Needed: id.Meta.NeededApprovals(), Approved: id.Meta.IsApproved()}, nil
}

// ---- the page for agents ----------------------------------------------------

// ideaAgentText is the page as an agent reads it: what it is, how to take
// part with nothing but HTTP, the text, the decisions and every thread.
func ideaAgentText(link ideaRemote, id *idea.Idea) string {
	m := id.Meta
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", firstNonEmpty(m.Name, m.Title))
	fmt.Fprintf(&b, "A %s page, %s, version %d (changed %s). %d comment(s) open.", firstNonEmpty(m.Type, idea.TypeIdea), firstNonEmpty(m.Status, idea.Draft), m.CurrentRev()+1, m.UpdatedAt.UTC().Format(time.RFC3339), id.Open())
	if len(m.Approvals) > 0 || m.Needed > 1 {
		fmt.Fprintf(&b, " Signed off by %d of %d.", len(m.Approvers()), m.NeededApprovals())
	}
	b.WriteString("\n\n## How to take part\n\n")
	b.WriteString("People and agents comment on this page; its own agent answers each comment and amends the page. Comment the way a careful reviewer would: on the exact words, with the reason.\n\n")
	fmt.Fprintf(&b, "- Read it again (this text): `GET %s`\n", link.url("agent"))
	fmt.Fprintf(&b, "- The same as JSON (comments with ids): `GET %s`\n", link.url("doc"))
	fmt.Fprintf(&b, "- Comment on words: `POST %s` with `{\"by\": \"your name\", \"text\": \"...\", \"quote\": \"exact words from the page\"}` (leave quote out for the whole page)\n", link.url("comment"))
	fmt.Fprintf(&b, "- Reply in a thread: `POST %s` with `{\"by\": \"...\", \"id\": 3, \"text\": \"...\"}`\n", link.url("reply"))
	fmt.Fprintf(&b, "- Sign it off: `POST %s` with `{\"by\": \"...\"}`\n", link.url("approve"))
	fmt.Fprintf(&b, "- Follow changes: `GET %s` (server-sent events; read again on each `change`)\n", link.url("stream"))
	fmt.Fprintf(&b, "- With corgi: `corgi idea comment \"%s\" \"...\" --quote \"...\"`, `corgi idea follow \"%s\"`\n", link.Link(), link.Link())
	b.WriteString("\n## The page\n\n")
	b.WriteString(strings.TrimSpace(id.Doc))
	b.WriteString("\n")
	if len(m.Decisions) > 0 {
		b.WriteString("\n## Decided\n\n")
		b.WriteString(decisionsText(m.Decisions))
	}
	if len(id.Comments.Comments) > 0 {
		b.WriteString("\n## Comments\n\n")
		b.WriteString(idea.Transcript(id.Comments.Comments))
	}
	return b.String()
}

// ideaRequestBase is where the one who asked reached the page: the public
// address when they came through the tunnel, this machine's otherwise.
func ideaRequestBase(r *http.Request) string {
	host := r.Host
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	if _, public := ideaLinkBases(); public != "" && h != "localhost" && net.ParseIP(h) == nil {
		return public
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + host
}

// ideaShellFor is the page's shell with a line for agents that fetch the
// link: where to read it as text and how to comment.
func ideaShellFor(r *http.Request, slug string) string {
	link := ideaRemote{Base: ideaRequestBase(r) + "/idea/" + slug, Slug: slug, Key: r.URL.Query().Get("k")}
	agent := htmlEscape(link.url("agent"))
	line := `<p class="agents" id="agents">AI agents: this page is drawn by script. Read it as text, with how to comment, at <a href="` + agent + `">` + agent + `</a>.</p>`
	return strings.Replace(ideaPageHTML, "</main>", "</main>\n"+line, 1)
}

// ---- remote verbs -----------------------------------------------------------

func remoteIdeaShow(r ideaRemote) {
	if utils.JSONOutput {
		data, err := r.do(http.MethodGet, "doc", nil)
		if err != nil {
			exitWithError("idea", err, 1)
		}
		_, _ = os.Stdout.Write(append(data, '\n'))
		return
	}
	data, err := r.do(http.MethodGet, "agent", nil)
	if err != nil {
		exitWithError("idea", err, 1)
	}
	fmt.Print(string(data))
}

type remoteComment struct {
	Text, By, Quote, Prefix, Suffix, Heading string
	Links, Files                             []string
	ReplyTo                                  int
}

func remoteIdeaComment(r ideaRemote, c remoteComment) (any, error) {
	var files []idea.Attachment
	for _, f := range c.Files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		out, err := r.do(http.MethodPost, "file", map[string]any{"name": filepath.Base(f), "type": mimeFor(f), "data": base64.StdEncoding.EncodeToString(data)})
		if err != nil {
			return nil, err
		}
		var got struct {
			File idea.Attachment `json:"file"`
		}
		if err := json.Unmarshal(out, &got); err != nil {
			return nil, err
		}
		files = append(files, got.File)
	}
	body := map[string]any{"by": c.By, "text": c.Text, "links": c.Links, "files": files}
	what := "comment"
	if c.ReplyTo > 0 {
		what, body["id"] = "reply", c.ReplyTo
	} else {
		body["quote"], body["prefix"], body["suffix"], body["heading"] = c.Quote, c.Prefix, c.Suffix, c.Heading
	}
	out, err := r.do(http.MethodPost, what, body)
	if err != nil {
		return nil, err
	}
	var res map[string]any
	return res, json.Unmarshal(out, &res)
}

// ---- following a page -------------------------------------------------------

type followEvent struct {
	Event   string `json:"event"`
	Page    string `json:"page"`
	Version int    `json:"version,omitempty"`
	Comment int    `json:"comment,omitempty"`
	By      string `json:"by,omitempty"`
	Text    string `json:"text,omitempty"`
	Quote   string `json:"quote,omitempty"`
	Status  string `json:"status,omitempty"`
}

func (e followEvent) line() string {
	switch e.Event {
	case "version":
		s := fmt.Sprintf("version %d", e.Version)
		if e.By != "" {
			s += " by " + e.By
		}
		if e.Text != "" {
			s += ": " + e.Text
		}
		return s
	case "comment":
		s := fmt.Sprintf("comment %d by %s", e.Comment, e.By)
		if e.Quote != "" {
			s += " on “" + clipTitle(e.Quote, 60) + "”"
		}
		return s + ": " + clipTitle(e.Text, 200)
	case "reply":
		return fmt.Sprintf("reply under %d by %s: %s", e.Comment, e.By, clipTitle(e.Text, 200))
	case "status":
		return fmt.Sprintf("comment %d is %s", e.Comment, e.Status)
	case "approved":
		return "approved by " + e.Text
	case "decided":
		return "decided: " + e.Text
	}
	return e.Event
}

// followChanges is what moved between two reads of a page.
func followChanges(before, after followDoc) []followEvent {
	page := firstNonEmpty(after.Idea.Name, after.Idea.Title, after.Idea.Slug)
	var out []followEvent
	if v := after.Idea.CurrentRev(); v > before.Idea.CurrentRev() || (v == before.Idea.CurrentRev() && after.Markdown != before.Markdown) {
		e := followEvent{Event: "version", Page: page, Version: v + 1}
		for _, r := range after.Idea.Revisions {
			if r.ID == v {
				e.By, e.Text = r.By, r.Note
			}
		}
		out = append(out, e)
	}
	old := map[int]idea.Comment{}
	for _, c := range before.Comments {
		old[c.ID] = c
	}
	for _, c := range after.Comments {
		o, seen := old[c.ID]
		if !seen {
			e := followEvent{Event: "comment", Page: page, Comment: c.ID, By: c.By, Text: c.Text, Status: c.Status}
			if c.Anchor != nil {
				e.Quote = c.Anchor.Quote
			}
			out = append(out, e)
		}
		for i, r := range c.Replies {
			if i >= len(o.Replies) {
				out = append(out, followEvent{Event: "reply", Page: page, Comment: c.ID, By: r.By, Text: r.Text})
			}
		}
		if seen && o.Status != c.Status {
			out = append(out, followEvent{Event: "status", Page: page, Comment: c.ID, Status: c.Status})
		}
	}
	if after.Approved && !before.Approved {
		out = append(out, followEvent{Event: "approved", Page: page, Text: strings.Join(after.Approvers, ", ")})
	}
	for i := len(before.Idea.Decisions); i < len(after.Idea.Decisions); i++ {
		out = append(out, followEvent{Event: "decided", Page: page, Text: after.Idea.Decisions[i].Text})
	}
	return out
}

// waitForChange blocks until the page may have changed: the link's event
// stream, or a poll when the stream cannot be had.
func (r ideaRemote) waitForChange(ctx context.Context, events chan<- struct{}) {
	backoff := time.Second
	for ctx.Err() == nil {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, r.url("stream"), nil)
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("ngrok-skip-browser-warning", "1")
		resp, err := http.DefaultClient.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			backoff = time.Second
			sc := bufio.NewScanner(resp.Body)
			for sc.Scan() {
				// hello too: a change between the last read and this connection is not missed.
				if strings.HasPrefix(sc.Text(), "event: change") || strings.HasPrefix(sc.Text(), "event: hello") {
					select {
					case events <- struct{}{}:
					default:
					}
				}
			}
			resp.Body.Close()
		} else if resp != nil {
			resp.Body.Close()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		select {
		case events <- struct{}{}:
		default:
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

var ideaFollowCmd = &cobra.Command{
	Use:   "follow <slug | link>",
	Short: "Follow a page: every new version, comment, reply, sign-off and decision as it lands, here or on someone else's laptop",
	Long: `  corgi idea follow checkout
  corgi idea follow "https://….trycloudflare.com/idea/checkout?k=…"
  corgi idea follow checkout --json        one JSON line per change, for an agent
  corgi idea follow checkout --notify      a desktop notification too

A slug is a page on this machine; a link is a page anyone shared with you -
the same link the browser opens. Runs until stopped.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		notify, _ := cmd.Flags().GetBool("notify")
		once, _ := cmd.Flags().GetBool("once")
		read, changes := followSource(args[0])
		last, err := read()
		if err != nil {
			exitWithError("idea", err, 1)
		}
		name := firstNonEmpty(last.Idea.Name, last.Idea.Title, last.Idea.Slug)
		if !utils.JSONOutput {
			fmt.Printf("following %s (version %d, %d comment(s)); ctrl-c stops\n", name, last.Idea.CurrentRev()+1, len(last.Comments))
		}
		if once {
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		ch := make(chan struct{}, 1)
		go changes(ctx, ch)
		for range ch {
			now, err := read()
			if err != nil {
				utils.Infof("%s: %v\n", name, err)
				continue
			}
			for _, e := range followChanges(last, now) {
				if utils.JSONOutput {
					data, _ := json.Marshal(e)
					fmt.Println(string(data))
				} else {
					fmt.Printf("%s  %s\n", time.Now().Format("15:04"), e.line())
				}
				if notify {
					utils.Notify("💡 "+e.Page, e.line())
				}
			}
			last = now
		}
	},
}

// followSource reads a page and says when it may have changed: a link
// through its stream, a slug on this machine through its files.
func followSource(target string) (func() (followDoc, error), func(context.Context, chan<- struct{})) {
	if r, ok := parseIdeaLink(target); ok {
		return r.doc, r.waitForChange
	}
	dir := mustAgentDir()
	slug := target
	if _, err := idea.Load(dir, slug); err != nil {
		exitWithError("idea", err, 2)
	}
	read := func() (followDoc, error) { return localFollowDoc(dir, slug) }
	poll := func(ctx context.Context, ch chan<- struct{}) {
		stamp := func() string {
			var b strings.Builder
			for _, f := range []string{idea.MetaName, idea.DocName, idea.CommentsName} {
				if info, err := os.Stat(filepath.Join(idea.Dir(dir, slug), f)); err == nil {
					fmt.Fprintf(&b, "%d:%d;", info.ModTime().UnixNano(), info.Size())
				}
			}
			return b.String()
		}
		last := stamp()
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if s := stamp(); s != last {
					last = s
					select {
					case ch <- struct{}{}:
					default:
					}
				}
			}
		}
	}
	return read, poll
}

func init() {
	ideaFollowCmd.Flags().Bool("notify", false, "a desktop notification for each change")
	ideaFollowCmd.Flags().Bool("once", false, "read the page once and stop (checks the link works)")
	ideaCmd.AddCommand(ideaFollowCmd)
}
