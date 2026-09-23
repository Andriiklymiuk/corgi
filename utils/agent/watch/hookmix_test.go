package watch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitLabHookClaimsOnlyMyMergeRequests(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/hooks/gitlab", nil)
	note := `{"object_kind":"note","user":{"username":"max"},"project":{"path_with_namespace":"acme/web"},
	  "object_attributes":{"id":44,"note":"rename it","noteable_type":"MergeRequest","created_at":"2026-09-09 10:00:00 UTC","url":"u"},
	  "merge_request":{"iid":3,"title":"Search","state":"opened","author_id":7}}`
	events, _ := ParseHookAs("gitlab", r, []byte(note), HookIdentity{Me: "andrii", ID: "7"})
	if len(events) != 1 || !events[0].Mine || events[0].Key != "gitlab:note:44" || events[0].State != "opened" {
		t.Fatalf("my merge request: %+v", events)
	}
	events, _ = ParseHookAs("gitlab", r, []byte(strings.Replace(note, `"author_id":7`, `"author_id":9`, 1)), HookIdentity{Me: "andrii", ID: "7"})
	if len(events) != 1 || events[0].Mine {
		t.Fatalf("a colleague's merge request is not mine to fix: %+v", events)
	}
	events, _ = ParseHookAs("gitlab", r, []byte(note), HookIdentity{Me: "andrii"})
	if len(events) != 1 || events[0].Mine {
		t.Fatalf("without my id nothing is claimed; the poll finds it: %+v", events)
	}
}

func TestPollAndWebhookNameTheSameComment(t *testing.T) {
	cases := map[string]string{
		"https://api.github.com/repos/acme/api/issues/comments/77":        "c77",
		"https://api.github.com/repos/acme/api/pulls/comments/78":         "c78",
		"https://api.github.com/repos/acme/api/pulls/12/reviews/5":        "r5",
		"https://api.github.com/repos/acme/api/pulls/12":                  "",
		"https://api.github.com/repos/acme/api/check-suites/12/something": "",
	}
	for url, want := range cases {
		if got := githubCommentKey(url); got != want {
			t.Errorf("%s → %q, want %q", url, got, want)
		}
	}
}

func TestInstallGitLabHookCreatesThenUpdates(t *testing.T) {
	var hooks []map[string]any
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.EscapedPath())
		if r.Header.Get("PRIVATE-TOKEN") != "tok" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(hooks)
		case http.MethodPost:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["note_events"] != true || body["push_events"] != false || body["token"] != "sec" {
				t.Errorf("body = %v", body)
			}
			body["id"] = 5
			hooks = append(hooks, body)
			w.WriteHeader(http.StatusCreated)
		case http.MethodPut:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()
	first := InstallGitLabHook(context.Background(), srv.URL, "tok", "acme/group/api", "https://x/hooks/gitlab", "sec")
	second := InstallGitLabHook(context.Background(), srv.URL, "tok", "acme/group/api", "https://x/hooks/gitlab", "sec")
	if first.Err != nil || first.Action != "created" || second.Err != nil || second.Action != "updated" {
		t.Fatalf("first %+v second %+v", first, second)
	}
	if calls[0] != "GET /api/v4/projects/acme%2Fgroup%2Fapi/hooks" || calls[3] != "PUT /api/v4/projects/acme%2Fgroup%2Fapi/hooks/5" {
		t.Fatalf("calls = %v", calls)
	}
	denied := InstallGitLabHook(context.Background(), srv.URL, "wrong", "acme/api", "https://x/hooks/gitlab", "sec")
	if denied.Err == nil || !strings.Contains(denied.Missing, "Maintainer") {
		t.Fatalf("denied = %+v", denied)
	}
}

func TestInstallGitHubHookSendsTheEventsCorgiReads(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[{"id":1,"config":{"url":"https://other/hook"}}]`))
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	prev := GitHubAPI
	GitHubAPI = srv.URL
	defer func() { GitHubAPI = prev }()
	res := InstallGitHubHook(context.Background(), "tok", "acme/api", "https://x/hooks/github", "sec")
	if res.Err != nil || res.Action != "created" || got["name"] != "web" {
		t.Fatalf("res %+v body %v", res, got)
	}
	cfg := got["config"].(map[string]any)
	if cfg["secret"] != "sec" || cfg["content_type"] != "json" || len(got["events"].([]any)) != 3 {
		t.Fatalf("body %v", got)
	}
}
