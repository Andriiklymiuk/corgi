package watch

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

const (
	ArmedNow     = "armed"
	ArmedAlready = "already armed"
)

var gitlabPipelineRuns = map[string]bool{"running": true, "pending": true, "created": true, "waiting_for_resource": true, "preparing": true, "scheduled": true}

func (f Forge) ArmMerge(ctx context.Context, link string) (string, error) {
	link = PullLink(link)
	switch {
	case githubPull.MatchString(link):
		return f.armGitHub(ctx, link)
	case gitlabMR.MatchString(link):
		return f.armGitLab(ctx, link)
	}
	return "", fmt.Errorf("%s is not a GitHub pull request or a GitLab merge request", link)
}

func (f Forge) armGitHub(ctx context.Context, link string) (string, error) {
	m := githubPRPath.FindStringSubmatch(link)
	if m == nil {
		return "", fmt.Errorf("cannot read a repo and number out of %s", link)
	}
	if f.Secrets.GitHub == "" {
		return "", ErrNoToken
	}
	var pr struct {
		NodeID    string `json:"node_id"`
		State     string `json:"state"`
		Draft     bool   `json:"draft"`
		Merged    bool   `json:"merged"`
		AutoMerge *struct {
			MergeMethod string `json:"merge_method"`
		} `json:"auto_merge"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	if err := f.call(ctx, http.MethodGet, f.GitHubAPI+reposSegment+m[1]+pullsSegment+m[2], nil, &pr); err != nil {
		return "", err
	}
	var me struct {
		Login string `json:"login"`
	}
	if err := f.call(ctx, http.MethodGet, f.GitHubAPI+"/user", nil, &me); err != nil {
		return "", err
	}
	switch {
	case pr.AutoMerge != nil:
		return ArmedAlready, nil
	case pr.Merged || pr.State != "open":
		return "", fmt.Errorf("%s is not open", PullRef(link))
	case pr.Draft:
		return "", fmt.Errorf("%s is a draft", PullRef(link))
	case !strings.EqualFold(pr.User.Login, me.Login):
		return "", fmt.Errorf("%s is %s's, not mine", PullRef(link), pr.User.Login)
	}
	var last error
	for _, method := range []string{"MERGE", "SQUASH", "REBASE"} {
		var out struct {
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		payload := map[string]any{
			"query":     "mutation($id: ID!, $method: PullRequestMergeMethod!) { enablePullRequestAutoMerge(input: {pullRequestId: $id, mergeMethod: $method}) { clientMutationId } }",
			"variables": map[string]string{"id": pr.NodeID, "method": method},
		}
		if err := f.call(ctx, http.MethodPost, f.GitHubAPI+"/graphql", payload, &out); err != nil {
			return "", err
		}
		if len(out.Errors) == 0 {
			return ArmedNow, nil
		}
		last = fmt.Errorf("%s: %s", PullRef(link), out.Errors[0].Message)
		if !strings.Contains(strings.ToLower(out.Errors[0].Message), "merge method") {
			break
		}
	}
	return "", last
}

func (f Forge) armGitLab(ctx context.Context, link string) (string, error) {
	if f.Secrets.GitLab == "" {
		return "", ErrNoToken
	}
	base, err := gitlabMRAPI(f.Secrets, link)
	if err != nil {
		return "", err
	}
	var mr struct {
		State  string `json:"state"`
		Draft  bool   `json:"draft"`
		Armed  bool   `json:"merge_when_pipeline_succeeds"`
		SHA    string `json:"sha"`
		Author struct {
			Username string `json:"username"`
		} `json:"author"`
		HeadPipeline *struct {
			Status string `json:"status"`
		} `json:"head_pipeline"`
	}
	if err := f.call(ctx, http.MethodGet, base, nil, &mr); err != nil {
		return "", err
	}
	var me struct {
		Username string `json:"username"`
	}
	if err := f.call(ctx, http.MethodGet, f.gitlabAPI()+"/user", nil, &me); err != nil {
		return "", err
	}
	switch {
	case mr.Armed:
		return ArmedAlready, nil
	case mr.State != "opened":
		return "", fmt.Errorf("%s is not open", PullRef(link))
	case mr.Draft:
		return "", fmt.Errorf("%s is a draft", PullRef(link))
	case !strings.EqualFold(mr.Author.Username, me.Username):
		return "", fmt.Errorf("%s is %s's, not mine", PullRef(link), mr.Author.Username)
	case mr.HeadPipeline == nil || !gitlabPipelineRuns[mr.HeadPipeline.Status]:
		return "", fmt.Errorf("%s has no pipeline running: arming it now would merge it at once", PullRef(link))
	}
	payload := map[string]any{"merge_when_pipeline_succeeds": true, "auto_merge": true}
	if mr.SHA != "" {
		payload["sha"] = mr.SHA
	}
	if err := f.call(ctx, http.MethodPut, base+"/merge", payload, nil); err != nil {
		return "", err
	}
	return ArmedNow, nil
}
