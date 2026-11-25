package watch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type ThreadTarget struct {
	Thread string `json:"thread"`
	Note   string `json:"note"`
}

const githubResolveMutation = `mutation($id:ID!){resolveReviewThread(input:{threadId:$id}){thread{isResolved}}}`

func ReplyInThread(ctx context.Context, s Secrets, link string, to ThreadTarget, body string) error {
	return defaultThreadsClient.reply(ctx, s, link, to, body)
}

func ResolveThread(ctx context.Context, s Secrets, link, thread string) error {
	return defaultThreadsClient.resolve(ctx, s, link, thread)
}

func (c threadsClient) githubToken(s Secrets) (string, error) {
	token := strings.TrimSpace(s.GitHub)
	if token == "" {
		token = strings.TrimSpace(c.ghToken())
	}
	if token == "" {
		return "", ErrNoToken
	}
	return token, nil
}

func (c threadsClient) reply(ctx context.Context, s Secrets, link string, to ThreadTarget, body string) error {
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("an empty reply")
	}
	payload, _ := json.Marshal(map[string]string{"body": body})
	if m := githubPRPath.FindStringSubmatch(link); m != nil {
		token, err := c.githubToken(s)
		if err != nil {
			return err
		}
		path := "/repos/" + m[1] + "/issues/" + m[2] + "/comments"
		if to.Thread != "" && to.Note != "" {
			path = "/repos/" + m[1] + "/pulls/" + m[2] + "/comments/" + to.Note + "/replies"
		}
		return c.send(ctx, http.MethodPost, strings.TrimRight(c.github, "/")+path, "Authorization", "Bearer "+token, payload, link)
	}
	if !strings.Contains(link, "/-/merge_requests/") || s.GitLab == "" {
		return ErrNoToken
	}
	api, err := c.gitlab(s, link)
	if err != nil {
		return err
	}
	path := "/notes"
	if to.Thread != "" {
		path = "/discussions/" + to.Thread + "/notes"
	}
	return c.send(ctx, http.MethodPost, api+path, gitlabTokenHeader, s.GitLab, payload, link)
}

func (c threadsClient) resolve(ctx context.Context, s Secrets, link, thread string) error {
	if thread == "" {
		return fmt.Errorf("no thread to resolve")
	}
	if githubPRPath.MatchString(link) {
		token, err := c.githubToken(s)
		if err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"query": githubResolveMutation, "variables": map[string]string{"id": thread}})
		return c.send(ctx, http.MethodPost, strings.TrimRight(c.github, "/")+"/graphql", "Authorization", "Bearer "+token, payload, link)
	}
	if !strings.Contains(link, "/-/merge_requests/") || s.GitLab == "" {
		return ErrNoToken
	}
	api, err := c.gitlab(s, link)
	if err != nil {
		return err
	}
	return c.send(ctx, http.MethodPut, api+"/discussions/"+thread+"?resolved=true", gitlabTokenHeader, s.GitLab, nil, link)
}

func (c threadsClient) send(ctx context.Context, method, url, header, value string, payload []byte, link string) error {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set(header, value)
	req.Header.Set("Content-Type", "application/json")
	var out struct {
		Errors []githubThreadPageErrors `json:"errors"`
	}
	if err := c.do(req, link, &out); err != nil {
		return err
	}
	if len(out.Errors) > 0 {
		return fmt.Errorf("%s: %s", link, out.Errors[0].Message)
	}
	return nil
}
