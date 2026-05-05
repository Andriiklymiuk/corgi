package cmd

import (
	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/tunnel"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func TestFirstLine(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"hello\nworld", "hello"},
		{"single line", "single line"},
		{"", ""},
		{"\nleading", ""},
	}
	for _, tt := range tests {
		if got := firstLine(tt.in); got != tt.want {
			t.Errorf("firstLine(%q) = %q want %q", tt.in, got, tt.want)
		}
	}
}

func TestEnvFilePathsEmpty(t *testing.T) {
	if got := envFilePaths(utils.Service{}); len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

func TestEnvFilePathsAbsoluteOnly(t *testing.T) {
	got := envFilePaths(utils.Service{AbsolutePath: "/srv/api"})
	if len(got) != 1 || got[0] != "/srv/api/.env" {
		t.Errorf("got %v", got)
	}
}

func TestEnvFilePathsCopyEnvAbs(t *testing.T) {
	got := envFilePaths(utils.Service{
		AbsolutePath:        "/srv/api",
		CopyEnvFromFilePath: "/etc/.env",
	})
	if len(got) != 2 || got[1] != "/etc/.env" {
		t.Errorf("got %v", got)
	}
}

func TestEnvFilePathsCopyEnvRelative(t *testing.T) {
	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = "/proj"
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	got := envFilePaths(utils.Service{
		AbsolutePath:        "/srv/api",
		CopyEnvFromFilePath: "configs/.env",
	})
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	if got[1] != filepath.Join("/proj", "configs/.env") {
		t.Errorf("got %v", got)
	}
}

func TestBuildTunnelTargetForServiceSkipsZeroPort(t *testing.T) {
	_, skip, err := buildTunnelTargetForService(utils.Service{Port: 0}, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !skip {
		t.Error("expected skip")
	}
}

func TestBuildTunnelTargetForServiceSkipsManualWithoutRequest(t *testing.T) {
	_, skip, err := buildTunnelTargetForService(utils.Service{Port: 8080, ManualRun: true, ServiceName: "api"}, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !skip {
		t.Error("expected skip for manual without request")
	}
}

func TestBuildTunnelTargetForServiceRequestedNotInList(t *testing.T) {
	_, skip, _ := buildTunnelTargetForService(
		utils.Service{Port: 8080, ServiceName: "api"},
		map[string]bool{"other": true},
		nil, false,
	)
	if !skip {
		t.Error("expected skip when not requested")
	}
}

func TestBuildTunnelTargetForServiceNoTunnelConfig(t *testing.T) {
	target, skip, err := buildTunnelTargetForService(
		utils.Service{Port: 8080, ServiceName: "api"},
		nil, nil, false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if skip {
		t.Fatal("did not expect skip")
	}
	if target.service != "api" || target.port != 8080 {
		t.Errorf("got %+v", target)
	}
}

func TestWarnUnknownRequestedNoOp(t *testing.T) {
	warnUnknownRequested(nil, nil)
}

func TestWarnUnknownRequestedSeen(t *testing.T) {
	warnUnknownRequested(
		map[string]bool{"api": true, "x": true},
		[]tunnelTarget{{service: "api"}},
	)
}

func TestEffectiveProvider(t *testing.T) {
	t.Run("override wins", func(t *testing.T) {
		ovr := tunnel.Cloudflared{}
		fb := tunnel.Ngrok{}
		got := tunnelTarget{providerOvr: ovr}.effectiveProvider(fb)
		if got.Name() != "cloudflared" {
			t.Errorf("got %s", got.Name())
		}
	})
	t.Run("fallback used when no override", func(t *testing.T) {
		fb := tunnel.Ngrok{}
		got := tunnelTarget{}.effectiveProvider(fb)
		if got.Name() != "ngrok" {
			t.Errorf("got %s", got.Name())
		}
	})
}

func TestCollectRunTargetsEmpty(t *testing.T) {
	got := collectRunTargets(nil)
	if len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

func TestCollectRunTargetsSkipsNoTunnel(t *testing.T) {
	got := collectRunTargets([]utils.Service{
		{ServiceName: "a", Port: 80, Tunnel: nil},
		{ServiceName: "b", Port: 0, Tunnel: &utils.TunnelConfig{Hostname: "x"}},
		{ServiceName: "c", Port: 80, ManualRun: true, Tunnel: &utils.TunnelConfig{Hostname: "x"}},
	})
	if len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

func TestStopRunTunnelsNil(t *testing.T) {
	runTunnelsCancel = nil
	stopRunTunnels()
}

func TestPreflightTargetsEmpty(t *testing.T) {
	if err := preflightTargets(nil, tunnel.Cloudflared{}); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestPreflightTargetsBasic(t *testing.T) {
	targets := []tunnelTarget{
		{service: "x", port: 3000},
	}
	err := preflightTargets(targets, tunnel.Cloudflared{})
	_ = err
}

func TestPrintTunnelSummary(t *testing.T) {
	targets := []tunnelTarget{
		{service: "x", port: 3000},
		{service: "y", port: 4000, named: &tunnel.NamedConfig{Hostname: "y.example.com"}},
	}
	printTunnelSummary(targets, tunnel.Cloudflared{})
}

func TestBuildTargetsFromComposeError(t *testing.T) {
	c := &cobra.Command{}
	c.Flags().Bool("global", false, "")
	for _, f := range []string{"filename", "fromTemplate", "fromTemplateName", "privateToken", "dockerContext"} {
		c.Flags().String(f, "/nonexistent/zzz.yml", "")
	}
	for _, f := range []string{"exampleList", "describe", "fromScratch", "runOnce"} {
		c.Flags().Bool(f, false, "")
	}
	_, err := buildTargetsFromCompose(c, nil, tunnel.Cloudflared{}, false)
	if err == nil {
		t.Error("expected err")
	}
}

func newTestTunnelCommand() (*cobra.Command, *cobra.Command) {
	root := &cobra.Command{Use: "corgi"}
	c := &cobra.Command{Use: "tunnel"}
	root.AddCommand(c)
	for _, f := range []string{"filename", "fromTemplate", "fromTemplateName", "privateToken", "dockerContext"} {
		root.Flags().String(f, "", "")
	}
	for _, f := range []string{"exampleList", "describe", "fromScratch", "runOnce"} {
		root.Flags().Bool(f, false, "")
	}
	c.Flags().Bool("global", false, "")
	return root, c
}

func TestBuildTargetsFromComposeSuccess(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "corgi-compose.yml")
	content := "name: test\nservices:\n  api:\n    port: 3000\n"
	if err := os.WriteFile(yml, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })

	_, c := newTestTunnelCommand()
	targets, err := buildTargetsFromCompose(c, nil, tunnel.Cloudflared{}, false)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(targets) != 1 {
		t.Errorf("expected 1 target, got %d", len(targets))
	}
}

func TestBuildTargetsFromComposeNoServices(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "corgi-compose.yml")
	if err := os.WriteFile(yml, []byte("name: test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })

	_, c := newTestTunnelCommand()
	targets, err := buildTargetsFromCompose(c, nil, tunnel.Cloudflared{}, false)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(targets) != 0 {
		t.Errorf("expected 0 targets, got %d", len(targets))
	}
}

func TestCollectRunTargetsSkipsMissingTunnel(t *testing.T) {
	// service has no tunnel block → collectRunTargets skips it
	targets := collectRunTargets([]utils.Service{
		{ServiceName: "api", Port: 3000, Tunnel: nil},
	})
	if len(targets) != 0 {
		t.Errorf("expected 0, got %d", len(targets))
	}
}
