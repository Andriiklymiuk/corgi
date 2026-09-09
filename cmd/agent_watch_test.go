package cmd

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/watch"
	"andriiklymiuk/corgi/utils/agent/workspace"
)

// watchFixture: a home with one registered workspace at ws, watch enabled.
func watchFixture(t *testing.T, wc *config.WatchConfig) (dir, ws string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	dir = mustAgentDir()
	ws = filepath.Join(home, "dev", "acme-stack")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	registry, _ := workspace.Load(agentRegistryPath(dir))
	registry.Upsert(workspace.Workspace{ID: "acme-stack", AbsPath: ws, Status: workspace.StatusOK})
	if err := workspace.Save(agentRegistryPath(dir), registry); err != nil {
		t.Fatal(err)
	}
	user, _ := config.LoadUser(agentUserConfigPath(dir))
	if user.Workspaces == nil {
		user.Workspaces = map[string]config.WorkspaceConfig{}
	}
	user.Workspaces["acme-stack"] = config.WorkspaceConfig{Watch: wc}
	if err := writeUserConfig(agentUserConfigPath(dir), user); err != nil {
		t.Fatal(err)
	}
	return dir, ws
}

func TestLoadWatchSpecsFromConfigAndTokens(t *testing.T) {
	dir, ws := watchFixture(t, &config.WatchConfig{Enabled: true, Labels: []string{"bug"}, PRs: true, Action: "fix", Interval: "5m", Project: "ABC", Repos: []string{"acme/api"}})
	t.Setenv("LINEAR_API_KEY", "lin_x")
	t.Setenv("GITHUB_TOKEN", "gh_x")
	t.Setenv("JIRA_API_TOKEN", "")
	t.Setenv("GITLAB_TOKEN", "")
	specs, err := loadWatchSpecs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 {
		t.Fatalf("specs %+v", specs)
	}
	s := specs[0]
	if s.Workspace != "acme-stack" || s.Dir != ws || s.Action != "fix" || s.Interval != 5*time.Minute || s.Project != "ABC" {
		t.Fatalf("%+v", s)
	}
	if len(s.Sources) != 2 || s.Sources[0].Name() != "linear" || s.Sources[1].Name() != "github" {
		t.Fatalf("sources %v", s.Sources)
	}
	if !s.Rules.PRs || s.Rules.Labels[0] != "bug" {
		t.Fatalf("rules %+v", s.Rules)
	}

	// Disabled, or no tokens: still listed, so the status can say why.
	t.Setenv("LINEAR_API_KEY", "")
	t.Setenv("GITHUB_TOKEN", "")
	githubTokenSeam := os.Getenv("PATH")
	t.Setenv("PATH", "")
	defer t.Setenv("PATH", githubTokenSeam)
	specs, _ = loadWatchSpecs(dir)
	if len(specs) != 1 || len(specs[0].Sources) != 0 {
		t.Fatalf("without tokens: %+v", specs)
	}
}

func TestWatchEnableDisableWriteTheConfig(t *testing.T) {
	dir, ws := watchFixture(t, nil)
	wd, _ := os.Getwd()
	if err := os.Chdir(ws); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)

	f := agentWatchEnableCmd.Flags()
	_ = f.Set("labels", "bug, defect")
	_ = f.Set("prs", "true")
	_ = f.Set("action", "fix")
	_ = f.Set("interval", "2m")
	_ = f.Set("project", "ABC")
	defer func() {
		for _, name := range []string{"labels", "prs", "action", "interval", "project"} {
			_ = f.Set(name, "")
			f.Lookup(name).Changed = false
		}
	}()
	out := captureStdout(t, func() {
		if err := agentWatchEnableCmd.RunE(agentWatchEnableCmd, nil); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "watching acme-stack") || !strings.Contains(out, "labels bug,defect") || !strings.Contains(out, "→ fix") {
		t.Fatalf("enable said: %s", out)
	}
	user, _ := config.LoadUser(agentUserConfigPath(dir))
	wc := user.Workspaces["acme-stack"].Watch
	if wc == nil || !wc.Enabled || wc.Interval != "2m" || wc.Project != "ABC" || !wc.PRs || len(wc.Labels) != 2 {
		t.Fatalf("config %+v", wc)
	}
	_ = f.Set("action", "merge")
	if err := agentWatchEnableCmd.RunE(agentWatchEnableCmd, nil); err == nil {
		t.Fatal("action merge must be refused")
	}
	_ = f.Set("action", "")
	_ = f.Set("interval", "soon")
	if err := agentWatchEnableCmd.RunE(agentWatchEnableCmd, nil); err == nil {
		t.Fatal("a bad interval must be refused")
	}

	if err := agentWatchDisableCmd.RunE(agentWatchDisableCmd, nil); err != nil {
		t.Fatal(err)
	}
	user, _ = config.LoadUser(agentUserConfigPath(dir))
	if user.Workspaces["acme-stack"].Watch.Enabled {
		t.Fatal("still enabled")
	}

	os.Chdir(t.TempDir())
	if _, err := currentWorkspaceID(dir); err == nil {
		t.Fatal("outside a workspace must fail")
	}
}

func TestWatchStatusRunAndHooks(t *testing.T) {
	dir, _ := watchFixture(t, &config.WatchConfig{Enabled: true, PRs: true})
	t.Setenv("LINEAR_API_KEY", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("PATH", "")
	out := captureStdout(t, func() { runAgentWatchStatus(nil, nil) })
	if !strings.Contains(out, "acme-stack") || !strings.Contains(out, "no source with a token") {
		t.Fatalf("status: %s", out)
	}
	utils.JSONOutput = true
	out = captureStdout(t, func() { runAgentWatchStatus(nil, nil) })
	utils.JSONOutput = false
	var rep struct {
		Workspaces []struct{ Workspace string }
	}
	if json.Unmarshal([]byte(out), &rep) != nil || len(rep.Workspaces) != 1 {
		t.Fatalf("json: %s", out)
	}

	out = captureStdout(t, func() {
		if err := agentWatchRunCmd.RunE(agentWatchRunCmd, nil); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "nothing new") {
		t.Fatalf("run: %s", out)
	}

	out = captureStdout(t, func() {
		if err := agentWatchHooksCmd.RunE(agentWatchHooksCmd, nil); err != nil {
			t.Fatal(err)
		}
	})
	secret := watch.LoadSecrets(dir).HookSecret
	if secret == "" || !strings.Contains(out, secret) || !strings.Contains(out, "/hooks/linear") {
		t.Fatalf("hooks: %s", out)
	}
	_ = agentWatchHooksCmd.Flags().Set("rotate", "true")
	defer agentWatchHooksCmd.Flags().Set("rotate", "false")
	captureStdout(t, func() { _ = agentWatchHooksCmd.RunE(agentWatchHooksCmd, nil) })
	if watch.LoadSecrets(dir).HookSecret == secret {
		t.Fatal("rotate kept the secret")
	}

	a := agentWatchAuthCmd.Flags()
	_ = a.Set("token", "lin_abc")
	_ = a.Set("me", "u1")
	defer func() { _ = a.Set("token", ""); _ = a.Set("me", "") }()
	captureStdout(t, func() {
		if err := agentWatchAuthCmd.RunE(agentWatchAuthCmd, []string{"linear"}); err != nil {
			t.Fatal(err)
		}
	})
	if s := watch.LoadSecrets(dir); s.Linear != "lin_abc" || s.Me != "u1" {
		t.Fatalf("auth saved %+v", s)
	}
	if err := agentWatchAuthCmd.RunE(agentWatchAuthCmd, []string{"bitbucket"}); err == nil {
		t.Fatal("unknown source must fail")
	}
}

func TestWatchHookHandlerQueuesForTheDaemon(t *testing.T) {
	dir, _ := watchFixture(t, &config.WatchConfig{Enabled: true})
	if err := watch.SaveSecrets(dir, watch.Secrets{HookSecret: "s3cret", Me: "u1"}); err != nil {
		t.Fatal(err)
	}
	body := `{"action":"create","type":"Issue","data":{"id":"i1","identifier":"ABC-7","title":"Login loops","url":"https://linear.app/x/ABC-7","createdAt":"2026-09-09T10:00:00.000Z","state":{"name":"Todo"},"labels":[{"name":"Bug"}],"assignee":{"id":"u1","name":"Andrii"}}}`
	mac := hmac.New(sha256.New, []byte("s3cret"))
	mac.Write([]byte(body))
	sig := hex.EncodeToString(mac.Sum(nil))

	h := watchHookHandler("linear")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/hooks/linear", strings.NewReader(body))
	req.Header.Set("Linear-Signature", sig)
	h(rec, req)
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"queued":1`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	cmds, err := command.Drain(dir, time.Now(), time.Hour)
	if err != nil || len(cmds) != 1 || cmds[0].Action != command.ActionWatch || cmds[0].WatchEvent == nil || cmds[0].WatchEvent.Ref != "ABC-7" || !cmds[0].WatchEvent.Mine {
		t.Fatalf("spool %v %+v", err, cmds)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/hooks/linear", strings.NewReader(body))
	req.Header.Set("Linear-Signature", "deadbeef")
	h(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad signature got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/hooks/linear", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/hooks/linear", strings.NewReader("{"))
	req.Header.Set("Linear-Signature", signHook("s3cret", "{"))
	h(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad payload got %d", rec.Code)
	}
}

func signHook(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestWatchHelpers(t *testing.T) {
	if got := splitList(" bug, ,defect "); len(got) != 2 || got[1] != "defect" {
		t.Fatal(got)
	}
	if firstLineOf("a\nb") != "a" || firstLineOf("a") != "a" {
		t.Fatal("firstLineOf")
	}
	if d := describeWatch(&config.WatchConfig{Assignee: "any", Comments: true}); d != "any assignee · issue comments → notify" {
		t.Fatal(d)
	}
	if len(randomSecret()) < 30 {
		t.Fatal("secret too short")
	}
	dir := t.TempDir()
	if countWatchEventsToday(dir) != 0 {
		t.Fatal("no file")
	}
	_ = os.MkdirAll(filepath.Join(dir, "watch"), 0o700)
	now := time.Now().Format(time.RFC3339)
	_ = os.WriteFile(filepath.Join(dir, "watch", "events.jsonl"), []byte(`{"at":"`+now+`"}`+"\n"+`{"at":"2001-01-01T00:00:00Z"}`+"\n"), 0o600)
	if countWatchEventsToday(dir) != 1 {
		t.Fatal("count")
	}
}
