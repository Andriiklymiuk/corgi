package cmd

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils/agent/idea"
	"andriiklymiuk/corgi/utils/agent/watch"
	"github.com/russross/blackfriday/v2"
)

const (
	headerContentSecurityPolicy = "Content-Security-Policy"
	htmlContentType             = "text/html; charset=utf-8"
)

// The idea page over HTTP, on the dashboard's port.
//
//	GET  /idea/<slug>?k=<key>            the page (HTML)
//	GET  /idea/<slug>/doc?k=<key>        the page, comments and tasks as JSON
//	GET  /idea/<slug>/stream?k=<key>     server-sent events: "change" when any of it moves
//	POST /idea/<slug>/comment            {text, by, quote, prefix, suffix, heading, links, files}
//	POST /idea/<slug>/reply              {id, text, by, links, files}
//	POST /idea/<slug>/status             {id, status}
//	POST /idea/<slug>/file               {name, type, data (base64)} -> an attachment to put on a comment
//	GET  /idea/<slug>/file/<name>        the attachment
//	POST /idea/<slug>/build              {workspace, slots, run}   dashboard token or paired device only
//	POST /idea/<slug>/edit               {markdown}                 same
//	POST /idea/<slug>/auto               {auto}                     same
//	POST /idea/<slug>/approve            {by, off}                  anyone with the link signs off
//	POST /idea/<slug>/decide             {text, id, drop}           dashboard token or paired device only
//	GET  /idea/<slug>/mock                                          the mockup, sandboxed
//	GET  /idea/<slug>/agent                                         the page as an agent reads it: markdown, how to comment, every thread
//	POST /idea/<slug>/mock                                          the agent draws it; dashboard or device only
//
// The key in the link is what lets anyone it was sent to read and comment;
// a page with no key answers only to the dashboard's token or a paired
// device. Phones use /launch/ideas and /launch/idea (same JSON, same
// actions under "do"), which the usual device auth and sealing cover.

type ideaAction struct {
	Do        string            `json:"do"`
	Slug      string            `json:"slug"`
	Text      string            `json:"text"`
	By        string            `json:"by"`
	Quote     string            `json:"quote"`
	Prefix    string            `json:"prefix"`
	Suffix    string            `json:"suffix"`
	Heading   string            `json:"heading"`
	Links     []string          `json:"links"`
	Files     []idea.Attachment `json:"files"`
	ID        int               `json:"id"`
	Status    string            `json:"status"`
	Workspace string            `json:"workspace"`
	Slots     int               `json:"slots"`
	Run       *bool             `json:"run"`
	Name      string            `json:"name"`
	Type      string            `json:"type"`
	Data      string            `json:"data"`
	Markdown  string            `json:"markdown"`
	Auto      *bool             `json:"auto"`
	// new: Type (the page type; for a file it is the MIME type) and
	// Markdown (a page already written); send: To (jira, linear,
	// confluence), Workspace doubles as the project or team key, Space is
	// the Confluence space.
	To    string `json:"to"`
	Space string `json:"space"`
	// set: Name, Summary, Tags, Workspace, Type, Auto.
	Summary string   `json:"summary"`
	Tags    []string `json:"tags"`
	// share: Off closes the link, Rotate gives it a new key; approve: Off
	// takes the sign-off back.
	Off    bool `json:"off"`
	Rotate bool `json:"rotate"`
	// share: For is how long the link works (7d, 12h; "0" lifts it),
	// ReadOnly makes it read-only or opens comments again.
	For      string `json:"for"`
	ReadOnly *bool  `json:"readOnly"`
	// decide: Text is the decision, ID the thread it rewords, Drop the
	// place of one to remove.
	Drop int `json:"drop"`
}

const ideaBodyLimit = 12 << 20

// ideaDocument is the page as a client draws it. trusted adds what only
// the owner should see: the file's path on this machine and the share link.
func ideaDocument(dir string, id *idea.Idea, withHTML, trusted bool) map[string]any {
	type taskRow struct {
		Ref   string `json:"ref"`
		Key   string `json:"key"`
		Title string `json:"title"`
		State string `json:"state"`
	}
	tasks := []taskRow{}
	board := watch.LoadTasks(dir)
	for _, t := range board.ByIdea(id.Meta.Slug) {
		tasks = append(tasks, taskRow{Ref: t.Ref(), Key: t.Key(), Title: t.Title, State: t.State})
	}
	plan := map[string]any(nil)
	if id.Meta.Plan > 0 {
		if p, ok := watch.LoadPlans(dir).Find(fmt.Sprintf("P-%d", id.Meta.Plan)); ok {
			pr := p.Progress(board)
			plan = map[string]any{"ref": p.Ref(), "state": p.State, "todo": pr.Todo, "doing": pr.Doing, "review": pr.Review, "done": pr.Done}
		}
	}
	meta := id.Meta
	if !trusted {
		meta.Key, meta.Source = "", ""
	}
	out := map[string]any{
		"idea":      meta,
		"markdown":  id.Doc,
		"comments":  id.Comments.Comments,
		"tasks":     tasks,
		"tickets":   liveTickets(dir, id.Meta),
		"sent":      id.Meta.Sent,
		"send":      ideaSendOptions(dir, id.Meta),
		"plan":      plan,
		"counts":    id.Counts(),
		"open":      id.Open(),
		"forAgent":  len(id.ForAgent()),
		"approvers": id.Meta.Approvers(),
		"needed":    id.Meta.NeededApprovals(),
		"approved":  id.Meta.IsApproved(),
	}
	if trusted {
		out["file"] = filepath.Join(id.Dir, idea.DocName)
		local, public := ideaLinkBases()
		out["link"] = ideaLinkFrom(local, public, id.Meta, false)
		out["public"] = id.Meta.Key != "" && public != ""
	}
	if withHTML {
		out["html"] = renderIdeaHTML(id.Doc)
	}
	return out
}

// renderIdeaHTML is the page as HTML: raw HTML in the markdown is dropped,
// and no smart quotes, so the words on the screen are the words in the
// file and a highlight can be found again.
func renderIdeaHTML(md string) string {
	r := mediaRenderer{blackfriday.NewHTMLRenderer(blackfriday.HTMLRendererParameters{Flags: blackfriday.UseXHTML | blackfriday.SkipHTML | blackfriday.Safelink})}
	return string(blackfriday.Run([]byte(htmlMediaToMarkdown(md)), blackfriday.WithRenderer(r),
		blackfriday.WithExtensions(blackfriday.CommonExtensions&^blackfriday.Autolink|blackfriday.AutoHeadingIDs)))
}

// ideaTrusted says the request carries the dashboard's own token or a
// paired device that may act (not a viewer, not a peer, not one that must
// speak sealed - those go through /launch/idea).
func ideaTrusted(r *http.Request, token, deviceStore string) bool {
	if token == "" && deviceStore == "" {
		return true
	}
	got := r.Header.Get("Authorization")
	if token != "" && subtle.ConstantTimeCompare([]byte(got), []byte(bearerPrefix+token)) == 1 {
		return true
	}
	device, ok := authorizedDeviceFull(deviceStore, got)
	return ok && !device.Viewer() && !device.Peer() && !device.Encrypted()
}

func ideaOpenHandler(token, deviceStore string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setLaunchHeaders(w)
		// The link carries its key: never into a search index, never in a Referer to another site.
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		w.Header().Set("Referrer-Policy", "no-referrer")
		parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/idea/"), "/", 3)
		for len(parts) < 3 {
			parts = append(parts, "")
		}
		slug, what, name := parts[0], parts[1], parts[2]
		trusted := ideaTrusted(r, token, deviceStore)
		switch {
		case slug == "":
			ideaIndex(w, r, trusted)
		case what == "":
			// The page is a shell; it asks for /doc with the key, and is told there when the key is wrong.
			w.Header().Set(headerContentType, htmlContentType)
			w.Header().Set(headerContentSecurityPolicy, ideaPageCSP)
			_, _ = fmt.Fprint(w, ideaShellFor(r, slug, trusted))
		default:
			ideaPart(w, r, ideaPartRequest{slug: slug, what: what, name: name, trusted: trusted})
		}
	}
}

// ideaIndex is the menu of every page: a shell for any browser, the list
// itself only for the dashboard's own (a share key opens one page, not all).
func ideaIndex(w http.ResponseWriter, r *http.Request, trusted bool) {
	if !strings.Contains(r.Header.Get("Accept"), "json") {
		w.Header().Set(headerContentType, htmlContentType)
		_, _ = fmt.Fprint(w, ideaIndexHTML)
		return
	}
	if !trusted {
		writeLaunchError(w, http.StatusForbidden, "the list of pages is for the dashboard: open it from corgi agent dashboard")
		return
	}
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeLaunchJSON(w, map[string]any{"ideas": ideaRows(dir, true)})
}

type ideaPartRequest struct {
	slug, what, name string
	trusted          bool
}

func guestRefusal(w http.ResponseWriter, r *http.Request, id *idea.Idea, slug string) (int, string) {
	if id.Meta.Key == "" || subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("k")), []byte(id.Meta.Key)) != 1 {
		return http.StatusForbidden, "this page is closed - ask for a fresh link"
	}
	if !id.Meta.LinkOpen(time.Now()) {
		return http.StatusForbidden, "this link has expired - ask for a fresh one"
	}
	if r.Method == http.MethodGet {
		return 0, ""
	}
	if id.Meta.ReadOnly {
		return http.StatusForbidden, "this link is for reading only"
	}
	if !ideaGuestWrites.allow(slug, guestAddress(r), time.Now()) {
		w.Header().Set("Retry-After", "60")
		return http.StatusTooManyRequests, "too many comments at once - wait a minute"
	}
	return 0, ""
}

var ideaPostParts = map[string]bool{"comment": true, "reply": true, "status": true, "build": true, "edit": true, "auto": true,
	"send": true, "set": true, "share": true, "approve": true, "decide": true, "delete": true}

func ideaPart(w http.ResponseWriter, r *http.Request, req ideaPartRequest) {
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	id, err := idea.Load(dir, req.slug)
	if err != nil {
		writeLaunchError(w, http.StatusNotFound, "no such idea page")
		return
	}
	if !req.trusted {
		if status, msg := guestRefusal(w, r, id, req.slug); status != 0 {
			writeLaunchError(w, status, msg)
			return
		}
	}
	if ideaPostParts[req.what] {
		ideaPostPart(w, r, dir, req)
		return
	}
	ideaReadPart(w, r, dir, id, req)
}

func ideaReadPart(w http.ResponseWriter, r *http.Request, dir string, id *idea.Idea, req ideaPartRequest) {
	switch req.what {
	case "doc":
		if r.Method != http.MethodGet {
			writeLaunchError(w, http.StatusMethodNotAllowed, "GET the page")
			return
		}
		writeLaunchJSON(w, ideaDocument(dir, id, true, req.trusted))
	case "rev":
		n, _ := strconv.Atoi(req.name)
		rv, doc, err := id.RevisionDoc(n)
		if err != nil {
			writeLaunchError(w, ideaHTTPStatus(err), err.Error())
			return
		}
		writeLaunchJSON(w, map[string]any{"revision": rv, "markdown": doc, "html": renderIdeaHTML(doc)})
	case "stream":
		ideaStream(w, r, dir, id)
	case "agent":
		w.Header().Set(headerContentType, "text/markdown; charset=utf-8")
		link := ideaRemote{Base: ideaRequestBase(r) + "/idea/" + req.slug, Slug: req.slug, Key: r.URL.Query().Get("k")}
		_, _ = fmt.Fprint(w, ideaAgentText(link, id))
	case "file":
		ideaFile(w, r, dir, id, req.name)
	case "mock":
		ideaMockPart(w, r, dir, id, req)
	default:
		writeLaunchError(w, http.StatusNotFound, "no such part of an idea page")
	}
}

func ideaMockPart(w http.ResponseWriter, r *http.Request, dir string, id *idea.Idea, req ideaPartRequest) {
	switch r.Method {
	case http.MethodGet:
		ideaMock(w, r, id)
	case http.MethodPost:
		writeIdeaResult(w, dir, ideaAction{Do: "mock", Slug: req.slug}, req.trusted)
	default:
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET the mockup, or POST to draw it")
	}
}

func ideaPostPart(w http.ResponseWriter, r *http.Request, dir string, req ideaPartRequest) {
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "POST to "+req.what)
		return
	}
	var a ideaAction
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, ideaBodyLimit)).Decode(&a); err != nil {
		writeLaunchError(w, http.StatusBadRequest, requestUnreadableMessage)
		return
	}
	a.Do, a.Slug = req.what, req.slug
	writeIdeaResult(w, dir, a, req.trusted)
}

func writeIdeaResult(w http.ResponseWriter, dir string, a ideaAction, trusted bool) {
	out, status, err := ideaPerform(dir, a, trusted, "someone")
	if err != nil {
		writeLaunchError(w, status, err.Error())
		return
	}
	writeLaunchJSON(w, out)
}

// ideaMock serves the mockup the agent drew. It is the agent's HTML with its
// own scripts, so it runs sandboxed in an origin of its own: it cannot read
// the page, the key or the dashboard's token, and it cannot call out.
func ideaMock(w http.ResponseWriter, r *http.Request, id *idea.Idea) {
	path := id.FilePath(idea.MockName)
	if path == "" || id.Meta.Mock == "" {
		writeLaunchError(w, http.StatusNotFound, "no mockup yet - corgi idea mock "+id.Meta.Slug)
		return
	}
	w.Header().Set(headerContentType, htmlContentType)
	w.Header().Set(headerContentSecurityPolicy, "sandbox allow-scripts; default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; font-src data:; frame-ancestors 'self'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, path)
}

func ideaFile(w http.ResponseWriter, r *http.Request, dir string, id *idea.Idea, name string) {
	switch r.Method {
	case http.MethodGet:
		path := id.FilePath(name)
		if path == "" {
			writeLaunchError(w, http.StatusNotFound, "no such file on this page")
			return
		}
		mime := mimeFor(path)
		w.Header().Set(headerContentType, mime)
		// Anyone with the link may attach a file: whatever it is, it runs nothing here.
		w.Header().Set(headerContentSecurityPolicy, "default-src 'none'; img-src 'self'; media-src 'self'; style-src 'unsafe-inline'; sandbox")
		if !(strings.HasPrefix(mime, "image/") || strings.HasPrefix(mime, "video/")) || mime == "image/svg+xml" {
			w.Header().Set("Content-Disposition", "attachment; filename=\""+filepath.Base(path)+"\"")
		}
		http.ServeFile(w, r, path)
	case http.MethodPost:
		var a ideaAction
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, ideaBodyLimit)).Decode(&a); err != nil {
			writeLaunchError(w, http.StatusBadRequest, requestUnreadableMessage)
			return
		}
		a.Do, a.Slug = "file", id.Meta.Slug
		out, status, err := ideaPerform(dir, a, true, "someone")
		if err != nil {
			writeLaunchError(w, status, err.Error())
			return
		}
		writeLaunchJSON(w, out)
	default:
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET a file, or POST {name, type, data} to add one")
	}
}

// ideaPerform is one action on a page, from any client. trusted says the
// dashboard's own token or a paired device sent it; the share key alone
// may comment, reply, set a status and attach files, never build or edit.
type ideaCall struct {
	dir     string
	a       ideaAction
	trusted bool
	by      string
	now     time.Time
}

type ideaResult struct {
	out    any
	status int
	err    error
}

func ideaOK(out any) ideaResult { return ideaResult{out: out} }

func ideaFailed(err error) ideaResult { return ideaResult{status: ideaHTTPStatus(err), err: err} }

func ideaRefused(status int, format string, a ...any) ideaResult {
	return ideaResult{status: status, err: fmt.Errorf(format, a...)}
}

func ideaPerformers() map[string]func(ideaCall) ideaResult {
	return map[string]func(ideaCall) ideaResult{
		"comment": ideaDoComment, "reply": ideaDoReply, "status": ideaDoStatus, "file": ideaDoFile,
		"share": ideaDoShare, "send": ideaDoSend, "build": ideaDoBuild, "edit": ideaDoEdit, "set": ideaDoSet,
		"auto": ideaDoAuto, "approve": ideaDoApprove, "delete": ideaDoDelete, "decide": ideaDoDecide, "mock": ideaDoMock,
	}
}

func ideaPerform(dir string, a ideaAction, trusted bool, defaultBy string) (any, int, error) {
	by := firstNonEmpty(strings.TrimSpace(a.By), defaultBy)
	if by == idea.Agent {
		by = "someone"
	}
	do, ok := ideaPerformers()[a.Do]
	if !ok {
		return nil, http.StatusBadRequest, fmt.Errorf("do is comment, reply, status, file, build, edit, auto, send, set, share, approve, decide or mock")
	}
	res := do(ideaCall{dir: dir, a: a, trusted: trusted, by: by, now: time.Now()})
	return res.out, res.status, res.err
}

func commentCounts(id *idea.Idea, out map[string]any) map[string]any {
	out["open"], out["forAgent"] = id.Open(), len(id.ForAgent())
	return out
}

func ideaDoComment(c ideaCall) ideaResult {
	var made idea.Comment
	id, err := idea.Update(c.dir, c.a.Slug, func(id *idea.Idea) error {
		anchor := anchorOrGiven(id.Doc, c.a.Quote, c.a.Prefix, c.a.Suffix, c.a.Heading)
		var err error
		made, err = id.AddComment(idea.KindComment, c.by, c.a.Text, anchor, ownFiles(id, c.a.Files), c.a.Links, c.now)
		if err == nil && !c.trusted {
			id.MarkGuest(made.ID, 0)
			made.Guest = true
		}
		return err
	})
	if err != nil {
		return ideaFailed(err)
	}
	ideaNotify(c.dir, id.Meta, "comment by "+c.by, clipTitle(c.a.Text, 160))
	scheduleIdeaAnswer(c.dir, c.a.Slug)
	return ideaOK(commentCounts(id, map[string]any{"comment": made}))
}

func ideaDoReply(c ideaCall) ideaResult {
	var r idea.Reply
	id, err := idea.Update(c.dir, c.a.Slug, func(id *idea.Idea) error {
		var err error
		r, err = id.Reply(c.a.ID, c.by, c.a.Text, false, ownFiles(id, c.a.Files), c.a.Links, c.now)
		if err == nil && !c.trusted {
			id.MarkGuest(c.a.ID, r.ID)
			r.Guest = true
		}
		return err
	})
	if err != nil {
		return ideaFailed(err)
	}
	ideaNotify(c.dir, id.Meta, "reply by "+c.by, clipTitle(c.a.Text, 160))
	scheduleIdeaAnswer(c.dir, c.a.Slug)
	comment, _ := id.Find(c.a.ID)
	return ideaOK(commentCounts(id, map[string]any{"reply": r, "comment": comment}))
}

func ideaDoStatus(c ideaCall) ideaResult {
	var comment idea.Comment
	id, err := idea.Update(c.dir, c.a.Slug, func(id *idea.Idea) error {
		var err error
		comment, err = id.SetStatus(c.a.ID, c.a.Status, c.now)
		return err
	})
	if err != nil {
		return ideaFailed(err)
	}
	if c.a.Status == idea.StatusOpen {
		scheduleIdeaAnswer(c.dir, c.a.Slug)
	}
	return ideaOK(commentCounts(id, map[string]any{"comment": comment}))
}

func ideaDoFile(c ideaCall) ideaResult {
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(c.a.Data))
	if err != nil || len(data) == 0 {
		return ideaRefused(http.StatusBadRequest, "data is the file, base64")
	}
	id, err := idea.Load(c.dir, c.a.Slug)
	if err != nil {
		return ideaResult{status: http.StatusNotFound, err: err}
	}
	att, err := id.AddFile(c.a.Name, data, firstNonEmpty(c.a.Type, mimeFor(c.a.Name)))
	if err != nil {
		return ideaResult{status: http.StatusBadRequest, err: err}
	}
	return ideaOK(map[string]any{"file": att})
}

func ideaDoShare(c ideaCall) ideaResult {
	if !c.trusted {
		return ideaRefused(http.StatusForbidden, "only the page's owner opens or closes its link")
	}
	if _, err := idea.Load(c.dir, c.a.Slug); err != nil {
		return ideaFailed(err)
	}
	req := shareRequest{Target: c.a.Slug, By: c.by, Off: c.a.Off, Rotate: c.a.Rotate, ReadOnly: c.a.ReadOnly}
	if strings.TrimSpace(c.a.For) != "" {
		d, err := parseLinkFor(c.a.For)
		if err != nil {
			return ideaResult{status: http.StatusBadRequest, err: err}
		}
		req.For = &d
	}
	res, err := shareIdea(c.dir, req, c.now)
	if err != nil {
		return ideaFailed(err)
	}
	return ideaOK(res)
}

func ideaDoSend(c ideaCall) ideaResult {
	if !c.trusted {
		return ideaRefused(http.StatusForbidden, "sending needs the dashboard: open the page from corgi agent dashboard, or run corgi idea send %s", c.a.Slug)
	}
	links, err := sendIdea(c.dir, c.a.Slug, c.a.To, c.a.Workspace, c.a.Space)
	if err != nil {
		return ideaFailed(err)
	}
	return ideaOK(map[string]any{"sent": links})
}

func ideaDoBuild(c ideaCall) ideaResult {
	if !c.trusted {
		return ideaRefused(http.StatusForbidden, "building needs the dashboard: open the page from corgi agent dashboard, or run corgi idea build %s", c.a.Slug)
	}
	res, err := buildIdea(c.dir, c.a.Slug, c.a.Workspace, "", c.a.Slots, c.a.Run == nil || *c.a.Run)
	if err != nil {
		return ideaFailed(err)
	}
	return ideaOK(res)
}

func ideaDoEdit(c ideaCall) ideaResult {
	if !c.trusted {
		return ideaRefused(http.StatusForbidden, "only the dashboard edits the page itself; comment instead")
	}
	id, err := idea.Update(c.dir, c.a.Slug, func(id *idea.Idea) error {
		id.ChangeBy, id.ChangeNote = c.by, "edited"
		return id.SetDoc(c.a.Markdown, c.now)
	})
	if err != nil {
		return ideaFailed(err)
	}
	return ideaOK(ideaDocument(c.dir, id, true, true))
}

func ideaDoSet(c ideaCall) ideaResult {
	if !c.trusted {
		return ideaRefused(http.StatusForbidden, "only the dashboard changes that")
	}
	ch := ideaChanges{Name: c.a.Name, Summary: c.a.Summary, Workspace: c.a.Workspace, Type: c.a.Type}
	if c.a.Auto != nil {
		ch.Auto = map[bool]string{true: "on", false: "off"}[*c.a.Auto]
	}
	if c.a.Tags != nil {
		ch.Tags = &c.a.Tags
	}
	id, err := setIdea(c.dir, c.a.Slug, ch)
	if err != nil {
		return ideaFailed(err)
	}
	return ideaOK(map[string]any{"idea": id.Meta})
}

func ideaDoAuto(c ideaCall) ideaResult {
	if !c.trusted {
		return ideaRefused(http.StatusForbidden, "only the dashboard sets that")
	}
	id, err := idea.Update(c.dir, c.a.Slug, func(id *idea.Idea) error {
		if c.a.Auto != nil {
			id.Meta.Auto = *c.a.Auto
		}
		return nil
	})
	if err != nil {
		return ideaFailed(err)
	}
	if id.Meta.Auto {
		scheduleIdeaAnswer(c.dir, c.a.Slug)
	}
	return ideaOK(map[string]any{"auto": id.Meta.Auto})
}

func ideaDoApprove(c ideaCall) ideaResult {
	res, err := approveIdea(c.dir, c.a.Slug, c.by, c.a.Off, false, c.trusted)
	if err != nil {
		return ideaFailed(err)
	}
	if res.JustApproved && res.Idea.BuildWhenApproved {
		meta, dir, slug := res.Idea, c.dir, c.a.Slug
		go func() {
			if _, err := buildIdea(dir, slug, "", "", 1, true); err != nil {
				ideaNotify(dir, meta, "approved, but not built", err.Error())
			}
		}()
	}
	if !c.trusted {
		res.Idea.Key, res.Idea.Source = "", ""
	}
	return ideaOK(res)
}

func ideaDoDelete(c ideaCall) ideaResult {
	if !c.trusted {
		return ideaRefused(http.StatusForbidden, "only the page's owner removes comments")
	}
	id, err := idea.Update(c.dir, c.a.Slug, func(id *idea.Idea) error { return id.DeleteComment(c.a.ID) })
	if err != nil {
		return ideaFailed(err)
	}
	return ideaOK(commentCounts(id, map[string]any{}))
}

func ideaDoDecide(c ideaCall) ideaResult {
	if !c.trusted {
		return ideaRefused(http.StatusForbidden, "resolving a thread writes its decision; rewording them is for the page's owner")
	}
	id, err := idea.Update(c.dir, c.a.Slug, func(id *idea.Idea) error {
		if c.a.Drop > 0 {
			return id.Undecide(c.a.Drop)
		}
		_, err := id.Decide(c.a.Text, c.by, c.a.ID, c.now)
		return err
	})
	if err != nil {
		return ideaFailed(err)
	}
	return ideaOK(map[string]any{"decisions": id.Meta.Decisions})
}

func ideaDoMock(c ideaCall) ideaResult {
	if !c.trusted {
		return ideaRefused(http.StatusForbidden, "drawing the mockup needs the dashboard: open the page from corgi agent dashboard, or run corgi idea mock %s", c.a.Slug)
	}
	id, err := idea.Load(c.dir, c.a.Slug)
	if err != nil {
		return ideaFailed(err)
	}
	if !id.Meta.Mocking {
		scheduleMock(c.dir, c.a.Slug, mockNotes(id.Comments.Comments))
	}
	return ideaOK(map[string]any{"mocking": true})
}

// ownFiles keeps only attachments that are this page's files, so a
// comment cannot point at anything else.
func ownFiles(id *idea.Idea, files []idea.Attachment) []idea.Attachment {
	var out []idea.Attachment
	for _, f := range files {
		base := filepath.Base(f.Path)
		if path := id.FilePath(base); path != "" {
			info, _ := os.Stat(path)
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			out = append(out, idea.Attachment{Name: firstNonEmpty(f.Name, base), Path: idea.FilesDir + "/" + base, Type: firstNonEmpty(f.Type, mimeFor(base)), Size: size})
		}
	}
	return out
}

var ideaStreamTick = 300 * time.Millisecond

// ideaStream says "change" whenever the page, its comments or its tasks
// move on disk, however they moved (the agent, an editor, a phone).
func ideaStream(w http.ResponseWriter, r *http.Request, dir string, id *idea.Idea) {
	if r.Method != http.MethodGet {
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET to listen")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeLaunchError(w, http.StatusInternalServerError, "this server cannot stream")
		return
	}
	files := []string{filepath.Join(id.Dir, idea.MetaName), filepath.Join(id.Dir, idea.DocName), filepath.Join(id.Dir, idea.CommentsName), filepath.Join(dir, "watch", "tasks.json")}
	stamp := func() string {
		var b strings.Builder
		for _, f := range files {
			if info, err := os.Stat(f); err == nil {
				fmt.Fprintf(&b, "%d:%d;", info.ModTime().UnixNano(), info.Size())
			} else {
				b.WriteString("-;")
			}
		}
		return b.String()
	}
	w.Header().Set(headerContentType, "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	write := func(event string, v any) bool {
		data, _ := json.Marshal(v)
		if _, err := w.Write([]byte("event: " + event + "\ndata: " + string(data) + "\n\n")); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if !write("hello", map[string]any{"at": time.Now(), "slug": id.Meta.Slug}) {
		return
	}
	last := stamp()
	seq := 0
	tick := time.NewTicker(ideaStreamTick)
	defer tick.Stop()
	ping := time.NewTicker(streamPing)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			flusher.Flush()
		case <-tick.C:
			if s := stamp(); s != last {
				last = s
				seq++
				if !write("change", map[string]any{"seq": seq, "at": time.Now(), "what": []string{"idea"}}) {
					return
				}
			}
		}
	}
}

// ---- for paired devices: the phone -----------------------------------------

func launchIdeasHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodGet {
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET the idea pages")
		return
	}
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	mode, every := ideaNotifyMode(dir)
	writeLaunchJSON(w, map[string]any{"ideas": ideaRows(dir, !isViewer(r)), "notify": mode != ideaNotifyOff, "notifyMode": mode, "notifyEvery": every.String()})
}

func launchIdeaHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	switch r.Method {
	case http.MethodGet:
		launchIdeaGet(w, r, dir)
	case http.MethodPost:
		var a ideaAction
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, ideaBodyLimit)).Decode(&a); err != nil {
			writeLaunchError(w, http.StatusBadRequest, requestUnreadableMessage)
			return
		}
		if a.Do == "new" {
			launchIdeaNew(w, dir, a)
			return
		}
		if a.Do == "notify" {
			launchIdeaNotify(w, dir, a)
			return
		}
		out, status, err := ideaPerform(dir, a, true, "phone")
		if err != nil {
			writeLaunchError(w, status, err.Error())
			return
		}
		writeLaunchJSON(w, out)
	default:
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET ?slug=, or POST {slug, do, …}")
	}
}

// launchIdeaGet answers a device reading a page: the document, one
// revision of it (rev=), or one of its files (file=).
func launchIdeaGet(w http.ResponseWriter, r *http.Request, dir string) {
	q := r.URL.Query()
	id, err := idea.Load(dir, strings.TrimSpace(q.Get("slug")))
	if err != nil {
		writeLaunchError(w, http.StatusNotFound, err.Error())
		return
	}
	if rev := strings.TrimSpace(q.Get("rev")); rev != "" {
		n, _ := strconv.Atoi(rev)
		rv, doc, err := id.RevisionDoc(n)
		if err != nil {
			writeLaunchError(w, ideaHTTPStatus(err), err.Error())
			return
		}
		writeLaunchJSON(w, map[string]any{"revision": rv, "markdown": doc})
		return
	}
	if name := strings.TrimSpace(q.Get("file")); name != "" {
		launchIdeaFile(w, id, name)
		return
	}
	writeLaunchJSON(w, ideaDocument(dir, id, q.Get("html") == "1", !isViewer(r)))
}

// launchIdeaFile sends one attachment inside JSON: a phone speaks sealed,
// so bytes travel as base64.
func launchIdeaFile(w http.ResponseWriter, id *idea.Idea, name string) {
	path := id.FilePath(name)
	if path == "" {
		writeLaunchError(w, http.StatusNotFound, "no such file on this page")
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeLaunchJSON(w, map[string]any{"name": filepath.Base(path), "type": mimeFor(path), "data": base64.StdEncoding.EncodeToString(data)})
}

// launchIdeaNotify sets the machine-wide ring on comments and answers: auto
// true or false (on or off, as older phones send it), or status on, batch
// or off with for as the batch's interval.
func launchIdeaNotify(w http.ResponseWriter, dir string, a ideaAction) {
	want := ideaNotifyOnEach
	if a.Auto != nil && !*a.Auto {
		want = ideaNotifyOff
	}
	switch s := strings.ToLower(strings.TrimSpace(a.Status)); s {
	case "":
	case ideaNotifyOnEach, ideaNotifyBatch, ideaNotifyOff:
		want = s
	default:
		writeLaunchError(w, http.StatusBadRequest, "notify is on, batch or off")
		return
	}
	path := agentUserConfigPath(dir)
	if every := strings.TrimSpace(a.For); every != "" {
		if d, err := parseLinkFor(every); err != nil || d < time.Minute {
			writeLaunchError(w, http.StatusBadRequest, "every is a minute or more, like 5m")
			return
		}
		if err := writeUserConfigLine(path, ideaNotifyEveryLine, "ideaNotifyEvery: "+every); err != nil {
			writeLaunchError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := writeUserConfigLine(path, ideaNotifyLine, "ideaNotify: "+want); err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	mode, every := ideaNotifyMode(dir)
	writeLaunchJSON(w, map[string]any{"notify": mode != ideaNotifyOff, "mode": mode, "every": every.String()})
}

// launchIdeaNew writes a page from the phone: the agent run takes a
// minute or more, so the answer is the page once it exists.
func launchIdeaNew(w http.ResponseWriter, dir string, a ideaAction) {
	text := strings.TrimSpace(a.Text)
	if text == "" && strings.TrimSpace(a.Markdown) == "" {
		writeLaunchError(w, http.StatusBadRequest, "say what to build")
		return
	}
	if ws := strings.TrimSpace(a.Workspace); ws != "" {
		if _, err := workspaceRoot(ws); err != nil {
			writeLaunchError(w, http.StatusBadRequest, "no such workspace: "+ws)
			return
		}
	}
	id, tasks, err := newIdea(dir, ideaRequest{Text: text, Workspace: strings.TrimSpace(a.Workspace), Model: ideaModel, By: firstNonEmpty(strings.TrimSpace(a.By), "phone"), Type: a.Type, From: a.Markdown, Tasks: true}, time.Now())
	if err != nil {
		writeLaunchError(w, http.StatusBadGateway, err.Error())
		return
	}
	out := ideaDocument(dir, id, false, true)
	out["made"] = len(tasks)
	writeLaunchJSON(w, out)
}

// guestLimiter keeps a link holder from flooding a page: so many writes a
// minute from one address, and so many a minute on one page from everyone.
type guestLimiter struct {
	mu   sync.Mutex
	seen map[string][]time.Time
}

const (
	guestWritesPerAddress = 20
	guestWritesPerPage    = 60
)

var ideaGuestWrites = &guestLimiter{seen: map[string][]time.Time{}}

func (g *guestLimiter) allow(slug, addr string, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	recent := func(k string) []time.Time {
		var keep []time.Time
		for _, t := range g.seen[k] {
			if now.Sub(t) < time.Minute {
				keep = append(keep, t)
			}
		}
		g.seen[k] = keep
		return keep
	}
	byAddr, byPage := slug+"|"+addr, slug
	if len(recent(byAddr)) >= guestWritesPerAddress || len(recent(byPage)) >= guestWritesPerPage {
		return false
	}
	g.seen[byAddr] = append(g.seen[byAddr], now)
	g.seen[byPage] = append(g.seen[byPage], now)
	if len(g.seen) > 4096 {
		for k, v := range g.seen {
			if len(v) == 0 || now.Sub(v[len(v)-1]) > time.Minute {
				delete(g.seen, k)
			}
		}
	}
	return true
}

// guestAddress is who is writing, as well as a tunnel lets us know it.
func guestAddress(r *http.Request) string {
	for _, h := range []string{"CF-Connecting-IP", "X-Forwarded-For", "X-Real-IP"} {
		if v := strings.TrimSpace(strings.Split(r.Header.Get(h), ",")[0]); v != "" {
			return v
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
