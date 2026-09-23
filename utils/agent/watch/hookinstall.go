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
		return failed(out, err, "admin on the repo (a token with admin:repo_hook)")
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
	out := HookInstall{Repo: project}
	if baseURL == "" {
		baseURL = "https://gitlab.com"
	}
	base := strings.TrimRight(baseURL, "/") + "/api/v4/projects/" + url.PathEscape(project) + "/hooks"
	var hooks []struct {
		ID  int64  `json:"id"`
		URL string `json:"url"`
	}
	headers := map[string]string{"PRIVATE-TOKEN": token}
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
	if strings.Contains(err.Error(), "HTTP 403") || strings.Contains(err.Error(), "HTTP 404") {
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
