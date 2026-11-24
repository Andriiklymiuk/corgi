package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/handoff"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/transcript"
	"andriiklymiuk/corgi/utils/agent/watch"
	"andriiklymiuk/corgi/utils/agent/workspace"
	"gopkg.in/yaml.v3"
)

const (
	reviewListFresh   = 45 * time.Second
	reviewListKeep    = 20 * time.Minute
	reviewStatusFresh = 2 * time.Minute
	reviewChangeFresh = 90 * time.Second
	reviewStatusAsks  = 30
	reviewAskers      = 12
)

type reviewChange struct {
	watch.Change
	Mine    bool           `json:"mine"`
	Checks  string         `json:"checks,omitempty"`
	Review  string         `json:"review,omitempty"`
	Ready   bool           `json:"ready"`
	Session string         `json:"session,omitempty"`
	Live    bool           `json:"live"`
	Proof   *reviewProof   `json:"proof,omitempty"`
	Notes   []string       `json:"notes,omitempty"`
	Unsure  []string       `json:"unsure,omitempty"`
	Why     map[string]why `json:"why,omitempty"`
	At      time.Time      `json:"at"`
}

type reviewProof struct {
	Cmd    string    `json:"cmd"`
	Passed bool      `json:"passed"`
	At     string    `json:"at,omitempty"`
	RanAt  time.Time `json:"ranAt,omitzero"`
	Newest bool      `json:"newest"`
}

type why struct {
	Text string    `json:"text"`
	At   time.Time `json:"at,omitzero"`
}

type reviewStore struct {
	mu         sync.Mutex
	rows       []watch.ReviewRow
	rowsAt     time.Time
	problem    string
	refreshing bool
	status     map[string]watch.PullStatus
	changes    map[string]reviewChange
}

var reviewCache = &reviewStore{status: map[string]watch.PullStatus{}, changes: map[string]reviewChange{}}

var forgeFor = func(dir, ws string) watch.Forge { return watch.NewForge(watch.LoadSecretsFor(dir, ws)) }

var pullAskerFor = func(dir, ws, link string) watch.PullAsker {
	secrets := watch.LoadSecretsFor(dir, ws)
	if strings.Contains(link, "github.com/") {
		return watch.NewGitHub(secrets, nil)
	}
	return watch.NewGitLab(secrets)
}

type reviewSeat struct {
	id    string
	repos []string
}

var workspaceProjects = func(ws workspace.Workspace) []string {
	var out []string
	seen := map[string]bool{}
	add := func(dir string) {
		root, ok := utils.RepoRootOf(dir)
		if !ok || seen[root] {
			return
		}
		seen[root] = true
		if project := utils.RepoProject(root); project != "" {
			out = append(out, project)
		}
	}
	add(ws.AbsPath)
	data, err := os.ReadFile(filepath.Join(ws.AbsPath, composeFileName(ws.AbsPath)))
	if err != nil {
		return out
	}
	var doc struct {
		Services map[string]struct {
			Path string `yaml:"path"`
		} `yaml:"services"`
	}
	if yaml.Unmarshal(data, &doc) != nil {
		return out
	}
	names := make([]string, 0, len(doc.Services))
	for name := range doc.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		path := doc.Services[name].Path
		if path == "" {
			continue
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(ws.AbsPath, path)
		}
		add(path)
	}
	return out
}

func reviewSeats(dir string) map[string][]reviewSeat {
	byToken := map[string][]reviewSeat{}
	user := loadUserConfigQuietly()
	var ids []string
	if user != nil {
		for id, ws := range user.Workspaces {
			if ws.Watch != nil && ws.Watch.Enabled {
				ids = append(ids, id)
			}
		}
	}
	sort.Strings(ids)
	paths := map[string]workspace.Workspace{}
	if registry, err := workspace.Load(agentRegistryPath(dir)); err == nil {
		for _, ws := range registry.Workspaces {
			paths[ws.ID] = ws
		}
	}
	for _, id := range ids {
		s := watch.LoadSecretsFor(dir, id)
		key := s.GitHub + "\x00" + s.GitLab + "\x00" + s.GitLabURL
		repos := user.Workspaces[id].Watch.Repos
		if ws, ok := paths[id]; ok && len(repos) == 0 {
			repos = workspaceProjects(ws)
		}
		byToken[key] = append(byToken[key], reviewSeat{id: id, repos: repos})
	}
	if len(byToken) == 0 {
		byToken[""] = []reviewSeat{{}}
	}
	return byToken
}

func seatFor(seats []reviewSeat, repo string) (string, bool) {
	for _, s := range seats {
		for _, r := range s.repos {
			if strings.EqualFold(r, repo) {
				return s.id, true
			}
		}
	}
	for _, s := range seats {
		if len(s.repos) == 0 {
			return s.id, true
		}
	}
	return "", false
}

func reviewRows(ctx context.Context, dir string, fresh bool) ([]watch.ReviewRow, string) {
	c := reviewCache
	c.mu.Lock()
	age := time.Since(c.rowsAt)
	if !fresh && !c.rowsAt.IsZero() && age < reviewListKeep {
		rows, problem := append([]watch.ReviewRow(nil), c.rows...), c.problem
		again := age >= reviewListFresh && !c.refreshing
		if again {
			c.refreshing = true
		}
		c.mu.Unlock()
		if again {
			go func() {
				later, cancel := context.WithTimeout(context.Background(), 40*time.Second)
				defer cancel()
				readReviewRows(later, dir)
			}()
		}
		return rows, problem
	}
	c.mu.Unlock()
	return readReviewRows(ctx, dir)
}

func readReviewRows(ctx context.Context, dir string) ([]watch.ReviewRow, string) {
	c := reviewCache
	now := time.Now()
	type found struct {
		seats []reviewSeat
		rows  []watch.ReviewRow
		err   error
	}
	var lists []*found
	var wg sync.WaitGroup
	for _, seats := range reviewSeats(dir) {
		list := &found{seats: seats}
		lists = append(lists, list)
		wg.Add(1)
		go func() {
			defer wg.Done()
			list.rows, list.err = forgeFor(dir, seats[0].id).ListReviews(ctx, now)
		}()
	}
	wg.Wait()
	sort.Slice(lists, func(i, j int) bool { return lists[i].seats[0].id < lists[j].seats[0].id })
	var rows []watch.ReviewRow
	problem := ""
	seen := map[string]bool{}
	for _, list := range lists {
		if list.err != nil && problem == "" {
			problem = firstLineOf(list.err.Error())
		}
		for _, row := range list.rows {
			if seen[row.Ref] {
				continue
			}
			seat, ok := seatFor(list.seats, row.Repo)
			if !ok {
				continue
			}
			seen[row.Ref] = true
			row.Workspace = seat
			rows = append(rows, row)
		}
	}
	fillReviewStatus(ctx, dir, rows, now)
	sessionsByPull := liveSessionsByPull(dir)
	for i := range rows {
		if s := sessionsByPull[rows[i].Link]; s != nil {
			rows[i].Session = firstNonEmpty(s.Display, s.Label)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Updated.After(rows[j].Updated) })
	c.mu.Lock()
	c.rows, c.rowsAt, c.problem, c.refreshing = rows, time.Now(), problem, false
	c.mu.Unlock()
	return append([]watch.ReviewRow(nil), rows...), problem
}

func fillReviewStatus(ctx context.Context, dir string, rows []watch.ReviewRow, now time.Time) {
	log := watch.LoadPullLog(dir)
	sem := make(chan struct{}, reviewAskers)
	var wg sync.WaitGroup
	asked := 0
	for i := range rows {
		row := &rows[i]
		if row.State == "merged" {
			row.Place(watch.PullStatus{State: "merged"}, now)
			continue
		}
		if st, ok := freshReviewStatus(log, row.Ref, now); ok {
			row.Place(st, now)
			continue
		}
		if row.Group == watch.GroupOld || row.Group == watch.GroupAnswered || asked >= reviewStatusAsks {
			continue
		}
		asked++
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			st, ok := pullAskerFor(dir, row.Workspace, row.Link).PullStatus(ctx, row.Ref)
			if !ok {
				return
			}
			reviewCache.mu.Lock()
			reviewCache.status[row.Ref] = st
			reviewCache.mu.Unlock()
			row.Place(st, now)
		}()
	}
	wg.Wait()
}

func freshReviewStatus(log *watch.PullLog, ref string, now time.Time) (watch.PullStatus, bool) {
	reviewCache.mu.Lock()
	st, ok := reviewCache.status[ref]
	reviewCache.mu.Unlock()
	if ok && now.Sub(st.At) < reviewStatusFresh {
		return st, true
	}
	if st, ok := log.Get(ref); ok && now.Sub(st.At) < reviewStatusFresh {
		return st, true
	}
	return watch.PullStatus{}, false
}

func liveSessionsByPull(dir string) map[string]*sessions.Session {
	out := map[string]*sessions.Session{}
	rep, err := readBoard(dir)
	if err != nil {
		return out
	}
	for i := range rep.Sessions {
		s := &rep.Sessions[i]
		if s.PR == "" || s.Status == sessions.StatusGone || s.Status == sessions.StatusStale {
			continue
		}
		out[watch.PullLink(s.PR)] = s
	}
	return out
}

func authorSession(dir, link, branch string) (sessions.Session, bool, bool) {
	rep, err := readBoard(dir)
	if err != nil {
		return sessions.Session{}, false, false
	}
	match := func(s sessions.Session) bool {
		return (s.PR != "" && watch.PullLink(s.PR) == link) || (branch != "" && s.Branch == branch)
	}
	for _, s := range rep.Sessions {
		if match(s) {
			return s, true, s.Status != sessions.StatusGone && s.Status != sessions.StatusStale
		}
	}
	for _, s := range rep.Ended {
		if match(s) {
			return s, true, false
		}
	}
	return sessions.Session{}, false, false
}

func reviewRowFor(ctx context.Context, dir, link string) (watch.ReviewRow, bool) {
	link = watch.PullLink(link)
	rows, _ := reviewRows(ctx, dir, false)
	for _, r := range rows {
		if r.Link == link {
			return r, true
		}
	}
	return watch.ReviewRow{}, false
}

func whyByFile(session sessions.Session, files []watch.ChangeFile) map[string]why {
	path := transcriptPathFor(session)
	steps, err := transcriptSteps(path, 0)
	if err != nil || len(steps) == 0 {
		return nil
	}
	out := map[string]why{}
	for _, f := range files {
		if !f.Eyes {
			continue
		}
		for i := len(steps) - 1; i >= 0; i-- {
			if steps[i].Text == "" || !touches(steps[i], f.Path) {
				continue
			}
			out[f.Path] = why{Text: steps[i].Text, At: steps[i].At}
			break
		}
	}
	return out
}

func touches(step transcript.Step, path string) bool {
	for _, f := range step.Files {
		if f == path || strings.HasSuffix(f, "/"+path) {
			return true
		}
	}
	return false
}

func handoffForBranch(dir, branch string) (handoff.Packet, bool) {
	if branch == "" {
		return handoff.Packet{}, false
	}
	registry, err := workspace.Load(agentRegistryPath(dir))
	if err != nil {
		return handoff.Packet{}, false
	}
	for _, ws := range registry.Workspaces {
		for _, p := range handoff.List(ws.AbsPath) {
			if p.Where.Branch == branch {
				return p, true
			}
		}
	}
	return handoff.Packet{}, false
}

func reviewChangeFor(ctx context.Context, dir, link string, fresh bool) (reviewChange, error) {
	link = watch.PullLink(link)
	c := reviewCache
	c.mu.Lock()
	cached, ok := c.changes[link]
	c.mu.Unlock()
	if ok && !fresh && time.Since(cached.At) < reviewChangeFresh {
		return cached, nil
	}
	row, known := reviewRowFor(ctx, dir, link)
	change, err := forgeFor(dir, row.Workspace).PullChange(ctx, link)
	if err != nil {
		return reviewChange{}, err
	}
	out := reviewChange{Change: change, Mine: row.Mine, At: time.Now()}
	if !known {
		out.Mine = false
	}
	if st, ok := pullAskerFor(dir, row.Workspace, link).PullStatus(ctx, change.Ref); ok {
		out.Checks, out.Review, out.Ready = st.Checks, st.Review, st.Ready()
		c.mu.Lock()
		c.status[change.Ref] = st
		c.mu.Unlock()
	}
	if session, found, live := authorSession(dir, link, change.Branch); found {
		out.Session, out.Live = firstNonEmpty(session.Display, session.Label, session.ID), live
		out.Why = whyByFile(session, change.Files)
	}
	if p, ok := handoffForBranch(dir, change.Branch); ok {
		out.Notes, out.Unsure = p.Decisions, p.Uncertain
		if v := p.Verification; v != nil && v.Cmd != "" {
			out.Proof = &reviewProof{Cmd: v.Cmd, Passed: v.Exit == 0, At: v.At, RanAt: v.RanAt,
				Newest: v.At != "" && change.Head != "" && (strings.HasPrefix(change.Head, v.At) || strings.HasPrefix(v.At, change.Head))}
		}
	}
	c.mu.Lock()
	c.changes[link] = out
	c.mu.Unlock()
	return out, nil
}

func forgetReview(link string) {
	c := reviewCache
	c.mu.Lock()
	delete(c.changes, watch.PullLink(link))
	delete(c.status, watch.PullRef(link))
	c.rowsAt = time.Time{}
	c.mu.Unlock()
}

func launchReviewsChangeHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodGet {
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET ?link=")
		return
	}
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	link := strings.TrimSpace(r.URL.Query().Get("link"))
	if watch.PullRef(link) == "" {
		writeLaunchError(w, http.StatusBadRequest, "link is a pull request or a merge request")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	change, err := reviewChangeFor(ctx, dir, link, r.URL.Query().Get("fresh") != "")
	if err != nil {
		writeLaunchError(w, http.StatusBadGateway, firstLineOf(err.Error()))
		return
	}
	writeLaunchJSON(w, change)
}

type reviewRequest struct {
	Link    string             `json:"link"`
	Head    string             `json:"head"`
	Do      string             `json:"do"`
	Verdict string             `json:"verdict"`
	Body    string             `json:"body"`
	Notes   []watch.ReviewNote `json:"notes"`
}

func launchReviewsHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	switch r.Method {
	case http.MethodGet:
		ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
		defer cancel()
		rows, problem := reviewRows(ctx, dir, r.URL.Query().Get("fresh") != "")
		if rows == nil {
			rows = []watch.ReviewRow{}
		}
		out := map[string]any{"reviews": rows, "at": time.Now()}
		if problem != "" {
			out["problem"] = problem
		}
		if device := deviceOf(r); device != "" {
			out["phone"] = phoneReviewsAllowed(dir, device)
		}
		writeLaunchJSON(w, out)
	case http.MethodPost:
		var req reviewRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req); err != nil {
			writeLaunchError(w, http.StatusBadRequest, requestUnreadableMessage)
			return
		}
		reviewAction(w, r, dir, req)
	default:
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET the list, POST {link, do: send|fix|merge}")
	}
}

func reviewAction(w http.ResponseWriter, r *http.Request, dir string, req reviewRequest) {
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	out, status, why := runReviewAction(ctx, dir, req, launchFrom(r))
	if why != "" {
		writeLaunchError(w, status, why)
		return
	}
	writeLaunchJSON(w, out)
}

func runReviewAction(ctx context.Context, dir string, req reviewRequest, from string) (map[string]any, int, string) {
	link := watch.PullLink(strings.TrimSpace(req.Link))
	row, known := reviewRowFor(ctx, dir, link)
	if !known {
		return nil, http.StatusNotFound, "not a pull request of yours or one you were asked to review"
	}
	switch strings.TrimSpace(req.Do) {
	case "send":
		verdict := firstNonEmpty(strings.TrimSpace(req.Verdict), watch.ReviewComment)
		if row.Mine && verdict != watch.ReviewComment {
			return nil, http.StatusBadRequest, "you cannot approve or block your own pull request"
		}
		if head := strings.TrimSpace(req.Head); head != "" {
			now, err := reviewChangeFor(ctx, dir, link, true)
			if err != nil {
				return nil, http.StatusBadGateway, firstLineOf(err.Error())
			}
			if now.Head != "" && now.Head != head {
				return nil, http.StatusConflict, "the pull request got new commits since you read it; read it again, then send"
			}
		}
		if err := forgeFor(dir, row.Workspace).SendReview(ctx, link, verdict, req.Body, req.Notes); err != nil {
			return nil, http.StatusBadGateway, firstLineOf(err.Error())
		}
		markInboxDone(dir, link, from)
		forgetReview(link)
		return map[string]any{"done": map[string]string{"approve": "approved", "request": "changes requested", "comment": "commented"}[verdict], "url": link}, http.StatusOK, ""
	case "fix":
		return reviewFix(dir, row, req, from)
	case "merge":
		if !row.Mine {
			return nil, http.StatusBadRequest, "not a pull request of yours"
		}
		if err := watch.MergePR(ctx, watch.LoadSecretsFor(dir, row.Workspace), link); err != nil {
			return nil, http.StatusBadGateway, firstLineOf(err.Error())
		}
		forgetReview(link)
		if info, err := daemon.ReadInfo(dir); err == nil && info != nil {
			daemon.Nudge(info)
		}
		return map[string]any{"done": "merged", "url": link}, http.StatusOK, ""
	}
	return nil, http.StatusBadRequest, "do is send, fix or merge"
}

func reviewFix(dir string, row watch.ReviewRow, req reviewRequest, from string) (map[string]any, int, string) {
	if !row.Mine {
		return nil, http.StatusBadRequest, "fix is for a pull request of yours; send posts the notes on someone else's"
	}
	text := watch.FixMessage(row.Link, req.Notes, req.Body)
	if text == "" {
		return nil, http.StatusBadRequest, "say what to change"
	}
	target := liveSessionsByPull(dir)[row.Link]
	if target == nil {
		return nil, http.StatusNotFound, "no live session is on this pull request"
	}
	info, err := daemon.ReadInfo(dir)
	if err != nil || info == nil {
		return nil, http.StatusServiceUnavailable, "the corgi daemon is not running on that machine"
	}
	forgetReview(row.Link)
	if _, err := command.Write(dir, command.Command{Action: command.ActionSend, SessionID: target.ID, Text: text, Enter: true, Source: from}); err != nil {
		return nil, http.StatusInternalServerError, err.Error()
	}
	daemon.Nudge(info)
	return map[string]any{"ok": true, "action": command.ActionSend}, http.StatusOK, ""
}

func launchFrom(r *http.Request) string {
	return firstNonEmptyString(r.URL.Query().Get("from"), "phone")
}
