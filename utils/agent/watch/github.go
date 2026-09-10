package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// GitHub polls the notifications feed: one conditional request per round,
// a 304 when nothing moved.
type GitHub struct {
	Token  string
	Repos  []string // "owner/repo"; empty means every repo
	Me     string
	Client *http.Client
	URL    string
}

// githubAuthToken asks the gh CLI for its token; tests override it.
var githubAuthToken = func() string {
	out, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// NewGitHub uses the saved token, else whatever `gh auth token` has.
func NewGitHub(s Secrets, repos []string) *GitHub {
	token := strings.TrimSpace(s.GitHub)
	if token == "" {
		token = strings.TrimSpace(githubAuthToken())
	}
	return &GitHub{Token: token, Repos: repos}
}

func (g *GitHub) Name() string { return "github" }

// githubReasons are the notification reasons that mean a pull request
// wants my attention; everything else (subscribed, ci_activity) is noise.
var githubReasons = map[string]struct {
	kind Kind
	mine bool
}{
	"review_requested": {KindReviewRequested, false},
	"mention":          {KindPRComment, true},
	"author":           {KindPRComment, true},
	"comment":          {KindPRComment, true},
	"team_mention":     {KindPRComment, false},
	// GitHub's default for Actions is to notify only when a run you caused
	// fails, so ci_activity on your own repo is a red build. It arrives as a
	// CheckSuite, not a PullRequest, and is only polled when --ci asked.
	"ci_activity": {KindCIFailed, true},
}

type githubThread struct {
	ID        string `json:"id"`
	Reason    string `json:"reason"`
	UpdatedAt string `json:"updated_at"`
	Subject   struct {
		Title string `json:"title"`
		URL   string `json:"url"`
		Type  string `json:"type"`
	} `json:"subject"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

// Poll sends If-Modified-Since from the cursor; a 304 returns the same
// cursor without reading the body. The first call also fetches /user once.
func (g *GitHub) Poll(ctx context.Context, cursor Cursor) ([]Event, Cursor, error) {
	if g.Token == "" {
		return nil, cursor, ErrNoToken
	}
	if cursor == nil {
		cursor = Cursor{}
	}
	if g.Me == "" {
		g.Me = cursor["me"]
	}
	if g.Me == "" {
		var user struct {
			Login string `json:"login"`
		}
		resp, err := g.get(ctx, "/user", "")
		if err != nil {
			return nil, cursor, err
		}
		err = githubDecode(resp, &user)
		if err != nil {
			return nil, cursor, err
		}
		g.Me = user.Login
	}

	resp, err := g.get(ctx, "/notifications?all=false&participating=true", cursor["lastModified"])
	if err != nil {
		return nil, cursor, err
	}
	if resp.StatusCode == http.StatusNotModified {
		resp.Body.Close()
		return nil, cursor, nil
	}
	var threads []githubThread
	if err := githubDecode(resp, &threads); err != nil {
		return nil, cursor, err
	}

	next := Cursor{}
	for k, v := range cursor {
		next[k] = v
	}
	next["me"] = g.Me
	if lm := resp.Header.Get("Last-Modified"); lm != "" {
		next["lastModified"] = lm
	}
	if pi := resp.Header.Get("X-Poll-Interval"); pi != "" {
		next["pollInterval"] = pi
	}

	var events []Event
	states := map[string]string{} // one lookup per pull request per round
	for _, t := range threads {
		if !g.wantsRepo(t.Repository.FullName) {
			continue
		}
		r, ok := githubReasons[t.Reason]
		if !ok {
			continue
		}
		if r.kind == KindCIFailed {
			if t.Subject.Type != "CheckSuite" {
				continue
			}
			at, _ := time.Parse(time.RFC3339, t.UpdatedAt)
			events = append(events, Event{
				Key:    "github:ci:" + t.Repository.FullName + ":" + t.ID + ":" + t.UpdatedAt,
				Source: g.Name(),
				Kind:   KindCIFailed,
				Ref:    t.Repository.FullName,
				Title:  firstNonEmptyText(t.Subject.Title, "a workflow run failed"),
				URL:    "https://github.com/" + t.Repository.FullName + "/actions",
				Mine:   true,
				At:     at,
			})
			continue
		}
		if t.Subject.Type != "PullRequest" {
			continue
		}
		number := t.Subject.URL[strings.LastIndex(t.Subject.URL, "/")+1:]
		ref := t.Repository.FullName + "#" + number
		at, _ := time.Parse(time.RFC3339, t.UpdatedAt)
		// A notification says nothing about whether the pull request is still
		// open, so a comment on one merged last week reads exactly like one on
		// live work. One lookup per pull request in a round settles it.
		state := g.pullState(ctx, states, t.Subject.URL)
		events = append(events, Event{
			Key:    "github:" + ref + ":" + t.ID + ":" + t.UpdatedAt,
			Source: g.Name(),
			Kind:   r.kind,
			Ref:    ref,
			Title:  t.Subject.Title,
			URL:    "https://github.com/" + t.Repository.FullName + "/pull/" + number,
			Mine:   r.mine,
			State:  state,
			At:     at,
		})
	}
	return events, next, nil
}

// pullState is "open", "merged" or "closed" for one pull request, cached for
// the round so several notifications about the same one cost a single call.
// An unreadable answer is "", which the rules treat as still open: guessing a
// pull request closed would silently swallow real feedback.
func (g *GitHub) pullState(ctx context.Context, cache map[string]string, apiURL string) string {
	if apiURL == "" {
		return ""
	}
	if state, ok := cache[apiURL]; ok {
		return state
	}
	cache[apiURL] = ""
	path := strings.TrimPrefix(apiURL, "https://api.github.com")
	if path == apiURL {
		return ""
	}
	resp, err := g.get(ctx, path, "")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var pr struct {
		State  string `json:"state"`
		Merged bool   `json:"merged"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&pr) != nil {
		return ""
	}
	state := pr.State
	if pr.Merged {
		state = "merged"
	}
	cache[apiURL] = state
	return state
}

func firstNonEmptyText(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

func (g *GitHub) wantsRepo(fullName string) bool {
	if len(g.Repos) == 0 {
		return true
	}
	return containsFold(g.Repos, fullName)
}

// get returns the response for the caller to close; a non-2xx, non-304
// status becomes an error carrying the start of the body.
func (g *GitHub) get(ctx context.Context, path, ifModifiedSince string) (*http.Response, error) {
	base := g.URL
	if base == "" {
		base = "https://api.github.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+path, nil)
	if err != nil {
		return nil, fmt.Errorf("github %s: %v", path, err)
	}
	req.Header.Set("Authorization", "Bearer "+g.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	if ifModifiedSince != "" {
		req.Header.Set("If-Modified-Since", ifModifiedSince)
	}
	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github %s: %v", path, err)
	}
	if resp.StatusCode == http.StatusNotModified || (resp.StatusCode >= 200 && resp.StatusCode < 300) {
		return resp, nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 120))
	return nil, fmt.Errorf("github %s: HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
}

func githubDecode(resp *http.Response, v any) error {
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("github: decoding %s: %v", resp.Request.URL.Path, err)
	}
	return nil
}
