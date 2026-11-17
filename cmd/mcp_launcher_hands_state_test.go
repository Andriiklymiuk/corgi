package cmd

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/peers"
	"andriiklymiuk/corgi/utils/agent/screen"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestAPausedAgentWaitsUntilThePersonLetsItCarryOn(t *testing.T) {
	resetHands(t)
	rig := newTermRig(t, "peer")
	bar := rig.barToken(t)
	if err := os.WriteFile(filepath.Join(rig.dir, mcpAddrName), []byte(strings.TrimPrefix(rig.srv.URL, "http://")), 0o600); err != nil {
		t.Fatal(err)
	}
	agentHandsAllowCmd.Run(agentHandsAllowCmd, []string{"here"})
	done := 0
	fakeBar(t, rig, bar, func(j handsJob) handsResult {
		done++
		return handsResult{OK: true, Text: j.Do}
	})
	ctx := context.Background()
	if _, err := callHands(ctx, "", handsRequest{Do: "menu", Path: []string{"Product", "Archive"}}); err != nil {
		t.Fatal(err)
	}
	st, err := handsStateCall(http.MethodGet, "")
	if err != nil || !st.Active || st.Step != 1 || st.Paused || len(st.Steps) != 1 || st.Steps[0].Text != "Product › Archive" {
		t.Fatalf("at work: %+v %v", st, err)
	}
	if st, err = handsStateCall(http.MethodPost, "pause"); err != nil || !st.Paused || st.Waiting {
		t.Fatalf("paused: %+v %v", st, err)
	}
	if _, err := callHands(ctx, "", handsRequest{Do: "act", Actions: []handsAction{{Do: "type", Text: "hunter2"}}}); err == nil || !strings.Contains(err.Error(), "took the Mac") {
		t.Fatalf("a paused agent is told to wait: %v", err)
	}
	if done != 1 {
		t.Fatalf("nothing reaches the Mac while paused: %d", done)
	}
	if st, _ = handsStateCall(http.MethodGet, ""); !st.Waiting {
		t.Fatalf("the phone can say an agent waits: %+v", st)
	}
	if st, err = handsStateCall(http.MethodPost, "resume"); err != nil || st.Paused || st.Waiting {
		t.Fatalf("resumed: %+v %v", st, err)
	}
	if _, err := callHands(ctx, "", handsRequest{Do: "act", Actions: []handsAction{{Do: "click"}, {Do: "type", Text: "hunter2"}}}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(rig.dir, "screen", "sessions.log"))
	if strings.Contains(string(raw), "hunter2") {
		t.Fatal("typed text never reaches the log")
	}
	recent, _ := screen.Recent(rig.dir, 10)
	var refused, typed bool
	for _, r := range recent {
		refused = refused || (r.Role == "agent" && strings.Contains(r.Failed, "took the Mac"))
		typed = typed || (r.Role == "agent" && r.Detail == "click, type" && r.Failed == "")
	}
	if !refused || !typed {
		t.Fatalf("the log keeps the refused step and the done one: %+v", recent)
	}
}

func TestOnlyADeviceWithControlPausesAgents(t *testing.T) {
	resetHands(t)
	rig := newTermRig(t, "")
	_ = runAgentScreen(rig.dir, "on", nil)
	c := peers.NewClient(rig.peer)
	var st handsState
	pause := func(do string) error {
		st = handsState{}
		return c.Call(context.Background(), "/launch/screen/agent", handsStateRequest{Do: do}, &st)
	}
	if err := pause("pause"); err == nil || !strings.Contains(err.Error(), "not control") {
		t.Fatalf("watch-only may not pause: %v", err)
	}
	if _, err := screen.Update(rig.dir, func(s *screen.Settings) error {
		s.Devices[rig.hash] = &screen.Grant{Name: "office-mac", Role: screen.RoleControl}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := pause("pause"); err != nil || !st.Paused || st.PausedBy == "" {
		t.Fatalf("pause: %+v %v", st, err)
	}
	if err := pause("nap"); err == nil {
		t.Fatal("do is pause or resume")
	}
	if err := pause("resume"); err != nil || st.Paused {
		t.Fatalf("resume: %+v %v", st, err)
	}
	_ = runAgentScreen(rig.dir, "off", nil)
	if err := pause("pause"); err == nil {
		t.Fatal("screen off refuses")
	}
}

func TestThePauseRouteForThisMacNeedsItsKey(t *testing.T) {
	resetHands(t)
	rig := newTermRig(t, "")
	key, _ := handsKey(rig.dir)
	get := func(k string, forwarded bool) int {
		req, _ := http.NewRequest(http.MethodPost, rig.srv.URL+handsStatePath, strings.NewReader(`{"do":"pause"}`))
		req.Header.Set(handsKeyHeader, k)
		if forwarded {
			req.Header.Set("X-Forwarded-For", "203.0.113.5")
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if code := get("wrong", false); code != http.StatusForbidden {
		t.Fatalf("wrong key: %d", code)
	}
	if code := get(key, true); code != http.StatusForbidden {
		t.Fatalf("through a tunnel: %d", code)
	}
	if code := get(key, false); code != http.StatusOK {
		t.Fatalf("this Mac: %d", code)
	}
}

func TestAPauseNobodyLiftedExpires(t *testing.T) {
	h := &handsHub{jobs: make(chan *handsJob, 1), pending: map[string]chan handsResult{}}
	now := time.Now()
	h.pause("phone", now)
	if err := h.admit(handsRequest{Do: "look"}, "mac", now.Add(time.Minute)); err == nil {
		t.Fatal("paused")
	}
	if err := h.admit(handsRequest{Do: "look"}, "mac", now.Add(handsPauseExpiry+time.Second)); err != nil {
		t.Fatalf("a forgotten pause ends: %v", err)
	}
	if st := h.state(now.Add(handsPauseExpiry + 2*time.Second)); st.Paused || !st.Active {
		t.Fatalf("%+v", st)
	}
	for i := 0; i < handsStepsKept+10; i++ {
		_ = h.admit(handsRequest{Do: "windows"}, "mac", now.Add(time.Hour))
	}
	if len(h.steps) != handsStepsKept {
		t.Fatalf("the steps kept are capped: %d", len(h.steps))
	}
}

func TestHandsPauseResumeAndLogCommands(t *testing.T) {
	resetHands(t)
	rig := newTermRig(t, "peer")
	bar := rig.barToken(t)
	if err := os.WriteFile(filepath.Join(rig.dir, mcpAddrName), []byte(strings.TrimPrefix(rig.srv.URL, "http://")), 0o600); err != nil {
		t.Fatal(err)
	}
	agentHandsAllowCmd.Run(agentHandsAllowCmd, []string{"here"})
	fakeBar(t, rig, bar, func(j handsJob) handsResult { return handsResult{OK: true, Text: j.Do} })
	if _, err := callHands(context.Background(), "", handsRequest{Do: "open", App: "Xcode"}); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() { agentHandsLogCmd.Run(agentHandsLogCmd, nil) })
	if !strings.Contains(out, "is using this Mac, step 1") || !strings.Contains(out, "open · Xcode") {
		t.Fatalf("log: %q", out)
	}
	out = captureStdout(t, func() { agentHandsPauseCmd.Run(agentHandsPauseCmd, nil) })
	if !strings.Contains(out, "Paused by") {
		t.Fatalf("pause: %q", out)
	}
	if _, err := callHands(context.Background(), "", handsRequest{Do: "look"}); err == nil {
		t.Fatal("paused")
	}
	out = captureStdout(t, func() { agentHandsLogCmd.Run(agentHandsLogCmd, nil) })
	if !strings.Contains(out, "an agent waits to carry on") || !strings.Contains(out, "failed:") {
		t.Fatalf("log while paused: %q", out)
	}
	out = captureStdout(t, func() { agentHandsResumeCmd.Run(agentHandsResumeCmd, nil) })
	if strings.Contains(out, "Paused") || !strings.Contains(out, "step 1") {
		t.Fatalf("resume: %q", out)
	}
}

func TestStepTextNeverCarriesWhatWasTyped(t *testing.T) {
	cases := map[string]handsRequest{
		"click, type":      {Do: "act", Actions: []handsAction{{Do: "click"}, {Do: "click"}, {Do: "type", Text: "secret"}}},
		"File › Save":      {Do: "menu", Path: []string{"File", "Save"}},
		"Xcode":            {Do: "open", App: "Xcode"},
		"a link":           {Do: "open", URL: "https://example.com/?token=secret"},
		"Safari":           {Do: "activate", App: "Safari"},
		"":                 {Do: "windows"},
		"a, b, c, d, e, …": {},
	}
	for want, req := range cases {
		if req.Do == "" {
			continue
		}
		if got := stepText(req); got != want {
			t.Fatalf("%s: %q, want %q", req.Do, got, want)
		}
	}
	long := handsRequest{Do: "act", Actions: []handsAction{{Do: "click"}, {Do: "type"}, {Do: "key"}, {Do: "scroll"}, {Do: "drag"}, {Do: "wait"}, {Do: "move"}}}
	if got := stepText(long); got != "click, type, key, scroll, drag, …" {
		t.Fatalf("long: %q", got)
	}
}

func TestTheMacsOwnBarPausesAgentsWithTheScreenOff(t *testing.T) {
	resetHands(t)
	rig := newTermRig(t, "")
	bar := rig.barToken(t)
	post := func(token string, forwarded bool) int {
		req, _ := http.NewRequest(http.MethodPost, rig.srv.URL+"/launch/screen/agent", strings.NewReader(`{"do":"pause"}`))
		req.Header.Set("Authorization", "Bearer "+token)
		if forwarded {
			req.Header.Set("X-Forwarded-For", "203.0.113.5")
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if code := post(bar, true); code == http.StatusOK {
		t.Fatal("a bar token used from elsewhere is not the Mac's own bar")
	}
	if code := post(bar, false); code != http.StatusOK {
		t.Fatalf("the Mac's bar pauses with remote screen off: %d", code)
	}
	if st := hands.state(time.Now()); !st.Paused || !strings.Contains(st.PausedBy, "the person at") {
		t.Fatalf("%+v", st)
	}
}

func mcpToolText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if res == nil || len(res.Content) == 0 {
		t.Fatal("no content")
	}
	text, _ := res.Content[0].(mcp.TextContent)
	return text.Text
}

func TestTheMacToolsObeyAPauseAndReportIt(t *testing.T) {
	t.Setenv("CORGI_MCP_ALLOW_DANGEROUS_TUNNEL", "")
	prev := mcpPublicTunnelActive.Load()
	mcpPublicTunnelActive.Store(false)
	defer mcpPublicTunnelActive.Store(prev)
	resetHands(t)
	rig := newTermRig(t, "peer")
	bar := rig.barToken(t)
	if err := os.WriteFile(filepath.Join(rig.dir, mcpAddrName), []byte(strings.TrimPrefix(rig.srv.URL, "http://")), 0o600); err != nil {
		t.Fatal(err)
	}
	agentHandsAllowCmd.Run(agentHandsAllowCmd, []string{"here"})
	var seen []string
	fakeBar(t, rig, bar, func(j handsJob) handsResult {
		seen = append(seen, j.Do+":"+j.App)
		return handsResult{OK: true, Text: j.Do + " ok"}
	})
	res, err := handsCall(mcp.CallToolRequest{}, handsRequest{Do: "activate", App: "Xcode"})
	if err != nil || res.IsError || mcpToolText(t, res) != "activate ok" {
		t.Fatalf("activate: %+v %v", res, err)
	}
	res, _ = handsCall(mcp.CallToolRequest{}, handsRequest{Do: "apps"})
	if res.IsError {
		t.Fatalf("apps: %+v", res)
	}
	st, _ := handsStateCall(http.MethodGet, "")
	if text := handsStateText(st); !strings.Contains(text, "is at work, step 2") || !strings.Contains(text, "activate · Xcode") {
		t.Fatalf("state: %q", text)
	}
	if _, err := handsStateCall(http.MethodPost, "pause"); err != nil {
		t.Fatal(err)
	}
	for _, do := range []string{"windows", "look", "menus", "apps"} {
		res, _ = handsCall(mcp.CallToolRequest{}, handsRequest{Do: do})
		if !res.IsError || !strings.Contains(mcpToolText(t, res), "took the Mac") {
			t.Fatalf("%s while paused: %+v", do, res)
		}
	}
	if len(seen) != 2 {
		t.Fatalf("a paused agent reaches nothing on the Mac: %v", seen)
	}
	st, _ = handsStateCall(http.MethodGet, "")
	if text := handsStateText(st); !strings.HasPrefix(text, "paused by ") {
		t.Fatalf("state while paused: %q", text)
	}
	if text := handsStateText(handsState{}); !strings.HasPrefix(text, "free") {
		t.Fatalf("free: %q", text)
	}
}
