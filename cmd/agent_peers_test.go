package cmd

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/pairing"
	"andriiklymiuk/corgi/utils/agent/peers"
	"andriiklymiuk/corgi/utils/agent/watch"
)

// Laptop B is this process (CORGI_DATA_DIR); laptop A is a bare /pair
// window with role peer. B joins A; then A, as B's paired peer, pulses B.
func TestPeerJoinsThroughAPairWindowAndPulses(t *testing.T) {
	dataB := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", dataB)
	dirB, err := agentDir()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(dirB, 0o700)

	// A: a pairing window opened for a laptop.
	sessionA, codeA, err := pairing.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	storeA := pairing.StorePath(t.TempDir())
	muxA := http.NewServeMux()
	muxA.Handle("/pair", pairingHandlerWithRole(sessionA, storeA, pairing.RolePeer))
	srvA := httptest.NewServer(muxA)
	defer srvA.Close()

	p, err := peers.Join(context.Background(), dirB, srvA.URL, codeA)
	if err != nil {
		t.Fatal(err)
	}
	if p.Token == "" || p.Key == "" || !strings.HasPrefix(p.Token, pairing.TokenPrefix) {
		t.Fatalf("join: %+v", p)
	}
	devA, _ := pairing.Load(storeA)
	if len(devA.Devices) != 1 || !devA.Devices[0].Peer() || !devA.Devices[0].Encrypted() {
		t.Fatalf("A holds B as an encrypted peer: %+v", devA.Devices)
	}
	stored, _ := peers.Load(peers.Path(dirB))
	if _, ok := stored.Find(p.Name); !ok {
		t.Fatal("B remembers A")
	}

	// A phone's window does not make a peer.
	sessionPhone, codePhone, _ := pairing.NewSession()
	muxP := http.NewServeMux()
	muxP.Handle("/pair", pairingHandler(sessionPhone, pairing.StorePath(t.TempDir())))
	srvP := httptest.NewServer(muxP)
	defer srvP.Close()
	if _, err := peers.Join(context.Background(), dirB, srvP.URL, codePhone); err == nil || !strings.Contains(err.Error(), "not for a laptop") {
		t.Fatalf("a phone window is refused: %v", err)
	}

	// Now A pairs into B the same way, so it can pulse B.
	sessionB, codeB, _ := pairing.NewSession()
	storeB := pairing.StorePath(dirB)
	muxB := http.NewServeMux()
	muxB.Handle("/pair", pairingHandlerWithRole(sessionB, storeB, pairing.RolePeer))
	muxB.Handle("/launch/peers/pulse", launchAuth("", http.HandlerFunc(launchPeersPulseHandler), storeB))
	muxB.Handle("/launch/peers/join", launchAuth("", http.HandlerFunc(launchPeersJoinHandler), storeB))
	muxB.Handle("/launch/board", launchAuth("", http.HandlerFunc(launchBoardHandler), storeB))
	srvB := httptest.NewServer(muxB)
	defer srvB.Close()
	dirA := t.TempDir()
	// A's own key lives in its agent dir; Join reads it from there.
	fromA, err := peers.Join(context.Background(), dirA, srvB.URL, codeB)
	if err != nil {
		t.Fatal(err)
	}
	// B must list A under the name A paired with (its hostname == ours here).
	_ = peers.Update(peers.Path(dirB), func(s *peers.Store) {
		for i := range s.Peers {
			s.Peers[i].Name = peers.Me()
		}
	})
	_ = os.MkdirAll(filepath.Dir(daemon.WatchIdentitiesPath(dirB)), 0o700)
	_ = os.WriteFile(daemon.WatchIdentitiesPath(dirB), []byte(`["linear/api"]`), 0o600)

	var heard peers.Pulse
	client := peers.NewClient(fromA)
	if err := client.Call(context.Background(), "/launch/peers/pulse", peers.Pulse{Name: "A", Watches: []string{"linear/api", "github/x/y"}, Lead: true, At: time.Now().UnixMilli()}, &heard); err != nil {
		t.Fatal(err)
	}
	if heard.Name != peers.Me() || len(heard.Watches) != 1 || heard.Watches[0] != "linear/api" {
		t.Fatalf("B answers with what it watches: %+v", heard)
	}
	after, _ := peers.Load(peers.Path(dirB))
	if len(after.Peers) != 1 || !after.Peers[0].Alive(time.Now()) || !after.Peers[0].Lead || len(after.Peers[0].Watches) != 2 {
		t.Fatalf("B recorded the pulse: %+v", after.Peers)
	}
	if got := peers.Leader(after, "zzz-"+peers.Me(), []string{"linear/api"}, time.Now()); got != peers.Me() {
		t.Fatalf("the peer that leads wins: %q", got)
	}

	// A peer token opens nothing else.
	if err := client.Call(context.Background(), "/launch/board", map[string]any{}, nil); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("board refused to a peer: %v", err)
	}
	if err := client.Call(context.Background(), "/launch/peers/join", map[string]any{"url": srvA.URL, "code": "X"}, nil); err == nil || !strings.Contains(err.Error(), "pairs back") {
		t.Fatalf("a peer cannot send B to a third laptop: %v", err)
	}
	if k, _ := base64.StdEncoding.DecodeString(fromA.Key); len(k) != 32 {
		t.Fatal("a 32-byte shared key")
	}
}

func TestTunnelNamePerLaptopAndForeignCredentials(t *testing.T) {
	if got := defaultTunnelName("corgi-agent"); got != "corgi-agent" {
		t.Fatalf("saved name wins: %q", got)
	}
	if got := defaultTunnelName(""); !strings.HasPrefix(got, "corgi-") || got == "corgi-" {
		t.Fatalf("a name from the hostname: %q", got)
	}
	list := "ID                                   NAME         CREATED              CONNECTIONS\n" +
		"8f1c2a3b-1111-2222-3333-444455556666 corgi-home   2026-09-01T10:00:00Z 1xfra\n" +
		"9a1c2a3b-1111-2222-3333-444455556666 corgi-agent  2026-08-01T10:00:00Z\n"
	if tunnelIDIn(list, "corgi-agent") != "9a1c2a3b-1111-2222-3333-444455556666" || tunnelIDIn(list, "corgi") != "" {
		t.Fatal("id by exact name")
	}
	old := tunnelCredentialsExist
	defer func() { tunnelCredentialsExist = old }()
	tunnelCredentialsExist = func(string) bool { return false }
	run := func(_ string, args ...string) (string, error) {
		if len(args) > 1 && args[1] == "list" {
			return list, nil
		}
		return "", nil
	}
	err := setupCloudflaredTunnel(run, func(string) error { return nil }, "corgi-agent", "corgi.example.com", false)
	if err == nil || !strings.Contains(err.Error(), "another one") {
		t.Fatalf("a tunnel made elsewhere is refused: %v", err)
	}
	if domainOf("home.corgi.example.com") != "example.com" || domainOf("example.com") != "example.com" {
		t.Fatal("domainOf")
	}
}

func TestCodexNotifyLineIsOursOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	changed, theirs, err := enableCodexNotify(path, "/usr/local/bin/corgi")
	if err != nil || !changed || theirs != "" {
		t.Fatalf("%v %v %q", changed, err, theirs)
	}
	raw, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(raw), `notify = ["/usr/local/bin/corgi", "agent", "event", "stop", "--agent", "codex", "--notify"]`) {
		t.Fatalf("%s", raw)
	}
	// Idempotent; a moved binary rewrites the line; a table stays below it.
	_ = os.WriteFile(path, append(raw, []byte("[model_providers.x]\nname = \"x\"\n")...), 0o600)
	if changed, _, _ := enableCodexNotify(path, "/usr/local/bin/corgi"); changed {
		t.Fatal("same line twice is no change")
	}
	if changed, _, _ := enableCodexNotify(path, "/opt/corgi"); !changed {
		t.Fatal("a new path rewrites our line")
	}
	raw, _ = os.ReadFile(path)
	if strings.Count(string(raw), "notify =") != 1 || !strings.Contains(string(raw), "[model_providers.x]") {
		t.Fatalf("%s", raw)
	}
	// Someone else's notify is left alone.
	_ = os.WriteFile(path, []byte("notify = [\"say\", \"done\"]\n"), 0o600)
	if changed, theirs, _ := enableCodexNotify(path, "/opt/corgi"); changed || theirs == "" {
		t.Fatal("their notify stays")
	}
	if removed, _ := disableCodexNotify(path); removed {
		t.Fatal("disable never removes theirs")
	}
	_ = os.WriteFile(path, []byte("notify = [\"/opt/corgi\", \"agent\", \"event\", \"stop\", \"--agent\", \"codex\", \"--notify\"]\nfoo = 1\n"), 0o600)
	if removed, _ := disableCodexNotify(path); !removed {
		t.Fatal("disable removes ours")
	}
	raw, _ = os.ReadFile(path)
	if string(raw) != "foo = 1\n" {
		t.Fatalf("%q", raw)
	}
}

// Two machines over the real pairing and the real sealed calls.
func TestAPeerCanHandOverATaskAndNothingMore(t *testing.T) {
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	home, err := agentDir()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(home, 0o700)
	session, code, _ := pairing.NewSession()
	store := pairing.StorePath(home)
	mux := http.NewServeMux()
	mux.Handle("/pair", pairingHandlerWithRole(session, store, pairing.RolePeer))
	mux.Handle("/launch/peers/tasks", launchAuth("", http.HandlerFunc(launchPeersTasksHandler), store))
	mux.Handle("/launch/task", launchAuth("", http.HandlerFunc(launchTaskHandler), store))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	other, err := peers.Join(context.Background(), t.TempDir(), srv.URL, code)
	if err != nil {
		t.Fatal(err)
	}
	client := peers.NewClient(other)
	devices, _ := pairing.Load(store)
	name := devices.Devices[0].Name

	var added struct {
		Ref string `json:"ref"`
	}
	said := []watch.TaskComment{{By: "you", Text: "the total is wrong", At: time.Now().Add(-time.Hour)}}
	if err := client.Call(context.Background(), "/launch/task", map[string]any{"title": "Fix the export", "body": "b", "comments": said}, &added); err != nil {
		t.Fatalf("a peer hands over a task: %v", err)
	}
	here := watch.LoadTasks(home)
	task, ok := here.Find(added.Ref)
	if !ok || task.By != name || len(task.Comments) != 1 || task.WantsRun() {
		t.Fatalf("it arrives with its thread, and wakes nothing by itself: %+v", task)
	}

	var reply struct {
		Taken []watch.TakenTask `json:"taken"`
	}
	sent := watch.TaskCopies{Fixes: []string{"api"}, Tasks: []watch.Task{{ID: 42, Title: "waiting", Workspace: "api", Run: true, State: "Todo"}}}
	if err := client.Call(context.Background(), "/launch/peers/tasks", sent, &reply); err != nil {
		t.Fatalf("a peer leaves a copy: %v", err)
	}
	if copies, ok := watch.LoadTaskCopies(home, name); !ok || len(copies.Tasks) != 1 || copies.Fixes[0] != "api" || copies.At.IsZero() {
		t.Fatalf("%+v", copies)
	}
	if len(reply.Taken) != 0 {
		t.Fatalf("nothing taken yet: %+v", reply.Taken)
	}
	if _, err := watch.LoadTasks(home).AddCopy(sent.Tasks[0], name, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := client.Call(context.Background(), "/launch/peers/tasks", sent, &reply); err != nil {
		t.Fatal(err)
	}
	if len(reply.Taken) != 1 || reply.Taken[0].ID != 42 {
		t.Fatalf("it hears this machine took it: %+v", reply.Taken)
	}
}

func TestAPeerMayNotReadTheTasksOrReachAnythingElse(t *testing.T) {
	for _, c := range []struct {
		method, path string
		may          bool
	}{
		{http.MethodPost, "/launch/peers/pulse", true},
		{http.MethodPost, "/launch/peers/tasks", true},
		{http.MethodPost, "/launch/task", true},
		{http.MethodGet, "/launch/task", false},
		{http.MethodGet, "/launch/peers/tasks", false},
		{http.MethodPost, "/launch/task/comment", false},
		{http.MethodGet, "/launch/board", false},
		{http.MethodPost, "/launch/new", false},
	} {
		if got := peerMay(c.method, c.path); got != c.may {
			t.Errorf("%s %s: %v", c.method, c.path, got)
		}
	}
}

func TestPeersTasksRefusesAnythingButAPeer(t *testing.T) {
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	rec := httptest.NewRecorder()
	launchPeersTasksHandler(rec, httptest.NewRequest(http.MethodPost, "/launch/peers/tasks", strings.NewReader("{}")))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("%d", rec.Code)
	}
}

func TestATaskThatIsOverOrRunningStaysPut(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		task watch.Task
		why  string
	}{
		{watch.Task{ID: 1, State: "Done", Workspace: "w"}, "nothing to send"},
		{watch.Task{ID: 2, State: "Canceled", Workspace: "w"}, "nothing to send"},
		{watch.Task{ID: 3, State: "Doing", Workspace: "w"}, "running here"},
		{watch.Task{ID: 4, State: "Todo", Workspace: "w", MovedTo: "b as TASK-1"}, "already moved"},
		{watch.Task{ID: 5, State: "Todo"}, "no workspace"},
		{watch.Task{ID: 6, State: "Todo", Workspace: "w"}, ""},
		{watch.Task{ID: 7, State: "Review", Workspace: "w"}, ""},
	} {
		got := taskSendRefusal(dir, c.task)
		if (c.why == "") != (got == "") || !strings.Contains(got, c.why) {
			t.Errorf("%+v: %q", c.task, got)
		}
	}
}
