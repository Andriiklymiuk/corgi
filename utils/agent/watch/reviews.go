package watch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	GroupReady    = "ready"
	GroupOpen     = "open"
	GroupDraft    = "draft"
	GroupMerged   = "merged"
	GroupToReview = "toReview"
	GroupAnswered = "answered"
	GroupOld      = "old"

	reviewOldDays   = 60
	changeFilePages = 3
	changeBodyCap   = 12000
)

type ReviewRow struct {
	Ref       string    `json:"ref"`
	Link      string    `json:"link"`
	Title     string    `json:"title"`
	Author    string    `json:"author,omitempty"`
	Repo      string    `json:"repo"`
	State     string    `json:"state"`
	Mine      bool      `json:"mine"`
	Answered  bool      `json:"answered,omitempty"`
	Group     string    `json:"group"`
	Checks    string    `json:"checks,omitempty"`
	Review    string    `json:"review,omitempty"`
	Workspace string    `json:"workspace,omitempty"`
	Session   string    `json:"session,omitempty"`
	Updated   time.Time `json:"updated,omitzero"`
}

func (r *ReviewRow) Place(p PullStatus, now time.Time) {
	r.Checks, r.Review = p.Checks, p.Review
	if p.State != "" {
		r.State = p.State
	}
	switch {
	case !r.Mine && r.Answered:
		r.Group = GroupAnswered
	case !r.Mine:
		r.Group = GroupToReview
	case r.State == "merged":
		r.Group = GroupMerged
	case !r.Updated.IsZero() && now.Sub(r.Updated) > reviewOldDays*24*time.Hour:
		r.Group = GroupOld
	case r.State == "draft":
		r.Group = GroupDraft
	case p.Ready():
		r.Group = GroupReady
	default:
		r.Group = GroupOpen
	}
}

type ReviewNote struct {
	Path string `json:"path"`
	Was  string `json:"was,omitempty"`
	Line int    `json:"line"`
	Side string `json:"side,omitempty"`
	Body string `json:"body"`
}

type Forge struct {
	HTTP      *http.Client
	GitHubAPI string
	Secrets   Secrets
}

func NewForge(s Secrets) Forge {
	if strings.TrimSpace(s.GitHub) == "" {
		s.GitHub = strings.TrimSpace(githubAuthToken())
	}
	return Forge{HTTP: &http.Client{Timeout: 30 * time.Second}, GitHubAPI: "https://api.github.com", Secrets: s}
}

func (f Forge) gitlabAPI() string {
	base := strings.TrimRight(strings.TrimSpace(f.Secrets.GitLabURL), "/")
	if base == "" {
		base = "https://gitlab.com"
	}
	return base + "/api/v4"
}

func (f Forge) call(ctx context.Context, method, endpoint string, payload, out any) error {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	if strings.HasPrefix(endpoint, f.GitHubAPI) {
		req.Header.Set("Authorization", bearerPrefix+f.Secrets.GitHub)
		req.Header.Set("Accept", githubJSONAccept)
	} else {
		req.Header.Set(gitlabTokenHeader, f.Secrets.GitLab)
	}
	if payload != nil {
		req.Header.Set(headerContentType, jsonContentType)
	}
	resp, err := f.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", req.URL.Host, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s: HTTP %d: %s", req.URL.Host, resp.StatusCode, clip(string(raw), bodyMax))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

type githubSearchItem struct {
	Title         string    `json:"title"`
	HTMLURL       string    `json:"html_url"`
	Draft         bool      `json:"draft"`
	State         string    `json:"state"`
	UpdatedAt     time.Time `json:"updated_at"`
	RepositoryURL string    `json:"repository_url"`
	User          struct {
		Login string `json:"login"`
	} `json:"user"`
	PullRequest struct {
		MergedAt *time.Time `json:"merged_at"`
	} `json:"pull_request"`
}

type gitlabListMR struct {
	Title     string    `json:"title"`
	WebURL    string    `json:"web_url"`
	Draft     bool      `json:"draft"`
	State     string    `json:"state"`
	UpdatedAt time.Time `json:"updated_at"`
	Author    struct {
		Username string `json:"username"`
	} `json:"author"`
}

func (f Forge) ListReviews(ctx context.Context, now time.Time) ([]ReviewRow, error) {
	var hub, lab []ReviewRow
	var hubErr, labErr error
	var wg sync.WaitGroup
	if f.Secrets.GitHub != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hub, hubErr = f.githubReviews(ctx)
		}()
	}
	if f.Secrets.GitLab != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lab, labErr = f.gitlabReviews(ctx)
		}()
	}
	wg.Wait()
	rows := append(hub, lab...)
	firstErr := hubErr
	if firstErr == nil {
		firstErr = labErr
	}
	seen := map[string]bool{}
	out := rows[:0]
	for _, r := range rows {
		if r.Ref == "" || seen[r.Ref] {
			continue
		}
		seen[r.Ref] = true
		r.Place(PullStatus{}, now)
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

func (f Forge) githubReviews(ctx context.Context) ([]ReviewRow, error) {
	queries := []struct {
		q    string
		mine bool
	}{
		{"is:pr is:open author:@me archived:false", true},
		{"is:pr is:open review-requested:@me archived:false", false},
	}
	pages := make([][]githubSearchItem, len(queries))
	errs := make([]error, len(queries))
	var wg sync.WaitGroup
	for i, query := range queries {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var page struct {
				Items []githubSearchItem `json:"items"`
			}
			endpoint := f.GitHubAPI + "/search/issues?per_page=50&sort=updated&order=desc&q=" + url.QueryEscape(query.q)
			errs[i] = f.call(ctx, http.MethodGet, endpoint, nil, &page)
			pages[i] = page.Items
		}()
	}
	wg.Wait()
	var rows []ReviewRow
	for i, query := range queries {
		for _, it := range pages[i] {
			link := PullLink(it.HTMLURL)
			row := ReviewRow{Ref: PullRef(link), Link: link, Title: it.Title, Author: it.User.Login, Mine: query.mine, Updated: it.UpdatedAt, State: "open"}
			row.Repo, _, _ = strings.Cut(row.Ref, "#")
			switch {
			case it.PullRequest.MergedAt != nil:
				row.State = "merged"
			case it.Draft:
				row.State = "draft"
			}
			rows = append(rows, row)
		}
	}
	return rows, firstError(errs)
}

func firstError(errs []error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func (f Forge) gitlabReviews(ctx context.Context) ([]ReviewRow, error) {
	api := f.gitlabAPI()
	var me struct {
		Username string `json:"username"`
	}
	if err := f.call(ctx, http.MethodGet, api+"/user", nil, &me); err != nil {
		return nil, err
	}
	queries := []struct {
		q    string
		mine bool
	}{
		{"scope=created_by_me&state=opened", true},
		{"scope=all&state=opened&reviewer_username=" + url.QueryEscape(me.Username), false},
	}
	approvedQuery := "per_page=100&scope=all&state=opened&reviewer_username=" + url.QueryEscape(me.Username) + "&approved_by_usernames[]=" + url.QueryEscape(me.Username)
	pages := make([][]gitlabListMR, len(queries)+1)
	errs := make([]error, len(queries))
	var wg sync.WaitGroup
	for i := range pages {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i == len(queries) {
				_ = f.call(ctx, http.MethodGet, api+"/merge_requests?"+approvedQuery, nil, &pages[i])
				return
			}
			errs[i] = f.call(ctx, http.MethodGet, api+"/merge_requests?per_page=50&order_by=updated_at&"+queries[i].q, nil, &pages[i])
		}()
	}
	wg.Wait()
	answered := map[string]bool{}
	for _, mr := range pages[len(queries)] {
		answered[PullLink(mr.WebURL)] = true
	}
	var rows []ReviewRow
	for i, query := range queries {
		for _, mr := range pages[i] {
			link := PullLink(mr.WebURL)
			row := ReviewRow{Ref: PullRef(link), Link: link, Title: mr.Title, Author: mr.Author.Username, Mine: query.mine, Updated: mr.UpdatedAt, State: gitlabMRState(mr.State, mr.Draft), Answered: !query.mine && answered[link]}
			row.Repo, _, _ = strings.Cut(row.Ref, "!")
			rows = append(rows, row)
		}
	}
	return rows, firstError(errs)
}

func (f Forge) PullChange(ctx context.Context, link string) (Change, error) {
	link = PullLink(link)
	var c Change
	var err error
	switch {
	case githubPull.MatchString(link):
		c, err = f.githubChange(ctx, link)
	case gitlabMR.MatchString(link):
		c, err = f.gitlabChange(ctx, link)
	default:
		return Change{}, fmt.Errorf("%s is not a GitHub pull request or a GitLab merge request", link)
	}
	if err != nil {
		return Change{}, err
	}
	c.Ref, c.Link = PullRef(link), link
	c.Sort()
	c.settle()
	return c, nil
}

func (f Forge) githubChange(ctx context.Context, link string) (Change, error) {
	m := githubPRPath.FindStringSubmatch(link)
	if m == nil {
		return Change{}, fmt.Errorf("cannot read a repo and number out of %s", link)
	}
	if f.Secrets.GitHub == "" {
		return Change{}, ErrNoToken
	}
	base := f.GitHubAPI + reposSegment + m[1] + pullsSegment + m[2]
	var pr struct {
		Title          string    `json:"title"`
		Body           string    `json:"body"`
		State          string    `json:"state"`
		Merged         bool      `json:"merged"`
		Draft          bool      `json:"draft"`
		Commits        int       `json:"commits"`
		MergeableState string    `json:"mergeable_state"`
		CreatedAt      time.Time `json:"created_at"`
		MergedAt       time.Time `json:"merged_at"`
		User           struct {
			Login string `json:"login"`
		} `json:"user"`
		MergedBy struct {
			Login string `json:"login"`
		} `json:"merged_by"`
		RequestedReviewers []struct {
			Login string `json:"login"`
		} `json:"requested_reviewers"`
		Head struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
	}
	if err := f.call(ctx, http.MethodGet, base, nil, &pr); err != nil {
		return Change{}, err
	}
	c := Change{Title: pr.Title, Body: clip(pr.Body, changeBodyCap), Author: pr.User.Login, Branch: pr.Head.Ref, Base: pr.Base.Ref, Head: pr.Head.SHA, State: pr.State,
		Opened: pr.CreatedAt, Commits: pr.Commits, Behind: pr.MergeableState == "behind", Conflicts: pr.MergeableState == "dirty"}
	switch {
	case pr.Merged:
		c.State = "merged"
	case pr.Draft && pr.State == "open":
		c.State = "draft"
	}
	c.Activity = append(c.Activity, ChangeEvent{Kind: "opened", Who: pr.User.Login, At: pr.CreatedAt})
	if pr.Merged {
		c.Activity = append(c.Activity, ChangeEvent{Kind: "merged", Who: pr.MergedBy.Login, At: pr.MergedAt})
	}
	var reviews []struct {
		State       string    `json:"state"`
		Body        string    `json:"body"`
		SubmittedAt time.Time `json:"submitted_at"`
		User        struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	var commits []struct {
		SHA    string `json:"sha"`
		Commit struct {
			Message string `json:"message"`
			Author  struct {
				Name string    `json:"name"`
				Date time.Time `json:"date"`
			} `json:"author"`
		} `json:"commit"`
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
	}
	var runs struct {
		CheckRuns []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
		} `json:"check_runs"`
	}
	var wg sync.WaitGroup
	side := func(endpoint string, out any) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = f.call(ctx, http.MethodGet, endpoint, nil, out)
		}()
	}
	side(base+"/reviews?per_page=100", &reviews)
	side(base+"/commits?per_page=100", &commits)
	if pr.Head.SHA != "" {
		side(f.GitHubAPI+reposSegment+m[1]+commitsSegment+pr.Head.SHA+"/check-runs?per_page=100", &runs)
	}
	wg.Wait()
	people := map[string]string{}
	var order []string
	note := func(name, state string) {
		if name == "" || strings.EqualFold(name, pr.User.Login) {
			return
		}
		if _, ok := people[name]; !ok {
			order = append(order, name)
		}
		if state == PersonCommented && people[name] != "" && people[name] != PersonWaiting {
			return
		}
		people[name] = state
	}
	for _, r := range reviews {
		state := map[string]string{"APPROVED": PersonApproved, "CHANGES_REQUESTED": PersonChanges, "COMMENTED": PersonCommented, "DISMISSED": PersonWaiting}[r.State]
		if state == "" {
			continue
		}
		note(r.User.Login, state)
		c.Activity = append(c.Activity, ChangeEvent{Kind: "review", Who: r.User.Login, Text: map[string]string{PersonApproved: "approved", PersonChanges: "asked for changes", PersonCommented: "commented", PersonWaiting: "review dismissed"}[state] + reviewWords(r.Body), At: r.SubmittedAt})
	}
	for _, r := range pr.RequestedReviewers {
		note(r.Login, PersonWaiting)
	}
	for _, name := range order {
		c.Reviewers = append(c.Reviewers, ChangePerson{Name: name, State: people[name]})
	}
	for _, k := range commits {
		short := k.SHA
		if len(short) > 7 {
			short = short[:7]
		}
		c.Activity = append(c.Activity, ChangeEvent{Kind: "commit", Who: firstSet(k.Author.Login, k.Commit.Author.Name), Text: short + " " + firstLineOfText(k.Commit.Message), At: k.Commit.Author.Date})
	}
	seen := map[string]bool{}
	for _, r := range runs.CheckRuns {
		if seen[r.Name] {
			continue
		}
		seen[r.Name] = true
		c.CheckRuns = append(c.CheckRuns, ChangeCheck{Name: r.Name, State: checkState(r.Status, r.Conclusion)})
	}
	for page := 1; page <= changeFilePages; page++ {
		var files []struct {
			Filename         string `json:"filename"`
			PreviousFilename string `json:"previous_filename"`
			Status           string `json:"status"`
			Additions        int    `json:"additions"`
			Deletions        int    `json:"deletions"`
			Patch            string `json:"patch"`
		}
		if err := f.call(ctx, http.MethodGet, fmt.Sprintf("%s/files?per_page=100&page=%d", base, page), nil, &files); err != nil {
			return Change{}, err
		}
		for _, gf := range files {
			file := ChangeFile{Path: gf.Filename, Was: gf.PreviousFilename, Status: gf.Status, Adds: gf.Additions, Dels: gf.Deletions}
			file.Hunks, file.Cut = ParsePatch(gf.Patch)
			if gf.Patch == "" && gf.Additions+gf.Deletions > 0 {
				file.Cut = true
			}
			c.Files = append(c.Files, file)
		}
		if len(files) < 100 {
			break
		}
	}
	return c, nil
}

type gitlabMRDetail struct {
	CreatedAt           time.Time `json:"created_at"`
	MergedAt            time.Time `json:"merged_at"`
	DetailedMergeStatus string    `json:"detailed_merge_status"`
	HasConflicts        bool      `json:"has_conflicts"`
	MergeUser           struct {
		Username string `json:"username"`
	} `json:"merge_user"`
	Reviewers []struct {
		Username string `json:"username"`
	} `json:"reviewers"`
	HeadPipeline struct {
		Status string `json:"status"`
	} `json:"head_pipeline"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	State        string `json:"state"`
	Draft        bool   `json:"draft"`
	SourceBranch string `json:"source_branch"`
	TargetBranch string `json:"target_branch"`
	Author       struct {
		Username string `json:"username"`
	} `json:"author"`
	DiffRefs struct {
		BaseSHA  string `json:"base_sha"`
		HeadSHA  string `json:"head_sha"`
		StartSHA string `json:"start_sha"`
	} `json:"diff_refs"`
}

func (f Forge) gitlabChange(ctx context.Context, link string) (Change, error) {
	if f.Secrets.GitLab == "" {
		return Change{}, ErrNoToken
	}
	base, err := gitlabMRAPI(f.Secrets, link)
	if err != nil {
		return Change{}, err
	}
	var mr gitlabMRDetail
	if err := f.call(ctx, http.MethodGet, base, nil, &mr); err != nil {
		return Change{}, err
	}
	c := Change{Title: mr.Title, Body: clip(mr.Description, changeBodyCap), Author: mr.Author.Username, Branch: mr.SourceBranch, Base: mr.TargetBranch,
		Head: mr.DiffRefs.HeadSHA, State: gitlabMRState(mr.State, mr.Draft), baseSHA: mr.DiffRefs.BaseSHA, startSHA: mr.DiffRefs.StartSHA,
		Opened: mr.CreatedAt, Behind: mr.DetailedMergeStatus == "need_rebase", Conflicts: mr.HasConflicts}
	c.Activity = append(c.Activity, ChangeEvent{Kind: "opened", Who: mr.Author.Username, At: mr.CreatedAt})
	if mr.State == "merged" {
		c.Activity = append(c.Activity, ChangeEvent{Kind: "merged", Who: mr.MergeUser.Username, At: mr.MergedAt})
	}
	var approvals struct {
		ApprovedBy []struct {
			User struct {
				Username string `json:"username"`
			} `json:"user"`
		} `json:"approved_by"`
	}
	var commits []struct {
		ShortID    string    `json:"short_id"`
		Title      string    `json:"title"`
		AuthorName string    `json:"author_name"`
		CreatedAt  time.Time `json:"created_at"`
	}
	var wg sync.WaitGroup
	for endpoint, out := range map[string]any{base + "/approvals": &approvals, base + "/commits?per_page=100": &commits} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = f.call(ctx, http.MethodGet, endpoint, nil, out)
		}()
	}
	wg.Wait()
	approved := map[string]bool{}
	for _, a := range approvals.ApprovedBy {
		approved[a.User.Username] = true
		c.Reviewers = append(c.Reviewers, ChangePerson{Name: a.User.Username, State: PersonApproved})
	}
	for _, r := range mr.Reviewers {
		if !approved[r.Username] {
			c.Reviewers = append(c.Reviewers, ChangePerson{Name: r.Username, State: PersonWaiting})
		}
	}
	c.Commits = len(commits)
	for _, k := range commits {
		c.Activity = append(c.Activity, ChangeEvent{Kind: "commit", Who: k.AuthorName, Text: k.ShortID + " " + clip(k.Title, 120), At: k.CreatedAt})
	}
	if mr.HeadPipeline.Status != "" {
		c.CheckRuns = append(c.CheckRuns, ChangeCheck{Name: "pipeline", State: checkState(mr.HeadPipeline.Status, map[string]string{"success": "success", "failed": "failed", "canceled": "canceled", "skipped": "skipped", "manual": "manual"}[mr.HeadPipeline.Status])})
	}
	for page := 1; page <= changeFilePages; page++ {
		var diffs []struct {
			OldPath     string `json:"old_path"`
			NewPath     string `json:"new_path"`
			Diff        string `json:"diff"`
			NewFile     bool   `json:"new_file"`
			RenamedFile bool   `json:"renamed_file"`
			DeletedFile bool   `json:"deleted_file"`
		}
		if err := f.call(ctx, http.MethodGet, fmt.Sprintf("%s/diffs?per_page=100&page=%d", base, page), nil, &diffs); err != nil {
			return Change{}, err
		}
		for _, d := range diffs {
			file := ChangeFile{Path: d.NewPath, Status: "modified"}
			switch {
			case d.NewFile:
				file.Status = "added"
			case d.DeletedFile:
				file.Status = "removed"
			case d.RenamedFile:
				file.Status, file.Was = "renamed", d.OldPath
			}
			file.Hunks, file.Cut = ParsePatch(d.Diff)
			for _, line := range strings.Split(d.Diff, "\n") {
				switch {
				case strings.HasPrefix(line, "+"):
					file.Adds++
				case strings.HasPrefix(line, "-"):
					file.Dels++
				}
			}
			c.Files = append(c.Files, file)
		}
		if len(diffs) < 100 {
			break
		}
	}
	return c, nil
}

func reviewWords(body string) string {
	body = oneLine(body)
	if body == "" {
		return ""
	}
	return ": " + clip(body, 160)
}

func firstLineOfText(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return clip(line, 120)
}

func (f Forge) SendReview(ctx context.Context, link, verdict, body string, notes []ReviewNote) error {
	link = PullLink(link)
	verdict = strings.ToLower(strings.TrimSpace(verdict))
	body = strings.TrimSpace(body)
	switch verdict {
	case ReviewApprove, ReviewRequest, ReviewComment:
	default:
		return fmt.Errorf("a review is approve, request or comment, not %q", verdict)
	}
	var kept []ReviewNote
	for _, n := range notes {
		n.Body = strings.TrimSpace(n.Body)
		if n.Body == "" || n.Path == "" || n.Line <= 0 {
			continue
		}
		if err := CorrectionRefusal(n.Body); err != nil {
			return err
		}
		kept = append(kept, n)
	}
	if err := CorrectionRefusal(body); err != nil {
		return err
	}
	if verdict != ReviewApprove && body == "" && len(kept) == 0 {
		return fmt.Errorf("say what to change")
	}
	switch {
	case githubPull.MatchString(link):
		return f.sendGitHubReview(ctx, link, verdict, body, kept)
	case gitlabMR.MatchString(link):
		return f.sendGitLabReview(ctx, link, verdict, body, kept)
	}
	return fmt.Errorf("%s is not a GitHub pull request or a GitLab merge request", link)
}

func (f Forge) sendGitHubReview(ctx context.Context, link, verdict, body string, notes []ReviewNote) error {
	m := githubPRPath.FindStringSubmatch(link)
	if m == nil {
		return fmt.Errorf("cannot read a repo and number out of %s", link)
	}
	if f.Secrets.GitHub == "" {
		return ErrNoToken
	}
	payload := map[string]any{"event": map[string]string{ReviewApprove: "APPROVE", ReviewRequest: "REQUEST_CHANGES", ReviewComment: "COMMENT"}[verdict]}
	if body != "" {
		payload["body"] = body
	}
	if len(notes) > 0 {
		comments := make([]map[string]any, 0, len(notes))
		for _, n := range notes {
			side := "RIGHT"
			if n.Side == "-" {
				side = "LEFT"
			}
			comments = append(comments, map[string]any{"path": n.Path, "line": n.Line, "side": side, "body": n.Body})
		}
		payload["comments"] = comments
	}
	return f.call(ctx, http.MethodPost, f.GitHubAPI+reposSegment+m[1]+pullsSegment+m[2]+"/reviews", payload, nil)
}

func (f Forge) sendGitLabReview(ctx context.Context, link, verdict, body string, notes []ReviewNote) error {
	if f.Secrets.GitLab == "" {
		return ErrNoToken
	}
	base, err := gitlabMRAPI(f.Secrets, link)
	if err != nil {
		return err
	}
	if len(notes) > 0 {
		var mr gitlabMRDetail
		if err := f.call(ctx, http.MethodGet, base, nil, &mr); err != nil {
			return err
		}
		for _, n := range notes {
			position := map[string]any{"position_type": "text", "base_sha": mr.DiffRefs.BaseSHA, "start_sha": mr.DiffRefs.StartSHA, "head_sha": mr.DiffRefs.HeadSHA,
				"new_path": n.Path, "old_path": firstSet(n.Was, n.Path)}
			if n.Side == "-" {
				position["old_line"] = n.Line
			} else {
				position["new_line"] = n.Line
			}
			if err := f.call(ctx, http.MethodPost, base+"/discussions", map[string]any{"body": n.Body, "position": position}, nil); err != nil {
				return err
			}
		}
	}
	if body != "" {
		note := body
		if verdict == ReviewRequest {
			note = "Changes requested: " + body
		}
		if err := f.call(ctx, http.MethodPost, base+"/notes", map[string]string{"body": note}, nil); err != nil {
			return err
		}
	}
	switch verdict {
	case ReviewApprove:
		return f.call(ctx, http.MethodPost, base+"/approve", nil, nil)
	case ReviewRequest:
		_ = f.call(ctx, http.MethodPost, base+"/unapprove", nil, nil)
	}
	return nil
}

func FixMessage(link string, notes []ReviewNote, body string) string {
	var asks []string
	if body = oneLine(body); body != "" {
		asks = append(asks, body)
	}
	for _, n := range notes {
		if text := oneLine(n.Body); text != "" {
			asks = append(asks, fmt.Sprintf("%s:%d: %s", n.Path, n.Line, text))
		}
	}
	if len(asks) == 0 {
		return ""
	}
	for i := range asks {
		asks[i] = fmt.Sprintf("(%d) %s", i+1, asks[i])
	}
	return fmt.Sprintf("I read %s on my phone. Change these, run the checks, push, then answer each in one line: %s", link, strings.Join(asks, " "))
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
