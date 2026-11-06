package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/watch"
)

func postToHandler(t *testing.T, handler http.HandlerFunc, path, body string, viewer bool) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if viewer {
		req.Header.Set(viewerHeader, "1")
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func TestAPageTaskStartsOnlyWhereTheWorkspaceRunsFixes(t *testing.T) {
	dir, _ := watchFixture(t, &config.WatchConfig{Enabled: true, Action: "notify"})
	rec, out := postToHandler(t, launchTaskHandler, "/launch/task", `{"title":"go","workspace":"acme-stack","run":true}`, false)
	if rec.Code != http.StatusBadRequest || !strings.Contains(out["error"].(string), "corgi agent level 3") {
		t.Fatalf("%d %v", rec.Code, out)
	}
	if rec, _ := postToHandler(t, launchTaskHandler, "/launch/task", `{"title":"later","workspace":"acme-stack"}`, false); rec.Code != http.StatusOK {
		t.Fatalf("a task for later: %d", rec.Code)
	}
	tasks := watch.LoadTasks(dir).Tasks
	if len(tasks) != 1 || tasks[0].Run {
		t.Fatalf("tasks: %+v", tasks)
	}
}

func TestAPageTaskIsMarkedToRunAtLevelThree(t *testing.T) {
	dir, _ := watchFixture(t, &config.WatchConfig{Enabled: true, Action: "fix"})
	rec, out := postToHandler(t, launchTaskHandler, "/launch/task", `{"title":"go","workspace":"acme-stack","run":true}`, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %v", rec.Code, out)
	}
	if task := watch.LoadTasks(dir).Tasks[0]; !task.Run {
		t.Fatalf("task: %+v", task)
	}
}

func TestACommentFromThePageReachesTheThreadAndAViewerCannotWrite(t *testing.T) {
	dir, _ := watchFixture(t, nil)
	task, _ := watch.LoadTasks(dir).Add("x", "", "acme-stack", "cli", time.Now())
	body := `{"key":"` + task.Key() + `","text":"still wrong","by":"maria"}`
	if rec, _ := postToHandler(t, launchTaskCommentHandler, "/launch/task/comment", body, true); rec.Code != http.StatusForbidden {
		t.Fatalf("a viewer wrote: %d", rec.Code)
	}
	if rec, _ := postToHandler(t, launchTaskHandler, "/launch/task", `{"title":"y"}`, true); rec.Code != http.StatusForbidden {
		t.Fatalf("a viewer added a task: %d", rec.Code)
	}
	rec, out := postToHandler(t, launchTaskCommentHandler, "/launch/task/comment", body, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %v", rec.Code, out)
	}
	got, _ := watch.LoadTasks(dir).Find(task.Ref())
	if len(got.Comments) != 1 || got.Comments[0].By != "maria" || !got.WantsRun() {
		t.Fatalf("task: %+v", got)
	}
	spoof := `{"key":"` + task.Key() + `","text":"hi","by":"agent"}`
	_, _ = postToHandler(t, launchTaskCommentHandler, "/launch/task/comment", spoof, false)
	got, _ = watch.LoadTasks(dir).Find(task.Ref())
	if got.Comments[1].By == watch.TaskAgent {
		t.Fatal("a page comment passed as the agent")
	}
}

func TestACoralogixAlertNeedsTheSecretAndLandsInItsWorkspace(t *testing.T) {
	dir, _ := watchFixture(t, nil)
	if err := watch.SaveSecrets(dir, watch.Secrets{HookSecret: "s3cret"}); err != nil {
		t.Fatal(err)
	}
	handler := watchHookHandler("coralogix")
	payload := `{"alert_name":"5xx spike","alert_id":"a1","description":"500s on /pay"}`
	if rec, _ := postToHandler(t, handler, "/hooks/coralogix?workspace=acme-stack&token=wrong", payload, false); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a wrong secret: %d", rec.Code)
	}
	rec, _ := postToHandler(t, handler, "/hooks/coralogix?workspace=acme-stack&token=s3cret", payload, false)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("the right secret: %d %s", rec.Code, rec.Body.String())
	}
}

func TestSentryIsAskedForOnlyWhereErrorsAreOnAndTheTokenIsThere(t *testing.T) {
	has := func(sources []watch.Source) bool {
		for _, s := range sources {
			if s.Name() == "sentry" {
				return true
			}
		}
		return false
	}
	on := &config.WatchConfig{Errors: true, SentryOrg: "acme"}
	if !has(watchSources(watch.Rules{Enabled: true, Errors: true}, on, watch.Secrets{Sentry: "t"})) {
		t.Fatal("no sentry source")
	}
	if has(watchSources(watch.Rules{Enabled: true}, &config.WatchConfig{SentryOrg: "acme"}, watch.Secrets{Sentry: "t"})) {
		t.Fatal("sentry was read with errors off")
	}
	if has(watchSources(watch.Rules{Enabled: true, Errors: true}, on, watch.Secrets{})) {
		t.Fatal("sentry was read with no token")
	}
}

func TestATaskForAPeerNeedsAKnownPeerAndAWorkspace(t *testing.T) {
	dir, _ := watchFixture(t, nil)
	if _, err := addTaskOnPeer(context.Background(), dir, "build-1", "x", "", "api", false); err == nil || !strings.Contains(err.Error(), "no peer") {
		t.Fatalf("unknown peer: %v", err)
	}
}
