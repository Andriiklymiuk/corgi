package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/pairing"
	"andriiklymiuk/corgi/utils/agent/rooms"
	"andriiklymiuk/corgi/utils/agent/watch"
)

type pairedDevice struct {
	name, token string
}

func roomsServer(t *testing.T) (string, *httptest.Server, func(name, role string) pairedDevice) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	home, err := agentDir()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(home, 0o700)
	store := pairing.StorePath(home)
	mux := http.NewServeMux()
	for path, h := range map[string]http.HandlerFunc{"/launch/rooms": launchRoomsHandler, "/launch/task/next": launchTaskNextHandler, "/launch/task/renew": launchTaskRenewHandler, "/launch/task/finish": launchTaskFinishHandler} {
		mux.Handle(path, launchAuth("", h, store))
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	pair := func(name, role string) pairedDevice {
		t.Helper()
		session, code, err := pairing.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		token, err := pairing.PairWithRole(store, session, code, name, "", role)
		if err != nil {
			t.Fatal(err)
		}
		return pairedDevice{name: name, token: token}
	}
	return home, srv, pair
}

func call(t *testing.T, srv *httptest.Server, d pairedDevice, method, path, body string, extra map[string]string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+d.token)
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestAPairedAgentTakesWorkUnderItsOwnName(t *testing.T) {
	home, srv, pair := roomsServer(t)
	agent := pair("ci-runner", "")
	tasks := watch.LoadTasks(home)
	first, _ := tasks.Add("fix the login", "", "api", "me", time.Now())
	_, _ = tasks.Add("second", "", "api", "me", time.Now())

	code, out := call(t, srv, agent, http.MethodPost, "/launch/task/next", `{"as":"someone-else","lease":"30m"}`, map[string]string{deviceHeader: "the-boss"})
	if code != http.StatusOK || out["ref"] != first.Ref() {
		t.Fatalf("next = %d %v", code, out)
	}
	held, _ := watch.LoadTasks(home).Find(first.Ref())
	if held.Owner != "ci-runner" || held.State != "Doing" {
		t.Fatalf("a paired agent is named by its pairing, never by what it says or a header it sends: %+v", held)
	}
	if code, out := call(t, srv, agent, http.MethodPost, "/launch/task/renew", `{"task":"`+first.Ref()+`","lease":"2h"}`, nil); code != http.StatusOK || out["leaseUntil"] == nil {
		t.Fatalf("renew = %d %v", code, out)
	}
	other := pair("other-runner", "")
	if code, _ := call(t, srv, other, http.MethodPost, "/launch/task/renew", `{"task":"`+first.Ref()+`"}`, nil); code != http.StatusConflict {
		t.Fatalf("another device renewed it: %d", code)
	}
	if code, out := call(t, srv, other, http.MethodPost, "/launch/task/next", `{"tags":["nothing-has-this"],"wait":"1s"}`, nil); code != http.StatusOK || out["task"] != nil {
		t.Fatalf("nothing ready = %d %v", code, out)
	}
	for _, body := range []string{`{"lease":"forever"}`, `{"wait":"-1s"}`, `not json`} {
		if code, _ := call(t, srv, other, http.MethodPost, "/launch/task/next", body, nil); code != http.StatusBadRequest {
			t.Errorf("%s = %d", body, code)
		}
	}
	if code, _ := call(t, srv, other, http.MethodGet, "/launch/task/next", "", nil); code != http.StatusMethodNotAllowed {
		t.Errorf("GET next = %d", code)
	}
	if code, _ := call(t, srv, other, http.MethodPost, "/launch/task/finish", `{"task":"`+first.Ref()+`","result":"mine now"}`, nil); code != http.StatusConflict {
		t.Fatalf("another device finished it: %d", code)
	}
	if code, _ := call(t, srv, agent, http.MethodPost, "/launch/task/finish", `{"task":"TASK-99"}`, nil); code != http.StatusNotFound {
		t.Fatalf("finish a missing task = %d", code)
	}
	if code, out := call(t, srv, agent, http.MethodPost, "/launch/task/finish", `{"task":"`+first.Ref()+`","result":"fixed in #8"}`, nil); code != http.StatusOK || out["state"] != "Done" {
		t.Fatalf("finish = %d %v", code, out)
	}
	finished, _ := watch.LoadTasks(home).Find(first.Ref())
	if len(finished.Comments) != 1 || finished.Comments[0].By != "ci-runner" || finished.WantsRun() {
		t.Fatalf("finished = %+v", finished)
	}
	viewer := pair("teammate", pairing.RoleViewer)
	if code, _ := call(t, srv, viewer, http.MethodPost, "/launch/task/next", `{}`, nil); code != http.StatusForbidden {
		t.Errorf("a read-only device took work: %d", code)
	}
}

func TestRoomsOverHTTPFollowTheGrants(t *testing.T) {
	home, srv, pair := roomsServer(t)
	phone := pair("phone", "")
	agent := pair("ci-runner", "")
	viewer := pair("teammate", pairing.RoleViewer)

	if code, out := call(t, srv, phone, http.MethodPost, "/launch/rooms", `{"room":"security/findings","text":"seen it","as":"liar"}`, nil); code != http.StatusOK {
		t.Fatalf("a full device with no grant naming it posts: %d %v", code, out)
	}
	msgs, _ := rooms.Read(home, rooms.Query{Room: "security/findings"})
	if len(msgs) != 1 || msgs[0].By != "phone" {
		t.Fatalf("the author is the paired device: %+v", msgs)
	}
	if code, _ := call(t, srv, viewer, http.MethodGet, "/launch/rooms?room=security/findings", "", nil); code != http.StatusForbidden {
		t.Fatalf("a read-only device read a room with no grant: %d", code)
	}

	if _, err := rooms.AddGrant(home, "ci-runner", "security/=read,post", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := rooms.AddGrant(home, "ci-runner", "security/secrets=none", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := rooms.AddGrant(home, "teammate", "all=read", time.Now()); err != nil {
		t.Fatal(err)
	}
	if code, _ := call(t, srv, agent, http.MethodPost, "/launch/rooms", `{"room":"security/findings","text":"fixed","thread":"m1"}`, nil); code != http.StatusOK {
		t.Fatalf("a granted post = %d", code)
	}
	if code, out := call(t, srv, agent, http.MethodGet, "/launch/rooms?room=security/secrets", "", nil); code != http.StatusForbidden || !strings.Contains(out["error"].(string), "security/secrets") {
		t.Fatalf("a path set to none = %d %v", code, out)
	}
	if code, _ := call(t, srv, agent, http.MethodPost, "/launch/rooms", `{"room":"deploys","text":"x"}`, nil); code != http.StatusForbidden {
		t.Fatalf("a room outside the grant = %d", code)
	}
	code, out := call(t, srv, viewer, http.MethodGet, "/launch/rooms?room=security/findings&after=m1", "", nil)
	if code != http.StatusOK || len(out["messages"].([]any)) != 1 {
		t.Fatalf("a viewer with a grant reads = %d %v", code, out)
	}
	if code, _ := call(t, srv, viewer, http.MethodPost, "/launch/rooms", `{"room":"security/findings","text":"x"}`, nil); code != http.StatusForbidden {
		t.Fatalf("a read-only device posted: %d", code)
	}
	_, list := call(t, srv, agent, http.MethodGet, "/launch/rooms", "", nil)
	if names := list["rooms"].([]any); len(names) != 1 {
		t.Fatalf("the list shows only rooms the device may read: %v", names)
	}
	start := time.Now()
	code, out = call(t, srv, agent, http.MethodGet, "/launch/rooms?room=security/findings&after=m2&wait=1s", "", nil)
	if code != http.StatusOK || len(out["messages"].([]any)) != 0 || time.Since(start) < 900*time.Millisecond {
		t.Fatalf("a wait with nothing new = %d %v after %v", code, out, time.Since(start))
	}
	for path, want := range map[string]int{"/launch/rooms?room=Bad Room": http.StatusBadRequest, "/launch/rooms?room=security/findings&wait=forever": http.StatusBadRequest} {
		if code, _ := call(t, srv, agent, http.MethodGet, strings.ReplaceAll(path, " ", "%20"), "", nil); code != want {
			t.Errorf("%s = %d", path, code)
		}
	}
	if code, _ := call(t, srv, agent, http.MethodDelete, "/launch/rooms", "", nil); code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE = %d", code)
	}
}

func TestQueueDurations(t *testing.T) {
	if d, ok := queueDuration("", time.Hour); !ok || d != 0 {
		t.Error("empty")
	}
	if d, ok := queueDuration("2h", time.Hour); !ok || d != time.Hour {
		t.Error("capped")
	}
	if _, ok := queueDuration("soon", time.Hour); ok {
		t.Error("words")
	}
}
