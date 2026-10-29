package cmd

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils/agent/idea"
	"andriiklymiuk/corgi/utils/agent/watch"
	"github.com/russross/blackfriday/v2"
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
	// share: Off closes the link, Rotate gives it a new key.
	Off    bool `json:"off"`
	Rotate bool `json:"rotate"`
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
		"idea":     meta,
		"markdown": id.Doc,
		"comments": id.Comments.Comments,
		"tasks":    tasks,
		"tickets":  liveTickets(dir, id.Meta),
		"sent":     id.Meta.Sent,
		"send":     ideaSendOptions(dir, id.Meta),
		"plan":     plan,
		"counts":   id.Counts(),
		"open":     id.Open(),
		"forAgent": len(id.ForAgent()),
	}
	if trusted {
		out["file"] = filepath.Join(id.Dir, idea.DocName)
		out["link"] = ideaLink(dir, id.Meta, false)
		out["public"] = id.Meta.Key != "" && launcherURL() != ""
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
	return string(blackfriday.Run([]byte(md), blackfriday.WithRenderer(r),
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
		rest := strings.TrimPrefix(r.URL.Path, "/idea/")
		parts := strings.SplitN(rest, "/", 3)
		slug := parts[0]
		what, name := "", ""
		if len(parts) > 1 {
			what = parts[1]
		}
		if len(parts) > 2 {
			name = parts[2]
		}
		if slug == "" {
			// The menu of every page: a shell for any browser, the list itself
			// only for the dashboard's own (a share key opens one page, not all).
			if !strings.Contains(r.Header.Get("Accept"), "json") {
				w.Header().Set(headerContentType, "text/html; charset=utf-8")
				_, _ = fmt.Fprint(w, ideaIndexHTML)
				return
			}
			if !ideaTrusted(r, token, deviceStore) {
				writeLaunchError(w, http.StatusForbidden, "the list of pages is for the dashboard: open it from corgi agent dashboard")
				return
			}
			dir, err := agentDir()
			if err != nil {
				writeLaunchError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeLaunchJSON(w, map[string]any{"ideas": ideaRows(dir, true)})
			return
		}
		if what == "" {
			// The page is a shell; it asks for /doc with the key, and is told there when the key is wrong.
			w.Header().Set(headerContentType, "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, ideaPageHTML)
			return
		}
		dir, err := agentDir()
		if err != nil {
			writeLaunchError(w, http.StatusInternalServerError, err.Error())
			return
		}
		id, err := idea.Load(dir, slug)
		if err != nil {
			writeLaunchError(w, http.StatusNotFound, "no such idea page")
			return
		}
		trusted := ideaTrusted(r, token, deviceStore)
		if !trusted && (id.Meta.Key == "" || subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("k")), []byte(id.Meta.Key)) != 1) {
			writeLaunchError(w, http.StatusForbidden, "this page is closed - ask for a fresh link")
			return
		}
		switch what {
		case "doc":
			if r.Method != http.MethodGet {
				writeLaunchError(w, http.StatusMethodNotAllowed, "GET the page")
				return
			}
			writeLaunchJSON(w, ideaDocument(dir, id, true, trusted))
		case "rev":
			n, _ := strconv.Atoi(name)
			rv, doc, err := id.RevisionDoc(n)
			if err != nil {
				writeLaunchError(w, ideaHTTPStatus(err), err.Error())
				return
			}
			writeLaunchJSON(w, map[string]any{"revision": rv, "markdown": doc, "html": renderIdeaHTML(doc)})
		case "stream":
			ideaStream(w, r, dir, id)
		case "file":
			ideaFile(w, r, dir, id, name)
		case "comment", "reply", "status", "build", "edit", "auto", "send", "set", "share":
			if r.Method != http.MethodPost {
				writeLaunchError(w, http.StatusMethodNotAllowed, "POST to "+what)
				return
			}
			var a ideaAction
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, ideaBodyLimit)).Decode(&a); err != nil {
				writeLaunchError(w, http.StatusBadRequest, "could not read the request")
				return
			}
			a.Do, a.Slug = what, slug
			out, status, err := ideaPerform(dir, a, trusted, "someone")
			if err != nil {
				writeLaunchError(w, status, err.Error())
				return
			}
			writeLaunchJSON(w, out)
		default:
			writeLaunchError(w, http.StatusNotFound, "no such part of an idea page")
		}
	}
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
		if !(strings.HasPrefix(mime, "image/") || strings.HasPrefix(mime, "video/")) || mime == "image/svg+xml" {
			w.Header().Set("Content-Disposition", "attachment; filename=\""+filepath.Base(path)+"\"")
		}
		http.ServeFile(w, r, path)
	case http.MethodPost:
		var a ideaAction
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, ideaBodyLimit)).Decode(&a); err != nil {
			writeLaunchError(w, http.StatusBadRequest, "could not read the request")
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
func ideaPerform(dir string, a ideaAction, trusted bool, defaultBy string) (any, int, error) {
	by := strings.TrimSpace(a.By)
	if by == "" {
		by = defaultBy
	}
	if by == idea.Agent {
		by = "someone"
	}
	now := time.Now()
	switch a.Do {
	case "comment":
		var c idea.Comment
		id, err := idea.Update(dir, a.Slug, func(id *idea.Idea) error {
			anchor := anchorOrGiven(id.Doc, a.Quote, a.Prefix, a.Suffix, a.Heading)
			var err error
			c, err = id.AddComment(idea.KindComment, by, a.Text, anchor, ownFiles(id, a.Files), a.Links, now)
			return err
		})
		if err != nil {
			return nil, ideaHTTPStatus(err), err
		}
		ideaNotify(dir, id.Meta, "comment by "+by, clipTitle(a.Text, 160))
		scheduleIdeaAnswer(dir, a.Slug)
		return map[string]any{"comment": c, "open": id.Open(), "forAgent": len(id.ForAgent())}, 0, nil
	case "reply":
		var r idea.Reply
		id, err := idea.Update(dir, a.Slug, func(id *idea.Idea) error {
			var err error
			r, err = id.Reply(a.ID, by, a.Text, false, ownFiles(id, a.Files), a.Links, now)
			return err
		})
		if err != nil {
			return nil, ideaHTTPStatus(err), err
		}
		ideaNotify(dir, id.Meta, "reply by "+by, clipTitle(a.Text, 160))
		scheduleIdeaAnswer(dir, a.Slug)
		c, _ := id.Find(a.ID)
		return map[string]any{"reply": r, "comment": c, "open": id.Open(), "forAgent": len(id.ForAgent())}, 0, nil
	case "status":
		var c idea.Comment
		id, err := idea.Update(dir, a.Slug, func(id *idea.Idea) error {
			var err error
			c, err = id.SetStatus(a.ID, a.Status, now)
			return err
		})
		if err != nil {
			return nil, ideaHTTPStatus(err), err
		}
		if a.Status == idea.StatusOpen {
			scheduleIdeaAnswer(dir, a.Slug)
		}
		return map[string]any{"comment": c, "open": id.Open(), "forAgent": len(id.ForAgent())}, 0, nil
	case "file":
		data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(a.Data))
		if err != nil || len(data) == 0 {
			return nil, http.StatusBadRequest, fmt.Errorf("data is the file, base64")
		}
		id, err := idea.Load(dir, a.Slug)
		if err != nil {
			return nil, http.StatusNotFound, err
		}
		att, err := id.AddFile(a.Name, data, firstNonEmpty(a.Type, mimeFor(a.Name)))
		if err != nil {
			return nil, http.StatusBadRequest, err
		}
		return map[string]any{"file": att}, 0, nil
	case "share":
		if !trusted {
			return nil, http.StatusForbidden, fmt.Errorf("only the page's owner opens or closes its link")
		}
		if _, err := idea.Load(dir, a.Slug); err != nil {
			return nil, ideaHTTPStatus(err), err
		}
		res, err := shareIdea(dir, shareRequest{Target: a.Slug, By: by, Off: a.Off, Rotate: a.Rotate}, now)
		if err != nil {
			return nil, ideaHTTPStatus(err), err
		}
		return res, 0, nil
	case "send":
		if !trusted {
			return nil, http.StatusForbidden, fmt.Errorf("sending needs the dashboard: open the page from corgi agent dashboard, or run corgi idea send %s", a.Slug)
		}
		links, err := sendIdea(dir, a.Slug, a.To, a.Workspace, a.Space)
		if err != nil {
			return nil, ideaHTTPStatus(err), err
		}
		return map[string]any{"sent": links}, 0, nil
	case "build":
		if !trusted {
			return nil, http.StatusForbidden, fmt.Errorf("building needs the dashboard: open the page from corgi agent dashboard, or run corgi idea build %s", a.Slug)
		}
		run := a.Run == nil || *a.Run
		res, err := buildIdea(dir, a.Slug, a.Workspace, "", a.Slots, run)
		if err != nil {
			return nil, ideaHTTPStatus(err), err
		}
		return res, 0, nil
	case "edit":
		if !trusted {
			return nil, http.StatusForbidden, fmt.Errorf("only the dashboard edits the page itself; comment instead")
		}
		id, err := idea.Update(dir, a.Slug, func(id *idea.Idea) error {
			id.ChangeBy, id.ChangeNote = by, "edited"
			return id.SetDoc(a.Markdown, now)
		})
		if err != nil {
			return nil, ideaHTTPStatus(err), err
		}
		return ideaDocument(dir, id, true, true), 0, nil
	case "set":
		if !trusted {
			return nil, http.StatusForbidden, fmt.Errorf("only the dashboard changes that")
		}
		ch := ideaChanges{Name: a.Name, Summary: a.Summary, Workspace: a.Workspace, Type: a.Type}
		if a.Auto != nil {
			ch.Auto = map[bool]string{true: "on", false: "off"}[*a.Auto]
		}
		if a.Tags != nil {
			ch.Tags = &a.Tags
		}
		id, err := setIdea(dir, a.Slug, ch)
		if err != nil {
			return nil, ideaHTTPStatus(err), err
		}
		return map[string]any{"idea": id.Meta}, 0, nil
	case "auto":
		if !trusted {
			return nil, http.StatusForbidden, fmt.Errorf("only the dashboard sets that")
		}
		id, err := idea.Update(dir, a.Slug, func(id *idea.Idea) error {
			if a.Auto != nil {
				id.Meta.Auto = *a.Auto
			}
			return nil
		})
		if err != nil {
			return nil, ideaHTTPStatus(err), err
		}
		if id.Meta.Auto {
			scheduleIdeaAnswer(dir, a.Slug)
		}
		return map[string]any{"auto": id.Meta.Auto}, 0, nil
	}
	return nil, http.StatusBadRequest, fmt.Errorf("do is comment, reply, status, file, build, edit, auto, send, set or share")
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
	writeLaunchJSON(w, map[string]any{"ideas": ideaRows(dir, !isViewer(r)), "notify": ideaNotifyOn(dir)})
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
			writeLaunchError(w, http.StatusBadRequest, "could not read the request")
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

// launchIdeaNotify turns the machine-wide ring on comments and answers on or off.
func launchIdeaNotify(w http.ResponseWriter, dir string, a ideaAction) {
	want := "on"
	if a.Auto != nil && !*a.Auto {
		want = "off"
	}
	if err := writeUserConfigLine(agentUserConfigPath(dir), ideaNotifyLine, ideaNotifyKey+": "+want); err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeLaunchJSON(w, map[string]any{"notify": want == "on"})
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
