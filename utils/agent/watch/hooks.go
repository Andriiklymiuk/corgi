package watch

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ErrBadSignature is a webhook whose signature did not match the secret.
var ErrBadSignature = errors.New("webhook signature does not match")

// VerifyHook checks the request against the shared secret the way each
// service signs: Linear and GitHub HMAC the body, GitLab sends the secret
// as a header, Jira has no signature so the secret rides in the query.
func VerifyHook(source string, r *http.Request, body []byte, secret string) error {
	if secret == "" {
		return errors.New("no webhook secret; run `corgi agent watch hooks`")
	}
	switch source {
	case "linear":
		return verifyHMAC(r.Header.Get("Linear-Signature"), body, secret)
	case "github":
		return verifyHMAC(strings.TrimPrefix(r.Header.Get("X-Hub-Signature-256"), "sha256="), body, secret)
	case "gitlab":
		if hmac.Equal([]byte(r.Header.Get("X-Gitlab-Token")), []byte(secret)) {
			return nil
		}
		return ErrBadSignature
	case "jira":
		if hmac.Equal([]byte(r.URL.Query().Get("token")), []byte(secret)) {
			return nil
		}
		return ErrBadSignature
	}
	return fmt.Errorf("unknown webhook source %q", source)
}

func verifyHMAC(got string, body []byte, secret string) error {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	if hmac.Equal([]byte(strings.ToLower(got)), []byte(want)) {
		return nil
	}
	return ErrBadSignature
}

// ParseHook turns a webhook body into events. Unknown payloads yield none,
// never an error: a service sends more event types than we care about.
func ParseHook(source string, r *http.Request, body []byte, me string) ([]Event, error) {
	switch source {
	case "linear":
		return parseLinearHook(body, me)
	case "github":
		return parseGitHubHook(r.Header.Get("X-GitHub-Event"), body, me)
	case "gitlab":
		return parseGitLabHook(body, me)
	case "jira":
		return parseJiraHook(body, me)
	}
	return nil, fmt.Errorf("unknown webhook source %q", source)
}

func parseLinearHook(body []byte, me string) ([]Event, error) {
	var p struct {
		Action string `json:"action"`
		Type   string `json:"type"`
		Data   struct {
			ID         string                  `json:"id"`
			Identifier string                  `json:"identifier"`
			Title      string                  `json:"title"`
			URL        string                  `json:"url"`
			Body       string                  `json:"body"`
			CreatedAt  string                  `json:"createdAt"`
			State      struct{ Name string }   `json:"state"`
			Labels     []struct{ Name string } `json:"labels"`
			Assignee   *struct {
				ID    string `json:"id"`
				Name  string `json:"name"`
				Email string `json:"email"`
			} `json:"assignee"`
			User  *struct{ ID, Name, Email string } `json:"user"`
			Issue *struct {
				Identifier string                            `json:"identifier"`
				Title      string                            `json:"title"`
				URL        string                            `json:"url"`
				Assignee   *struct{ ID, Name, Email string } `json:"assignee"`
			} `json:"issue"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, err
	}
	at := hookTime(p.Data.CreatedAt)
	switch {
	case p.Type == "Issue" && p.Action == "create":
		e := Event{Key: "linear:" + p.Data.Identifier, Source: "linear", Kind: KindIssueNew, Ref: p.Data.Identifier,
			Title: p.Data.Title, URL: p.Data.URL, State: p.Data.State.Name, At: at, Body: clip(p.Data.Body, 200)}
		for _, l := range p.Data.Labels {
			e.Labels = append(e.Labels, l.Name)
		}
		if p.Data.Assignee != nil {
			e.Assignee = p.Data.Assignee.Name
			e.Mine = isMe(me, p.Data.Assignee.ID, p.Data.Assignee.Email, p.Data.Assignee.Name)
		}
		return []Event{e}, nil
	case p.Type == "Comment" && p.Action == "create" && p.Data.Issue != nil:
		if p.Data.User != nil && isMe(me, p.Data.User.ID, p.Data.User.Email, p.Data.User.Name) {
			return nil, nil
		}
		e := Event{Key: "linear:" + p.Data.Issue.Identifier + ":c" + p.Data.ID, Source: "linear", Kind: KindIssueComment,
			Ref: p.Data.Issue.Identifier, Title: p.Data.Issue.Title, URL: p.Data.Issue.URL, Body: clip(p.Data.Body, 200), At: at}
		if p.Data.User != nil {
			e.Author = p.Data.User.Name
		}
		if a := p.Data.Issue.Assignee; a != nil {
			e.Mine = isMe(me, a.ID, a.Email, a.Name)
		}
		return []Event{e}, nil
	}
	return nil, nil
}

func parseGitHubHook(event string, body []byte, me string) ([]Event, error) {
	var p struct {
		Action string `json:"action"`
		Repo   struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		PR *struct {
			Number  int                    `json:"number"`
			Title   string                 `json:"title"`
			HTMLURL string                 `json:"html_url"`
			User    struct{ Login string } `json:"user"`
		} `json:"pull_request"`
		Issue *struct {
			Number  int                    `json:"number"`
			Title   string                 `json:"title"`
			HTMLURL string                 `json:"html_url"`
			User    struct{ Login string } `json:"user"`
			PR      *struct{}              `json:"pull_request"`
		} `json:"issue"`
		Comment *struct {
			ID        int64                  `json:"id"`
			Body      string                 `json:"body"`
			CreatedAt string                 `json:"created_at"`
			User      struct{ Login string } `json:"user"`
		} `json:"comment"`
		Review *struct {
			ID          int64                  `json:"id"`
			Body        string                 `json:"body"`
			State       string                 `json:"state"`
			SubmittedAt string                 `json:"submitted_at"`
			User        struct{ Login string } `json:"user"`
		} `json:"review"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, err
	}
	if p.Action != "created" && p.Action != "submitted" {
		return nil, nil
	}
	var number int
	var title, url, owner string
	switch {
	case p.PR != nil:
		number, title, url, owner = p.PR.Number, p.PR.Title, p.PR.HTMLURL, p.PR.User.Login
	case p.Issue != nil && p.Issue.PR != nil:
		number, title, url, owner = p.Issue.Number, p.Issue.Title, p.Issue.HTMLURL, p.Issue.User.Login
	default:
		return nil, nil
	}
	ref := fmt.Sprintf("%s#%d", p.Repo.FullName, number)
	mine := isMe(me, owner)
	switch event {
	case "pull_request_review":
		if p.Review == nil || isMe(me, p.Review.User.Login) {
			return nil, nil
		}
		return []Event{{Key: fmt.Sprintf("github:%s:r%d", ref, p.Review.ID), Source: "github", Kind: KindPRReview, Ref: ref,
			Title: title, URL: url, Body: clip(p.Review.Body, 200), Author: p.Review.User.Login, State: p.Review.State,
			Mine: mine, At: hookTime(p.Review.SubmittedAt)}}, nil
	case "pull_request_review_comment", "issue_comment":
		if p.Comment == nil || isMe(me, p.Comment.User.Login) {
			return nil, nil
		}
		return []Event{{Key: fmt.Sprintf("github:%s:c%d", ref, p.Comment.ID), Source: "github", Kind: KindPRComment, Ref: ref,
			Title: title, URL: url, Body: clip(p.Comment.Body, 200), Author: p.Comment.User.Login, Mine: mine, At: hookTime(p.Comment.CreatedAt)}}, nil
	}
	return nil, nil
}

func parseGitLabHook(body []byte, me string) ([]Event, error) {
	var p struct {
		ObjectKind string                    `json:"object_kind"`
		User       struct{ Username string } `json:"user"`
		Project    struct {
			PathWithNamespace string `json:"path_with_namespace"`
		} `json:"project"`
		Attrs struct {
			ID           int64  `json:"id"`
			Note         string `json:"note"`
			NoteableType string `json:"noteable_type"`
			CreatedAt    string `json:"created_at"`
			URL          string `json:"url"`
		} `json:"object_attributes"`
		MR *struct {
			IID      int    `json:"iid"`
			Title    string `json:"title"`
			URL      string `json:"url"`
			AuthorID int    `json:"author_id"`
		} `json:"merge_request"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, err
	}
	if p.ObjectKind != "note" || p.Attrs.NoteableType != "MergeRequest" || p.MR == nil || isMe(me, p.User.Username) {
		return nil, nil
	}
	ref := fmt.Sprintf("%s!%d", p.Project.PathWithNamespace, p.MR.IID)
	return []Event{{Key: fmt.Sprintf("gitlab:%s:c%d", ref, p.Attrs.ID), Source: "gitlab", Kind: KindPRComment, Ref: ref,
		Title: p.MR.Title, URL: p.Attrs.URL, Body: clip(p.Attrs.Note, 200), Author: p.User.Username, Mine: true, At: hookTime(p.Attrs.CreatedAt)}}, nil
}

func parseJiraHook(body []byte, me string) ([]Event, error) {
	var p struct {
		Event string `json:"webhookEvent"`
		Issue struct {
			Key    string `json:"key"`
			Self   string `json:"self"`
			Fields struct {
				Summary  string                `json:"summary"`
				Labels   []string              `json:"labels"`
				Created  string                `json:"created"`
				Status   struct{ Name string } `json:"status"`
				Assignee *struct {
					AccountID    string `json:"accountId"`
					EmailAddress string `json:"emailAddress"`
					DisplayName  string `json:"displayName"`
				} `json:"assignee"`
			} `json:"fields"`
		} `json:"issue"`
		Comment *struct {
			ID      string `json:"id"`
			Created string `json:"created"`
			Author  struct {
				AccountID   string `json:"accountId"`
				DisplayName string `json:"displayName"`
			} `json:"author"`
			Body json.RawMessage `json:"body"`
		} `json:"comment"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, err
	}
	url := jiraBrowseURL(p.Issue.Self, p.Issue.Key)
	mine := false
	if a := p.Issue.Fields.Assignee; a != nil {
		mine = isMe(me, a.AccountID, a.EmailAddress, a.DisplayName)
	}
	switch p.Event {
	case "jira:issue_created":
		e := Event{Key: "jira:" + p.Issue.Key, Source: "jira", Kind: KindIssueNew, Ref: p.Issue.Key, Title: p.Issue.Fields.Summary,
			URL: url, Labels: p.Issue.Fields.Labels, State: p.Issue.Fields.Status.Name, Mine: mine, At: hookTime(p.Issue.Fields.Created)}
		if a := p.Issue.Fields.Assignee; a != nil {
			e.Assignee = a.DisplayName
		}
		return []Event{e}, nil
	case "comment_created":
		if p.Comment == nil || isMe(me, p.Comment.Author.AccountID, p.Comment.Author.DisplayName) {
			return nil, nil
		}
		return []Event{{Key: "jira:" + p.Issue.Key + ":c" + p.Comment.ID, Source: "jira", Kind: KindIssueComment, Ref: p.Issue.Key,
			Title: p.Issue.Fields.Summary, URL: url, Body: clip(jiraText(p.Comment.Body), 200), Author: p.Comment.Author.DisplayName,
			Mine: mine, At: hookTime(p.Comment.Created)}}, nil
	}
	return nil, nil
}

// jiraText flattens an Atlassian document (or a plain string) to text.
func jiraText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var doc struct {
		Content []struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return ""
	}
	var parts []string
	for _, para := range doc.Content {
		for _, c := range para.Content {
			if c.Text != "" {
				parts = append(parts, c.Text)
			}
		}
	}
	return strings.Join(parts, " ")
}

func jiraBrowseURL(self, key string) string {
	if i := strings.Index(self, "/rest/"); i > 0 {
		return self[:i] + "/browse/" + key
	}
	return ""
}

// isMe: any of the ids the service gives for a person against what we know.
func isMe(me string, ids ...string) bool {
	if me == "" {
		return false
	}
	for _, id := range ids {
		if id != "" && strings.EqualFold(id, me) {
			return true
		}
	}
	return false
}

func hookTime(s string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.000-0700", "2006-01-02 15:04:05 UTC"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Now()
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func parseTime(s string) time.Time { return hookTime(s) }
