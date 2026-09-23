package watch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HookInstall is one repo's webhook as corgi left it.
type HookInstall struct {
	Repo    string
	Action  string // created, updated, unchanged
	Err     error
	Missing string // the permission a failure points at
	// ReadOnly says the token could not write at all, so another login may.
	ReadOnly bool
}

// The events each forge sends that corgi parses. Everything else a forge can
// send is left off, so a busy repo does not wake the laptop for nothing.
var (
	githubHookEvents = []string{"issue_comment", "pull_request_review", "pull_request_review_comment"}
	hookHTTP         = &http.Client{Timeout: 20 * time.Second}
)

// InstallGitHubHook points one repo's webhook at corgi: a hook already
// aimed at hookURL is updated in place (events and secret), else one is made.
func InstallGitHubHook(ctx context.Context, token, repo, hookURL, secret string) HookInstall {
	out := HookInstall{Repo: repo}
	var hooks []struct {
		ID     int64    `json:"id"`
		Events []string `json:"events"`
		Active bool     `json:"active"`
		Config struct {
			URL string `json:"url"`
		} `json:"config"`
	}
	base := strings.TrimRight(GitHubAPI, "/") + "/repos/" + repo + "/hooks"
	if err := hookCall(ctx, http.MethodGet, base, githubHeaders(token), nil, &hooks); err != nil {
		return githubHookRefused(ctx, token, repo, failed(out, err, "admin on the repo (a token with admin:repo_hook)"))
	}
	body := map[string]any{
		"active": true,
		"events": githubHookEvents,
		"config": map[string]any{"url": hookURL, "content_type": "json", "secret": secret, "insecure_ssl": "0"},
	}
	for _, h := range hooks {
		if h.Config.URL != hookURL {
			continue
		}
		out.Action = "updated"
		return orFailed(out, hookCall(ctx, http.MethodPatch, fmt.Sprintf("%s/%d", base, h.ID), githubHeaders(token), body, nil), "admin on the repo")
	}
	body["name"] = "web"
	out.Action = "created"
	return orFailed(out, hookCall(ctx, http.MethodPost, base, githubHeaders(token), body, nil), "admin on the repo (a token with admin:repo_hook)")
}

// InstallGitLabHook does the same for a GitLab project: comments only, with
// the shared secret as its token.
func InstallGitLabHook(ctx context.Context, baseURL, token, project, hookURL, secret string) HookInstall {
	return InstallGitLabHookWith(ctx, baseURL, map[string]string{"PRIVATE-TOKEN": token}, project, hookURL, secret)
}

// InstallGitLabHookWith takes the auth headers, for a login that is not a
// personal token (glab's OAuth session is a bearer token).
func InstallGitLabHookWith(ctx context.Context, baseURL string, headers map[string]string, project, hookURL, secret string) HookInstall {
	out := HookInstall{Repo: project}
	if baseURL == "" {
		baseURL = "https://gitlab.com"
	}
	base := strings.TrimRight(baseURL, "/") + "/api/v4/projects/" + url.PathEscape(project) + "/hooks"
	var hooks []struct {
		ID  int64  `json:"id"`
		URL string `json:"url"`
	}
	if err := hookCall(ctx, http.MethodGet, base, headers, nil, &hooks); err != nil {
		return failed(out, err, "Maintainer on the project (a token with the api scope)")
	}
	body := map[string]any{
		"url": hookURL, "token": secret, "note_events": true, "push_events": false,
		"enable_ssl_verification": true,
	}
	for _, h := range hooks {
		if h.URL != hookURL {
			continue
		}
		out.Action = "updated"
		return orFailed(out, hookCall(ctx, http.MethodPut, fmt.Sprintf("%s/%d", base, h.ID), headers, body, nil), "Maintainer on the project")
	}
	out.Action = "created"
	return orFailed(out, hookCall(ctx, http.MethodPost, base, headers, body, nil), "Maintainer on the project (a token with the api scope)")
}

func githubHeaders(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token, "Accept": "application/vnd.github+json"}
}

func failed(out HookInstall, err error, missing string) HookInstall {
	out.Action, out.Err = "", err
	switch msg := err.Error(); {
	case strings.Contains(msg, "insufficient_scope"), strings.Contains(msg, "HTTP 401"):
		out.Missing, out.ReadOnly = "a token that can write (GitLab: the api scope; GitHub: admin:repo_hook)", true
	case strings.Contains(msg, "HTTP 403"), strings.Contains(msg, "HTTP 404"):
		out.Missing = missing
	}
	return out
}

func orFailed(out HookInstall, err error, missing string) HookInstall {
	if err != nil {
		return failed(out, err, missing)
	}
	return out
}

func hookCall(ctx context.Context, method, endpoint string, headers map[string]string, body any, into any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := hookHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 160))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	if into == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(into)
}

// githubHookRefused says which of the two GitHub needs is missing: the role
// (only a repo admin may add a hook) or the token's admin:repo_hook scope.
func githubHookRefused(ctx context.Context, token, repo string, out HookInstall) HookInstall {
	var r struct {
		Permissions struct {
			Admin    bool `json:"admin"`
			Maintain bool `json:"maintain"`
		} `json:"permissions"`
	}
	if hookCall(ctx, http.MethodGet, strings.TrimRight(GitHubAPI, "/")+"/repos/"+repo, githubHeaders(token), nil, &r) != nil {
		return out
	}
	owner, _, _ := strings.Cut(repo, "/")
	switch {
	case !r.Permissions.Admin:
		out.Missing, out.ReadOnly = "repo admin (you are not) — an owner of "+owner+" can add one org webhook instead, covering every repo", false
	default:
		out.Missing, out.ReadOnly = "the admin:repo_hook scope — `gh auth refresh -s admin:repo_hook`", true
	}
	return out
}
