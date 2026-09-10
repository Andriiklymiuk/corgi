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
	"runtime"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/config"
	"github.com/spf13/pflag"

	"andriiklymiuk/corgi/utils/agent/daemon"
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

func TestLoadWatchSpecsSkipsDeadSourcesAndCarriesDefaults(t *testing.T) {
	dir, _ := watchFixture(t, &config.WatchConfig{Enabled: true, Action: "fix", MaxFixesPerDay: 4})
	user, _ := config.LoadUser(agentUserConfigPath(dir))
	user.Defaults.Watch = &config.WatchConfig{MaxFixesPerHour: 5, MaxFixesPerDay: 20, Quiet: "22:00-06:00"}
	if err := writeUserConfig(agentUserConfigPath(dir), user); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LINEAR_API_KEY", "lin_x")
	t.Setenv("GITHUB_TOKEN", "gh_x")
	t.Setenv("GITLAB_TOKEN", "gl_x")
	t.Setenv("JIRA_API_TOKEN", "")
	specs, err := loadWatchSpecs(dir)
	if err != nil || len(specs) != 1 {
		t.Fatalf("%v %+v", err, specs)
	}
	s := specs[0]
	if len(s.Sources) != 1 || s.Sources[0].Name() != "linear" || strings.Join(s.Skipped, ",") != "github,gitlab" {
		t.Fatalf("sources %v skipped %v", s.Sources, s.Skipped)
	}
	if s.MaxFixesPerHour != 5 || s.MaxFixesPerDay != 4 || s.Quiet != "22:00-06:00" {
		t.Fatalf("caps %+v", s)
	}
	if h, d := s.FixCaps(); h != 5 || d != 4 {
		t.Fatalf("caps %d %d", h, d)
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
	_ = f.Set("max-per-hour", "2")
	_ = f.Set("quiet", "23:00-07:00")
	defer func() {
		for _, name := range []string{"labels", "prs", "action", "interval", "project", "quiet"} {
			_ = f.Set(name, "")
			f.Lookup(name).Changed = false
		}
		for _, name := range []string{"max-per-hour", "max-per-day"} {
			_ = f.Set(name, "0")
			f.Lookup(name).Changed = false
		}
	}()
	out := captureStdout(t, func() {
		if err := agentWatchEnableCmd.RunE(agentWatchEnableCmd, nil); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "watching acme-stack") || !strings.Contains(out, "labels bug,defect") || !strings.Contains(out, "→ fix, at most 2/h 10/day, quiet 23:00-07:00") {
		t.Fatalf("enable said: %s", out)
	}
	user, _ := config.LoadUser(agentUserConfigPath(dir))
	wc := user.Workspaces["acme-stack"].Watch
	if wc == nil || !wc.Enabled || wc.Interval != "2m" || wc.Project != "ABC" || !wc.PRs || len(wc.Labels) != 2 {
		t.Fatalf("config %+v", wc)
	}
	if wc.MaxFixesPerHour != 2 || wc.MaxFixesPerDay != 0 || wc.Quiet != "23:00-07:00" {
		t.Fatalf("caps %+v", wc)
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
	_ = f.Set("interval", "")
	_ = f.Set("quiet", "bedtime")
	if err := agentWatchEnableCmd.RunE(agentWatchEnableCmd, nil); err == nil {
		t.Fatal("bad quiet hours must be refused")
	}
	_ = f.Set("quiet", "")
	_ = f.Set("max-per-day", "0")
	if err := agentWatchEnableCmd.RunE(agentWatchEnableCmd, nil); err == nil {
		t.Fatal("a zero cap must be refused")
	}
	_ = f.Set("max-per-day", "1")

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

func TestWatchStatusShowsFixBudgetAndSkippedSources(t *testing.T) {
	dir, _ := watchFixture(t, &config.WatchConfig{Enabled: true, Action: "fix", Quiet: "23:00-07:00"})
	t.Setenv("LINEAR_API_KEY", "lin_x")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("PATH", "")
	fixes := watch.LoadFixLog(dir)
	fixes.Start("acme-stack", "linear:ABC-1", time.Now().Add(-time.Minute))
	fixes.Defer(watch.Event{Key: "linear:ABC-2", Workspace: "acme-stack", Kind: watch.KindIssueNew, Ref: "ABC-2", Title: "waiting"})
	out := captureStdout(t, func() { runAgentWatchStatus(nil, nil) })
	want := "caps 3/h 10/day · quiet 23:00-07:00 · fixes today: 1 (last " + time.Now().Add(-time.Minute).Format("15:04") + ") · 1 deferred"
	if !strings.Contains(out, want) || !strings.Contains(out, "(github, gitlab skipped: --prs off)") {
		t.Fatalf("status: %s", out)
	}
	utils.JSONOutput = true
	out = captureStdout(t, func() { runAgentWatchStatus(nil, nil) })
	utils.JSONOutput = false
	var rep struct {
		Workspaces []struct {
			Skipped []string
			Quiet   string
			Fixes   struct{ Today, Deferred, PerHour int }
		}
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil || len(rep.Workspaces) != 1 {
		t.Fatalf("json: %v %s", err, out)
	}
	if w := rep.Workspaces[0]; len(w.Skipped) != 2 || w.Quiet != "23:00-07:00" || w.Fixes.Today != 1 || w.Fixes.Deferred != 1 || w.Fixes.PerHour != 3 {
		t.Fatalf("json: %+v", w)
	}

	// Without a daemon, `run` lists what waits rather than handing it anywhere.
	out = captureStdout(t, func() {
		if err := agentWatchRunCmd.RunE(agentWatchRunCmd, nil); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "1 deferred fix(es), no daemon") || !strings.Contains(out, "ABC-2 — waiting") {
		t.Fatalf("run: %s", out)
	}
	if watch.LoadFixLog(dir).DeferredCount("") != 1 {
		t.Fatal("run must not touch the fix log")
	}
}

func TestWatchTestWalksThePipeline(t *testing.T) {
	dir, _ := watchFixture(t, &config.WatchConfig{Enabled: true, Comments: true, Action: "fix", Project: "ABC"})
	t.Setenv("LINEAR_API_KEY", "lin_x")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("PATH", "")
	tf := agentWatchTestCmd.Flags()
	_ = tf.Set("body", "is this still needed?")
	defer func() { _ = tf.Set("body", ""); _ = tf.Set("ref", "") }()
	out := captureStdout(t, func() {
		if err := agentWatchTestCmd.RunE(agentWatchTestCmd, []string{"issue.comment"}); err != nil {
			t.Fatal(err)
		}
	})
	for _, want := range []string{"event      issue.comment ABC-1 — key test:issue.comment:ABC-1", "workspace  acme-stack", "rules      match", "seen       no", "fix        would start (0/3 this hour · 0/10 today)", `"is this still needed?"`, "/corgi:stories ABC-1", "--permission-mode acceptEdits"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if watch.LoadFixLog(dir).StartedSince("acme-stack", time.Time{}) != 0 || watch.LoadState(dir).IsSeen("test:issue.comment:ABC-1") {
		t.Fatal("a test run records nothing")
	}

	_ = tf.Set("ref", "acme/api#7")
	out = captureStdout(t, func() {
		if err := agentWatchTestCmd.RunE(agentWatchTestCmd, []string{"pr.review"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "workspace  none") {
		t.Fatalf("nobody watches PRs: %s", out)
	}
	if err := agentWatchTestCmd.RunE(agentWatchTestCmd, []string{"issue.closed"}); err == nil {
		t.Fatal("an unknown kind must be refused")
	}

	utils.JSONOutput = true
	out = captureStdout(t, func() { _ = agentWatchTestCmd.RunE(agentWatchTestCmd, []string{"issue.new"}) })
	utils.JSONOutput = false
	var rep struct {
		Routed bool
		Probe  struct {
			Workspace string
			Prompt    string
		}
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil || !rep.Routed || rep.Probe.Workspace != "acme-stack" || !strings.Contains(rep.Probe.Prompt, "/corgi:stories acme/api#7") {
		t.Fatalf("json: %v %s", err, out)
	}
}

// fakeGH puts a gh on PATH whose `auth token` prints token, and nothing else.
func fakeGH(t *testing.T, token string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("a shell script stands in for gh")
	}
	bin := t.TempDir()
	script := "#!/bin/sh\nif [ \"$1\" = auth ] && [ \"$2\" = token ]; then echo " + token + "; fi\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
}

func TestWatchStatusSeesTheGhCLIToken(t *testing.T) {
	watchFixture(t, &config.WatchConfig{Enabled: true, PRs: true})
	t.Setenv("LINEAR_API_KEY", "")
	t.Setenv("GITHUB_TOKEN", "")
	fakeGH(t, "ghp_from_cli")
	out := captureStdout(t, func() { runAgentWatchStatus(nil, nil) })
	if !strings.Contains(out, "github gh-auth "+watch.Fingerprint("ghp_from_cli")) || !strings.Contains(out, "every 3m0s github") {
		t.Fatalf("status: %s", out)
	}
	// A saved token wins over the CLI's.
	t.Setenv("GITHUB_TOKEN", "ghp_env")
	out = captureStdout(t, func() { runAgentWatchStatus(nil, nil) })
	if !strings.Contains(out, "github "+watch.Fingerprint("ghp_env")+" ·") || strings.Contains(out, "gh-auth") {
		t.Fatalf("status: %s", out)
	}
	if token, source := watch.GitHubToken(watch.Secrets{}); token != "ghp_from_cli" || source != "gh-auth" {
		t.Fatalf("%q %q", token, source)
	}
	t.Setenv("PATH", "")
	if token, source := watch.GitHubToken(watch.Secrets{}); token != "" || source != "" {
		t.Fatalf("%q %q", token, source)
	}
}

func TestSynthesizeWatchEvent(t *testing.T) {
	specs := []daemon.WatchSpec{{Workspace: "acme", Project: "ABC", Repos: []string{"acme/web"}}}
	e, err := synthesizeWatchEvent(watch.KindPRComment, specs, "", "", "")
	if err != nil || e.Ref != "acme/web#1" || e.URL != "https://github.com/acme/web/pull/1" || e.Body == "" || !e.Mine || e.Key != "test:pr.comment:acme/web#1" {
		t.Fatalf("%+v %v", e, err)
	}
	e, _ = synthesizeWatchEvent(watch.KindPRReview, specs, "acme/api#12", "", "")
	if e.URL != "https://github.com/acme/api/pull/12" || e.State != "changes_requested" {
		t.Fatalf("%+v", e)
	}
	e, _ = synthesizeWatchEvent(watch.KindIssueNew, specs, "", "", "")
	if e.Ref != "ABC-1" || e.Source != "linear" || e.Body != "" {
		t.Fatalf("%+v", e)
	}
	if _, err := synthesizeWatchEvent("nope", specs, "", "", ""); err == nil {
		t.Fatal("unknown kind")
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
	if d := describeWatch(&config.WatchConfig{Action: "fix", MaxFixesPerDay: 4}); d != "assigned to me → fix, at most 3/h 4/day" {
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

func TestWatchAuthStoresPerWorkspaceTokensAndTheSpecUsesThem(t *testing.T) {
	dir, ws := watchFixture(t, &config.WatchConfig{Enabled: true, Tracker: "jira", Project: "ACME"})
	t.Setenv("LINEAR_API_KEY", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("JIRA_API_TOKEN", "")
	t.Setenv("GITLAB_TOKEN", "")
	t.Chdir(ws)

	c := agentWatchAuthCmd
	for _, args := range [][]string{
		{"jira", "--token", "acme-jira", "--url", "https://acme.atlassian.net", "--email", "me@acme.com", "--local"},
		{"linear", "--token", "global-linear"},
	} {
		c.Flags().Visit(func(f *pflag.Flag) { _ = c.Flags().Set(f.Name, zeroFlag(f)) })
		if err := c.Flags().Parse(args[1:]); err != nil {
			t.Fatal(err)
		}
		if err := c.RunE(c, args[:1]); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}

	if got := watch.LoadSecrets(dir); got.JiraToken != "" || got.Linear != "global-linear" {
		t.Fatalf("--local must not touch the machine-wide tokens: %+v", got)
	}
	if got := watch.LoadSecretsFor(dir, "acme-stack"); got.JiraToken != "acme-jira" || got.Linear != "global-linear" {
		t.Fatalf("workspace tokens = %+v", got)
	}

	specs, err := loadWatchSpecs(dir)
	if err != nil || len(specs) != 1 {
		t.Fatalf("specs = %+v, %v", specs, err)
	}
	if len(specs[0].Sources) != 1 || specs[0].Sources[0].Name() != "jira" {
		t.Fatalf("the workspace should poll jira with its own token: %v", specs[0].Sources)
	}
}

func zeroFlag(f *pflag.Flag) string {
	if f.Value.Type() == "bool" {
		return "false"
	}
	return ""
}

func TestWatchAuthClearsAnOverrideAndRefusesAnUnknownSource(t *testing.T) {
	dir, _ := watchFixture(t, &config.WatchConfig{Enabled: true})
	c := agentWatchAuthCmd
	run := func(args ...string) error {
		c.Flags().Visit(func(f *pflag.Flag) { _ = c.Flags().Set(f.Name, zeroFlag(f)) })
		if err := c.Flags().Parse(args[1:]); err != nil {
			t.Fatal(err)
		}
		return c.RunE(c, args[:1])
	}

	if err := run("gitlab", "--token", "glpat-x", "--url", "https://git.acme.io", "--workspace", "acme-stack"); err != nil {
		t.Fatal(err)
	}
	if got := watch.WorkspaceSecrets(dir, "acme-stack"); got.GitLab != "glpat-x" || got.GitLabURL != "https://git.acme.io" {
		t.Fatalf("stored = %+v", got)
	}

	if err := run("gitlab", "--clear", "--workspace", "acme-stack"); err != nil {
		t.Fatal(err)
	}
	if got := watch.WorkspacesWithSecrets(dir); len(got) != 0 {
		t.Fatalf("clearing the only token should drop the override, got %v", got)
	}

	if err := run("bitbucket", "--token", "x"); err == nil {
		t.Error("an unknown source should be refused")
	}
	if err := run("linear", "--token", "x", "--workspace", "nope"); err != nil {
		t.Errorf("an unregistered id is still a valid key: %v", err)
	}
}

func TestWatchStatusPrintsARowPerWorkspaceOverride(t *testing.T) {
	dir, _ := watchFixture(t, &config.WatchConfig{Enabled: true})
	t.Setenv("LINEAR_API_KEY", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("JIRA_API_TOKEN", "")
	t.Setenv("GITLAB_TOKEN", "")
	if err := watch.SaveSecrets(dir, watch.Secrets{Linear: "global"}); err != nil {
		t.Fatal(err)
	}
	if err := watch.SaveWorkspaceSecrets(dir, "acme-stack", watch.Secrets{JiraToken: "acme"}); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() { runAgentWatchStatus(nil, nil) })
	if !strings.Contains(out, "machine-wide") || !strings.Contains(out, "acme-stack  ") {
		t.Fatalf("status should list both:\n%s", out)
	}
	if !strings.Contains(out, watch.Fingerprint("acme")) {
		t.Errorf("the override's own token is missing:\n%s", out)
	}
}

func TestAutoForTakesWordsAndKinds(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"all", nil},
		{"tickets", []string{"issue.new"}},
		{"issues", []string{"issue.new"}},
		{"reviews", []string{"pr.review"}},
		{"comments", []string{"issue.comment", "pr.comment"}},
		{"prs", []string{"pr.review", "pr.comment"}},
		{"pr.review", []string{"pr.review"}},
		{" Reviews , tickets ", []string{"pr.review", "issue.new"}},
		{"reviews,reviews", []string{"pr.review"}},
	}
	for _, c := range cases {
		got, err := parseAutoFor(c.in)
		if err != nil {
			t.Fatalf("%q: %v", c.in, err)
		}
		if len(got) != len(c.want) {
			t.Fatalf("%q: got %v want %v", c.in, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("%q: got %v want %v", c.in, got, c.want)
			}
		}
	}
	if _, err := parseAutoFor("everything"); err == nil {
		t.Fatal("a word nobody defined must say what the words are")
	}
}
