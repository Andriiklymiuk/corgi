package cmd

import (
	"context"
	"encoding/json"
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

func fakeBar(t *testing.T, rig *termRig, bar string, answer func(handsJob) handsResult) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		for ctx.Err() == nil {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, rig.srv.URL+"/launch/screen/hands/next", nil)
			req.Header.Set("Authorization", "Bearer "+bar)
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				return
			}
			var job handsJob
			ok := res.StatusCode == http.StatusOK && json.NewDecoder(res.Body).Decode(&job) == nil
			res.Body.Close()
			if !ok {
				time.Sleep(20 * time.Millisecond)
				continue
			}
			out := answer(job)
			out.ID = job.ID
			body, _ := json.Marshal(out)
			post, _ := http.NewRequestWithContext(ctx, http.MethodPost, rig.srv.URL+"/launch/screen/hands/next", strings.NewReader(string(body)))
			post.Header.Set("Authorization", "Bearer "+bar)
			if r, err := http.DefaultClient.Do(post); err == nil {
				r.Body.Close()
			}
		}
	}()
	deadline := time.Now().Add(3 * time.Second)
	for !hands.barListening(time.Now()) {
		if time.Now().After(deadline) {
			t.Fatal("the fake bar never polled")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func resetHands(t *testing.T) {
	t.Helper()
	prev := hands
	hands = &handsHub{jobs: make(chan *handsJob, 8), pending: map[string]chan handsResult{}}
	t.Cleanup(func() { hands = prev })
}

func TestHandsValidate(t *testing.T) {
	for _, bad := range []handsRequest{
		{Do: "format"},
		{Do: "act"},
		{Do: "act", Actions: []handsAction{{Do: "sudo"}}},
		{Do: "act", Actions: []handsAction{{Do: "wait", MS: 60000}}},
		{Do: "open"},
		{Do: "look", Max: 9000},
	} {
		if bad.validate() == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	if (handsRequest{Do: "act", Actions: []handsAction{{Do: "click", X: 1, Y: 2}, {Do: "type", Text: "hi"}}}).validate() != nil {
		t.Fatal("a fine request")
	}
}

func TestHandsActionWords(t *testing.T) {
	cases := map[string]handsAction{
		"click 10 20":      {Do: "click", X: 10, Y: 20},
		"drag 1 2 3 4":     {Do: "drag", X: 1, Y: 2, X2: 3, Y2: 4},
		"type hello world": {Do: "type", Text: "hello world"},
		"key cmd+shift+t":  {Do: "key", Keys: "cmd+shift+t"},
		"scroll 0 -300":    {Do: "scroll", DY: -300},
		"wait 250":         {Do: "wait", MS: 250},
	}
	for in, want := range cases {
		got, err := parseHandsAction(in)
		if err != nil || got != want {
			t.Errorf("%q: %+v %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "click 1", "click a b", "key", "fly 1 2"} {
		if _, err := parseHandsAction(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestHandsFromThisMacAndFromAPeer(t *testing.T) {
	resetHands(t)
	rig := newTermRig(t, "peer")
	rig.withSelfPeer(t, "")
	bar := rig.barToken(t)
	if err := os.WriteFile(filepath.Join(rig.dir, mcpAddrName), []byte(strings.TrimPrefix(rig.srv.URL, "http://")), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := callHands(ctx, "", handsRequest{Do: "windows"}); err == nil || !strings.Contains(err.Error(), "may not use") {
		t.Fatalf("nobody allowed yet: %v", err)
	}
	agentHandsAllowCmd.Run(agentHandsAllowCmd, []string{"here"})
	if _, err := callHands(ctx, "", handsRequest{Do: "windows"}); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("no bar yet: %v", err)
	}
	var seen []handsJob
	fakeBar(t, rig, bar, func(j handsJob) handsResult {
		seen = append(seen, j)
		if j.Do == "look" {
			return handsResult{OK: true, Image: "aGVsbG8=", Mime: "image/jpeg", Width: 1280, Height: 800, Text: "Safari in front"}
		}
		return handsResult{OK: true, Text: "Safari — corgi docs (front)\nXcode — CorgiBar"}
	})
	out, err := callHands(ctx, "", handsRequest{Do: "windows"})
	if err != nil || !strings.Contains(out.Text, "Safari") {
		t.Fatalf("local windows: %+v %v", out, err)
	}
	if _, err := callHands(ctx, "build-server", handsRequest{Do: "look"}); err == nil || !strings.Contains(err.Error(), "may not use") {
		t.Fatalf("the peer is not allowed yet: %v", err)
	}
	agentHandsAllowCmd.Run(agentHandsAllowCmd, []string{"office-mac"})
	out, err = callHands(ctx, "build-server", handsRequest{Do: "look", Max: 800})
	if err != nil || out.Image != "aGVsbG8=" || out.Width != 1280 {
		t.Fatalf("peer look: %+v %v", out, err)
	}
	if len(seen) != 2 || seen[1].By != "office-mac" || seen[1].Max != 800 {
		t.Fatalf("jobs: %+v", seen)
	}
	recent, _ := screen.Recent(rig.dir, 5)
	agentEvents := 0
	for _, r := range recent {
		if r.Role == "agent" {
			agentEvents++
		}
	}
	if agentEvents < 2 {
		t.Fatalf("every agent call is logged: %+v", recent)
	}
	agentHandsDenyCmd.Run(agentHandsDenyCmd, []string{"office-mac"})
	if _, err := callHands(ctx, "build-server", handsRequest{Do: "windows"}); err == nil {
		t.Fatal("denied again")
	}
}

func TestHandsLocalRouteNeedsTheKeyFromThisMachine(t *testing.T) {
	resetHands(t)
	rig := newTermRig(t, "peer")
	key, _ := handsKey(rig.dir)
	post := func(k string, forwarded bool) int {
		req, _ := http.NewRequest(http.MethodPost, rig.srv.URL+handsLocalPath, strings.NewReader(`{"do":"windows"}`))
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
	if code := post("wrong", false); code != http.StatusForbidden {
		t.Fatalf("wrong key: %d", code)
	}
	if code := post(key, true); code != http.StatusForbidden {
		t.Fatalf("through a tunnel: %d", code)
	}
	if code := post(key, false); code != http.StatusForbidden {
		t.Fatalf("not allowed yet: %d", code)
	}
	if info, err := os.Stat(handsKeyPath(rig.dir)); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("key file: %v %v", info, err)
	}
}

func TestMacToolsAreOffOverAPublicTunnel(t *testing.T) {
	t.Setenv("CORGI_MCP_ALLOW_DANGEROUS_TUNNEL", "")
	prev := mcpPublicTunnelActive.Load()
	mcpPublicTunnelActive.Store(true)
	defer mcpPublicTunnelActive.Store(prev)
	res, err := handsCall(mcp.CallToolRequest{}, handsRequest{Do: "windows"})
	if err != nil || !res.IsError || !strings.Contains(res.Content[0].(mcp.TextContent).Text, "public tunnel") {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestAPhoneWithControlAsksForAppsAndMenus(t *testing.T) {
	resetHands(t)
	rig := newTermRig(t, "")
	bar := rig.barToken(t)
	_ = runAgentScreen(rig.dir, "on", nil)
	fakeBar(t, rig, bar, func(j handsJob) handsResult {
		switch j.Do {
		case "apps":
			return handsResult{OK: true, Text: `[{"name":"Safari","bundle":"com.apple.Safari","active":true}]`}
		case "menu":
			return handsResult{OK: true, Text: "pressed " + strings.Join(j.Path, " › ")}
		}
		return handsResult{OK: true, Text: j.Do}
	})
	c := peers.NewClient(rig.peer)
	var out handsResult
	ask := func(req handsRequest) error {
		out = handsResult{}
		return c.Call(context.Background(), "/launch/screen/do", req, &out)
	}
	if err := ask(handsRequest{Do: "apps"}); err == nil || !strings.Contains(err.Error(), "not control") {
		t.Fatalf("watch-only or ungranted is refused: %v", err)
	}
	if _, err := screen.Update(rig.dir, func(s *screen.Settings) error {
		s.Devices[rig.hash] = &screen.Grant{Name: "office-mac", Role: screen.RoleControl}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := ask(handsRequest{Do: "apps", Icons: true}); err != nil || !strings.Contains(out.Text, "Safari") {
		t.Fatalf("apps: %+v %v", out, err)
	}
	if err := ask(handsRequest{Do: "menu", Path: []string{"File", "Save"}}); err != nil || out.Text != "pressed File › Save" {
		t.Fatalf("menu: %+v %v", out, err)
	}
	if err := ask(handsRequest{Do: "act", Actions: []handsAction{{Do: "click"}}}); err == nil {
		t.Fatal("a phone clicks through the stream, not this route")
	}
	if err := ask(handsRequest{Do: "menu", Path: []string{"File"}}); err == nil {
		t.Fatal("a menu path needs the item too")
	}
	_ = runAgentScreen(rig.dir, "off", nil)
	if err := ask(handsRequest{Do: "apps"}); err == nil {
		t.Fatal("screen off refuses")
	}
}
