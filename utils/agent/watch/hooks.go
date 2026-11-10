package watch

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var ErrBadSignature = errors.New("webhook signature does not match")

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
	case "jira", "coralogix":
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

// HookIdentity is who "me" is on the source a webhook came from: the name the
// payload carries, and for GitLab the numeric user id its merge request
// author is given by.
type HookIdentity struct {
	Me string
	ID string
}

func ParseHook(source string, r *http.Request, body []byte, me string) ([]Event, error) {
	return ParseHookAs(source, r, body, HookIdentity{Me: me})
}

func ParseHookAs(source string, r *http.Request, body []byte, who HookIdentity) ([]Event, error) {
	me := who.Me
	switch source {
	case "linear":
		return parseLinearHook(body, me)
	case "github":
		return parseGitHubHook(r.Header.Get("X-GitHub-Event"), body, me)
	case "gitlab":
		return parseGitLabHook(body, who)
	case "jira":
		return parseJiraHook(body, me)
	case "coralogix":
		return parseAlertHook(source, r, body, time.Now())
	}
	return nil, fmt.Errorf("unknown webhook source %q", source)
}

func parseLinearHook(body []byte, me string) ([]Event, error) {
	var p struct {
		Action string         `json:"action"`
		Type   string         `json:"type"`
		Data   linearHookData `json:"data"`
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

type linearHookName struct{ Name string }

type linearHookDataAssignee struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type linearHookDataUser struct{ ID, Name, Email string }

type linearHookDataIssue struct {
	Identifier string              `json:"identifier"`
	Title      string              `json:"title"`
	URL        string              `json:"url"`
	Assignee   *linearHookDataUser `json:"assignee"`
}

type linearHookData struct {
	ID         string                  `json:"id"`
	Identifier string                  `json:"identifier"`
	Title      string                  `json:"title"`
	URL        string                  `json:"url"`
	Body       string                  `json:"body"`
	CreatedAt  string                  `json:"createdAt"`
	State      linearHookName          `json:"state"`
	Labels     []linearHookName        `json:"labels"`
	Assignee   *linearHookDataAssignee `json:"assignee"`
	User       *linearHookDataUser     `json:"user"`
	Issue      *linearHookDataIssue    `json:"issue"`
}

func parseGitHubHook(event string, body []byte, me string) ([]Event, error) {
	var p struct {
		Action  string             `json:"action"`
		Repo    gitHubHookRepo     `json:"repository"`
		PR      *gitHubHookPR      `json:"pull_request"`
		Issue   *gitHubHookIssue   `json:"issue"`
		Comment *gitHubHookComment `json:"comment"`
		Review  *gitHubHookReview  `json:"review"`
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
		if p.Review == nil || isMe(me, p.Review.User.Login) || githubBot(p.Review.User.Login, p.Review.User.Type) {
			return nil, nil
		}
		return []Event{{Key: fmt.Sprintf("github:%s:r%d", ref, p.Review.ID), Source: "github", Kind: KindPRReview, Ref: ref,
			Title: title, URL: url, Body: clip(p.Review.Body, 200), Author: p.Review.User.Login, State: p.Review.State,
			Review: strings.ToLower(p.Review.State), Mine: mine, At: hookTime(p.Review.SubmittedAt)}}, nil
	case "pull_request_review_comment", "issue_comment":
		if p.Comment == nil || isMe(me, p.Comment.User.Login) || githubBot(p.Comment.User.Login, p.Comment.User.Type) {
			return nil, nil
		}
		return []Event{{Key: fmt.Sprintf("github:%s:c%d", ref, p.Comment.ID), Source: "github", Kind: KindPRComment, Ref: ref,
			Title: title, URL: url, Body: clip(p.Comment.Body, 200), Author: p.Comment.User.Login, Mine: mine, At: hookTime(p.Comment.CreatedAt)}}, nil
	}
	return nil, nil
}

type gitHubHookRepo struct {
	FullName string `json:"full_name"`
}

type gitHubHookLogin struct{ Login string }

type gitHubHookPR struct {
	Number  int             `json:"number"`
	Title   string          `json:"title"`
	HTMLURL string          `json:"html_url"`
	User    gitHubHookLogin `json:"user"`
}

type gitHubHookIssue struct {
	Number  int             `json:"number"`
	Title   string          `json:"title"`
	HTMLURL string          `json:"html_url"`
	User    gitHubHookLogin `json:"user"`
	PR      *struct{}       `json:"pull_request"`
}

type gitHubHookActor struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

type gitHubHookComment struct {
	ID        int64           `json:"id"`
	Body      string          `json:"body"`
	CreatedAt string          `json:"created_at"`
	User      gitHubHookActor `json:"user"`
}

type gitHubHookReview struct {
	ID          int64           `json:"id"`
	Body        string          `json:"body"`
	State       string          `json:"state"`
	SubmittedAt string          `json:"submitted_at"`
	User        gitHubHookActor `json:"user"`
}

func parseGitLabHook(body []byte, who HookIdentity) ([]Event, error) {
	me := who.Me
	var p struct {
		ObjectKind string            `json:"object_kind"`
		User       gitLabHookUser    `json:"user"`
		Project    gitLabHookProject `json:"project"`
		Attrs      gitLabHookAttrs   `json:"object_attributes"`
		MR         *gitLabHookMR     `json:"merge_request"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, err
	}
	if p.ObjectKind != "note" || p.Attrs.NoteableType != "MergeRequest" || p.MR == nil || isMe(me, p.User.Username) || gitlabBot(p.User.Username, false) {
		return nil, nil
	}
	ref := fmt.Sprintf("%s!%d", p.Project.PathWithNamespace, p.MR.IID)
	// A project hook sends every note on every merge request. Only one on a
	// merge request I opened is mine to act on; without my id it is not
	// claimed, and the poll of my own merge requests still finds it.
	mine := who.ID != "" && strconv.FormatInt(p.MR.AuthorID, 10) == who.ID
	return []Event{{Key: "gitlab:note:" + strconv.FormatInt(p.Attrs.ID, 10), Source: "gitlab", Kind: KindPRComment, Ref: ref,
		Title: p.MR.Title, URL: p.Attrs.URL, Body: clip(p.Attrs.Note, 200), Author: p.User.Username, Mine: mine, State: p.MR.State,
		At: hookTime(p.Attrs.CreatedAt)}}, nil
}

type gitLabHookUser struct{ Username string }

type gitLabHookProject struct {
	PathWithNamespace string `json:"path_with_namespace"`
}

type gitLabHookAttrs struct {
	ID           int64  `json:"id"`
	Note         string `json:"note"`
	NoteableType string `json:"noteable_type"`
	CreatedAt    string `json:"created_at"`
	URL          string `json:"url"`
}

type gitLabHookMR struct {
	IID      int    `json:"iid"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	State    string `json:"state"`
	AuthorID int64  `json:"author_id"`
}

func parseJiraHook(body []byte, me string) ([]Event, error) {
	var p struct {
		Event   string           `json:"webhookEvent"`
		Issue   jiraHookIssue    `json:"issue"`
		Comment *jiraHookComment `json:"comment"`
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

type jiraHookFieldsAssignee struct {
	AccountID    string `json:"accountId"`
	EmailAddress string `json:"emailAddress"`
	DisplayName  string `json:"displayName"`
}

type jiraHookIssueFields struct {
	Summary  string                  `json:"summary"`
	Labels   []string                `json:"labels"`
	Created  string                  `json:"created"`
	Status   linearHookName          `json:"status"`
	Assignee *jiraHookFieldsAssignee `json:"assignee"`
}

type jiraHookIssue struct {
	Key    string              `json:"key"`
	Self   string              `json:"self"`
	Fields jiraHookIssueFields `json:"fields"`
}

type jiraHookCommentAuthor struct {
	AccountID   string `json:"accountId"`
	DisplayName string `json:"displayName"`
}

type jiraHookComment struct {
	ID      string                `json:"id"`
	Created string                `json:"created"`
	Author  jiraHookCommentAuthor `json:"author"`
	Body    json.RawMessage       `json:"body"`
}

func jiraText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var doc struct {
		Content []jiraTextDocContent `json:"content"`
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

type jiraTextDocContentContent struct {
	Text string `json:"text"`
}

type jiraTextDocContent struct {
	Content []jiraTextDocContentContent `json:"content"`
}

func jiraBrowseURL(self, key string) string {
	if i := strings.Index(self, "/rest/"); i > 0 {
		return self[:i] + "/browse/" + key
	}
	return ""
}

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

func githubBot(login, kind string) bool {
	return strings.EqualFold(kind, "Bot") || strings.HasSuffix(strings.ToLower(login), "[bot]")
}
