package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// A pull request merges unread only when the forge's own account of it says it
// is small and away from what breaks badly. The run's risk card can only hold
// it back: an agent that writes its own score must not be able to wave itself
// through, so these rules read the diff and hold anything they are unsure of.
const (
	unreadMaxFiles = 25
	unreadMaxLines = 400
	// UnreadMaxRisk is the top of the band the risk skill calls trivial.
	UnreadMaxRisk = 2
	// UnreadMinScenarios is the share of hidden stories the change must satisfy.
	UnreadMinScenarios = 90
)

type ChangedFile struct {
	Path    string
	Lines   int
	Removed bool
}

type PullChanges struct {
	Files []ChangedFile
	Body  string
	Head  string
	// Partial says the forge would not list all of it.
	Partial bool
}

var riskCard = regexp.MustCompile(`<!--\s*corgi-risk\s+score=(\d+)`)

var unreadPaths = []struct {
	why   string
	words []string
}{
	{"auth or secrets", []string{"auth", "login", "session", "permission", "oauth", "jwt", "token", "secret", "credential", "crypto", "password", ".env"}},
	{"payments", []string{"payment", "billing", "invoice", "stripe", "purchase", "checkout", "refund", "revenuecat", "subscription"}},
	{"data or schema", []string{"migration", "migrate", "schema", ".sql", "prisma", "/db/", "graphql"}},
	{"CI, infra or release", []string{".github/", ".gitlab", "jenkinsfile", ".circleci", "dockerfile", "docker-compose", "terraform", ".tf", "helm", "k8s", "deploy", "release", "fastlane", "makefile", "corgi-compose", "eas.json"}},
	{"dependencies", []string{"go.mod", "go.sum", "package.json", "package-lock", "yarn.lock", "pnpm-lock", "bun.lock", "podfile", "gemfile", "requirements.txt", "pyproject", "cargo."}},
	{"native code", []string{"ios/", "android/", ".plist", "app.json", "app.config", ".entitlements"}},
	{"the checks themselves", []string{"jest.config", "vitest.config", "eslint", "sonar", "codecov", "golangci", "tsconfig", ".husky", "lefthook"}},
}

// UnreadRefusal says why this change needs a person, or "" when it may merge
// with nobody reading it.
func UnreadRefusal(c PullChanges) string {
	if c.Partial {
		return "the forge would not list all of it"
	}
	if len(c.Files) == 0 {
		return "the forge lists no files"
	}
	if len(c.Files) > unreadMaxFiles {
		return fmt.Sprintf("%d files", len(c.Files))
	}
	lines := 0
	for _, f := range c.Files {
		lines += f.Lines
		if why := unreadPathRefusal(f); why != "" {
			return why
		}
	}
	if lines > unreadMaxLines {
		return fmt.Sprintf("%d changed lines", lines)
	}
	score, ok := RiskScoreOf(c.Body)
	if !ok {
		return "no risk card"
	}
	if score > UnreadMaxRisk {
		return "risk " + strconv.Itoa(score)
	}
	return ""
}

func unreadPathRefusal(f ChangedFile) string {
	path := strings.ToLower(f.Path)
	if f.Removed && strings.Contains(path, "test") {
		return "removes " + f.Path
	}
	// "author" is not "auth". Everything else over-matches on purpose: a false
	// alarm costs a person a look, a miss costs a bad merge.
	path = strings.ReplaceAll(path, "author", "")
	for _, group := range unreadPaths {
		for _, word := range group.words {
			if strings.Contains(path, word) {
				return "touches " + group.why + " (" + f.Path + ")"
			}
		}
	}
	return ""
}

// RiskScoreOf reads the score the risk skill stamped into a description.
func RiskScoreOf(body string) (int, bool) {
	m := riskCard.FindStringSubmatch(body)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	return n, err == nil
}

// UnreadRunRefusal holds a run that did not end clean: failed, or one that says
// it skipped a check.
func UnreadRunRefusal(r FixRecord) string {
	switch {
	case !r.Done():
		return "its run is still going"
	case strings.TrimSpace(r.Error) != "" || r.Failure != "":
		return "its run failed"
	case len(r.Gaps) > 0:
		return "its run skipped a check: " + string(r.Gaps[0])
	}
	return ""
}

var unreadRunKinds = map[string]bool{string(KindIssueNew): true, string(KindIssueComment): true, string(KindTask): true, string(KindRoutine): true, string(KindErrorNew): true}

// RunThatStartedPull is the newest run that opened this pull request. A review
// run on a pull request someone else started does not count: it merges unread
// only if corgi started it, never one a person opened by hand.
func (l *FixLog) RunThatStartedPull(workspace, link string) (FixRecord, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	want := PullRef(link)
	for i := len(l.Started) - 1; i >= 0; i-- {
		r := l.Started[i]
		if r.Workspace != workspace || !unreadRunKinds[r.Kind] {
			continue
		}
		for _, pr := range r.PRs {
			if want != "" && PullRef(pr) == want {
				return r, true
			}
		}
	}
	return FixRecord{}, false
}

// LastRunOnPull is the newest run of any kind that worked on this pull request.
func (l *FixLog) LastRunOnPull(workspace, link string) (FixRecord, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	want := PullRef(link)
	for i := len(l.Started) - 1; i >= 0; i-- {
		r := l.Started[i]
		if r.Workspace != workspace {
			continue
		}
		for _, candidate := range append([]string{r.URL}, r.PRs...) {
			if want != "" && PullRef(candidate) == want {
				return r, true
			}
		}
	}
	return FixRecord{}, false
}

// FetchPullChanges asks the forge what a pull request changes.
func FetchPullChanges(ctx context.Context, s Secrets, link string) (PullChanges, error) {
	switch {
	case strings.Contains(link, "github.com/"):
		m := githubPRPath.FindStringSubmatch(link)
		if m == nil {
			return PullChanges{}, fmt.Errorf("cannot read a repo and number out of %s", link)
		}
		if s.GitHub == "" {
			return PullChanges{}, ErrNoToken
		}
		base := "https://api.github.com/repos/" + m[1] + "/pulls/" + m[2]
		auth := map[string]string{"Authorization": "Bearer " + s.GitHub, "Accept": "application/vnd.github+json"}
		pull, err := readForge(ctx, base, auth)
		if err != nil {
			return PullChanges{}, err
		}
		files, err := readForge(ctx, base+"/files?per_page=100", auth)
		if err != nil {
			return PullChanges{}, err
		}
		return parseGitHubChanges(pull, files)
	case strings.Contains(link, "/-/merge_requests/"):
		if s.GitLab == "" {
			return PullChanges{}, ErrNoToken
		}
		api, err := gitlabMRAPI(s, link)
		if err != nil {
			return PullChanges{}, err
		}
		changes, err := readForge(ctx, api+"/changes", map[string]string{"PRIVATE-TOKEN": s.GitLab})
		if err != nil {
			return PullChanges{}, err
		}
		return parseGitLabChanges(changes)
	}
	return PullChanges{}, fmt.Errorf("%s is not a GitHub pull request or a GitLab merge request", link)
}

func readForge(ctx context.Context, url string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d reading what the pull request changes", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 32<<20))
}

func parseGitHubChanges(pull, files []byte) (PullChanges, error) {
	var p struct {
		Body string `json:"body"`
		Head struct {
			SHA string `json:"sha"`
		} `json:"head"`
		ChangedFiles int `json:"changed_files"`
	}
	if err := json.Unmarshal(pull, &p); err != nil {
		return PullChanges{}, err
	}
	var list []struct {
		Filename     string `json:"filename"`
		PreviousName string `json:"previous_filename"`
		Status       string `json:"status"`
		Changes      int    `json:"changes"`
	}
	if err := json.Unmarshal(files, &list); err != nil {
		return PullChanges{}, err
	}
	c := PullChanges{Body: p.Body, Head: p.Head.SHA, Partial: p.ChangedFiles != len(list)}
	for _, f := range list {
		c.Files = append(c.Files, ChangedFile{Path: f.Filename, Lines: f.Changes, Removed: f.Status == "removed"})
		if f.PreviousName != "" {
			c.Files = append(c.Files, ChangedFile{Path: f.PreviousName, Removed: true})
		}
	}
	return c, nil
}

func parseGitLabChanges(data []byte) (PullChanges, error) {
	var mr struct {
		Description string `json:"description"`
		SHA         string `json:"sha"`
		Overflow    bool   `json:"overflow"`
		Changes     []struct {
			OldPath     string `json:"old_path"`
			NewPath     string `json:"new_path"`
			Diff        string `json:"diff"`
			DeletedFile bool   `json:"deleted_file"`
			RenamedFile bool   `json:"renamed_file"`
			TooLarge    bool   `json:"too_large"`
		} `json:"changes"`
	}
	if err := json.Unmarshal(data, &mr); err != nil {
		return PullChanges{}, err
	}
	c := PullChanges{Body: mr.Description, Head: mr.SHA, Partial: mr.Overflow}
	for _, f := range mr.Changes {
		c.Partial = c.Partial || f.TooLarge
		lines := 0
		for _, line := range strings.Split(f.Diff, "\n") {
			if (strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-")) && !strings.HasPrefix(line, "+++") && !strings.HasPrefix(line, "---") {
				lines++
			}
		}
		c.Files = append(c.Files, ChangedFile{Path: f.NewPath, Lines: lines, Removed: f.DeletedFile})
		if f.RenamedFile {
			c.Files = append(c.Files, ChangedFile{Path: f.OldPath, Removed: true})
		}
	}
	return c, nil
}
