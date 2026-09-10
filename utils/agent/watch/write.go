package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Status is one column a ticket can sit in.
type Status struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Identity is whoever the token belongs to, so "assign it to me" has a name.
type Identity struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// Writer is a tracker corgi can change, not only read. Every method is a
// deliberate act someone asked for: nothing here runs on a poll.
type Writer interface {
	Name() string
	Statuses(ctx context.Context) ([]Status, error)
	Whoami(ctx context.Context) (Identity, error)
	Move(ctx context.Context, ref, status string) error
	Assign(ctx context.Context, ref, userID string) error
	Comment(ctx context.Context, ref, body string) error
}

// WriterFor is the tracker a workspace writes to, or nil when it has no
// token for one.
func WriterFor(s Secrets, tracker, project string) Writer {
	switch strings.ToLower(strings.TrimSpace(tracker)) {
	case "jira":
		if s.JiraToken != "" && s.JiraURL != "" {
			return NewJira(s, project)
		}
	case "linear":
		if s.Linear != "" {
			return NewLinear(s, project)
		}
	}
	return nil
}

// ---- Jira ----

// Statuses is what the project's workflow offers, deduped by name across
// issue types: the menu, not the transitions, which depend on where the
// issue currently sits.
func (j *Jira) Statuses(ctx context.Context) ([]Status, error) {
	if j.Project == "" {
		return nil, fmt.Errorf("jira: no project key to read statuses from")
	}
	var out []struct {
		Statuses []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"statuses"`
	}
	if err := j.get(ctx, "/rest/api/3/project/"+j.Project+"/statuses", nil, &out); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var statuses []Status
	for _, byType := range out {
		for _, s := range byType.Statuses {
			if key := strings.ToLower(s.Name); !seen[key] {
				seen[key] = true
				statuses = append(statuses, Status{ID: s.ID, Name: s.Name})
			}
		}
	}
	return statuses, nil
}

func (j *Jira) Whoami(ctx context.Context) (Identity, error) {
	var me struct {
		AccountID   string `json:"accountId"`
		DisplayName string `json:"displayName"`
	}
	if err := j.get(ctx, "/rest/api/3/myself", nil, &me); err != nil {
		return Identity{}, err
	}
	return Identity{ID: me.AccountID, Name: me.DisplayName}, nil
}

// Move transitions an issue to the named status. Jira's transition ids are
// per-workflow-position, so the one to use is read from the issue itself
// rather than guessed from the cached status list.
func (j *Jira) Move(ctx context.Context, ref, status string) error {
	var available struct {
		Transitions []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			To   struct {
				Name string `json:"name"`
			} `json:"to"`
		} `json:"transitions"`
	}
	if err := j.get(ctx, "/rest/api/3/issue/"+ref+"/transitions", nil, &available); err != nil {
		return err
	}
	want := strings.ToLower(strings.TrimSpace(status))
	for _, t := range available.Transitions {
		if strings.ToLower(t.To.Name) == want || strings.ToLower(t.Name) == want {
			body := map[string]any{"transition": map[string]string{"id": t.ID}}
			return j.send(ctx, http.MethodPost, "/rest/api/3/issue/"+ref+"/transitions", body)
		}
	}
	var names []string
	for _, t := range available.Transitions {
		names = append(names, t.To.Name)
	}
	if len(names) == 0 {
		return fmt.Errorf("jira: %s cannot move anywhere from where it is", ref)
	}
	return fmt.Errorf("jira: %s cannot move to %q from where it is — it can go to: %s", ref, status, strings.Join(names, ", "))
}

func (j *Jira) Assign(ctx context.Context, ref, userID string) error {
	return j.send(ctx, http.MethodPut, "/rest/api/3/issue/"+ref+"/assignee", map[string]any{"accountId": userID})
}

func (j *Jira) Comment(ctx context.Context, ref, body string) error {
	return j.send(ctx, http.MethodPost, "/rest/api/3/issue/"+ref+"/comment", map[string]any{"body": adf(body)})
}

// adf is Jira Cloud's Atlassian Document Format: plain text is not accepted
// on this API, one paragraph per line is.
func adf(text string) map[string]any {
	var paragraphs []any
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		para := map[string]any{"type": "paragraph"}
		if line != "" {
			para["content"] = []any{map[string]any{"type": "text", "text": line}}
		}
		paragraphs = append(paragraphs, para)
	}
	return map[string]any{"type": "doc", "version": 1, "content": paragraphs}
}

func (j *Jira) send(ctx context.Context, method, path string, body any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	endpoint := strings.TrimRight(j.URL, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, endpoint, strings.NewReader(string(raw)))
	if err != nil {
		return fmt.Errorf("jira %s: %w", path, err)
	}
	req.SetBasicAuth(j.Email, j.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	client := j.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("jira %s: %w", path, err)
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("jira %s: HTTP %d: %s", path, resp.StatusCode, clip(string(answer), bodyMax))
	}
	return nil
}

// ---- Linear ----

func (l *Linear) Statuses(ctx context.Context) ([]Status, error) {
	filter := "" // every state the token can see, when no team is configured
	if l.Team != "" {
		filter = fmt.Sprintf("(filter: {team: {key: {eq: %s}}})", jsonString(l.Team))
	}
	var out struct {
		WorkflowStates struct {
			Nodes []struct {
				ID       string  `json:"id"`
				Name     string  `json:"name"`
				Position float64 `json:"position"`
			} `json:"nodes"`
		} `json:"workflowStates"`
	}
	if err := l.query(ctx, "query { workflowStates"+filter+" { nodes { id name position } } }", &out); err != nil {
		return nil, err
	}
	statuses := make([]Status, 0, len(out.WorkflowStates.Nodes))
	for _, n := range out.WorkflowStates.Nodes {
		statuses = append(statuses, Status{ID: n.ID, Name: n.Name})
	}
	return statuses, nil
}

func (l *Linear) Whoami(ctx context.Context) (Identity, error) {
	var out struct {
		Viewer struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"viewer"`
	}
	if err := l.query(ctx, "query { viewer { id name } }", &out); err != nil {
		return Identity{}, err
	}
	return Identity{ID: out.Viewer.ID, Name: out.Viewer.Name}, nil
}

func (l *Linear) Move(ctx context.Context, ref, status string) error {
	statuses, err := l.Statuses(ctx)
	if err != nil {
		return err
	}
	want := strings.ToLower(strings.TrimSpace(status))
	for _, s := range statuses {
		if strings.ToLower(s.Name) == want {
			return l.updateIssue(ctx, ref, "stateId: "+jsonString(s.ID))
		}
	}
	var names []string
	for _, s := range statuses {
		names = append(names, s.Name)
	}
	return fmt.Errorf("linear: no status %q — it has: %s", status, strings.Join(names, ", "))
}

func (l *Linear) Assign(ctx context.Context, ref, userID string) error {
	return l.updateIssue(ctx, ref, "assigneeId: "+jsonString(userID))
}

func (l *Linear) Comment(ctx context.Context, ref, body string) error {
	id, err := l.issueID(ctx, ref)
	if err != nil {
		return err
	}
	document := fmt.Sprintf("mutation { commentCreate(input: {issueId: %s, body: %s}) { success } }",
		jsonString(id), jsonString(body))
	var out struct {
		CommentCreate struct {
			Success bool `json:"success"`
		} `json:"commentCreate"`
	}
	if err := l.query(ctx, document, &out); err != nil {
		return err
	}
	if !out.CommentCreate.Success {
		return fmt.Errorf("linear: the comment on %s was refused", ref)
	}
	return nil
}

// issueID turns ABC-123 into the id the mutations take.
func (l *Linear) issueID(ctx context.Context, ref string) (string, error) {
	var out struct {
		Issue *struct {
			ID string `json:"id"`
		} `json:"issue"`
	}
	if err := l.query(ctx, "query { issue(id: "+jsonString(ref)+") { id } }", &out); err != nil {
		return "", err
	}
	if out.Issue == nil || out.Issue.ID == "" {
		return "", fmt.Errorf("linear: no issue %s", ref)
	}
	return out.Issue.ID, nil
}

func (l *Linear) updateIssue(ctx context.Context, ref, input string) error {
	id, err := l.issueID(ctx, ref)
	if err != nil {
		return err
	}
	document := fmt.Sprintf("mutation { issueUpdate(id: %s, input: {%s}) { success } }", jsonString(id), input)
	var out struct {
		IssueUpdate struct {
			Success bool `json:"success"`
		} `json:"issueUpdate"`
	}
	if err := l.query(ctx, document, &out); err != nil {
		return err
	}
	if !out.IssueUpdate.Success {
		return fmt.Errorf("linear: the change to %s was refused", ref)
	}
	return nil
}

// jsonString quotes a value for a GraphQL document. Everything user-supplied
// goes through it: a ticket key or a comment must never be able to close the
// string and add fields of its own.
func jsonString(s string) string {
	raw, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(raw)
}
