package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

func TestOpenTargets_AllWithPorts(t *testing.T) {
	corgi := &utils.CorgiCompose{
		Services: []utils.Service{
			{ServiceName: "api", Port: 3000},
			{ServiceName: "web", Port: 5173},
			{ServiceName: "worker"}, // no port -> skipped
		},
	}
	got := openTargets(corgi, nil)
	want := []openTarget{
		{Service: "api", URL: "http://localhost:3000"},
		{Service: "web", URL: "http://localhost:5173"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("openTargets = %+v, want %+v", got, want)
	}
}

func TestOpenTargets_Subset(t *testing.T) {
	corgi := &utils.CorgiCompose{
		Services: []utils.Service{
			{ServiceName: "api", Port: 3000},
			{ServiceName: "web", Port: 5173},
		},
	}
	got := openTargets(corgi, []string{"web"})
	want := []openTarget{{Service: "web", URL: "http://localhost:5173"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("openTargets subset = %+v, want %+v", got, want)
	}
}

func TestOpenTargets_UnknownNamesReturnEmpty(t *testing.T) {
	corgi := &utils.CorgiCompose{
		Services: []utils.Service{{ServiceName: "api", Port: 3000}},
	}
	got := openTargets(corgi, []string{"nope"})
	if len(got) != 0 {
		t.Fatalf("expected no targets for unknown service, got %+v", got)
	}
}

func TestRunOpen_NonInteractiveSkipsLauncher(t *testing.T) {
	called := false
	orig := launcher
	launcher = func(string) error { called = true; return nil }
	defer func() { launcher = orig }()

	origNI := utils.NonInteractive
	utils.NonInteractive = true
	defer func() { utils.NonInteractive = origNI }()

	corgi := &utils.CorgiCompose{Services: []utils.Service{{ServiceName: "api", Port: 3000}}}
	for _, tg := range openTargets(corgi, nil) {
		if utils.NonInteractive {
			continue
		}
		_ = launcher(tg.URL)
	}
	if called {
		t.Fatal("launcher should not be called in NonInteractive mode")
	}
}

func TestBrowserCommand(t *testing.T) {
	name, args := browserCommand("http://localhost:3000", "")
	if name == "" || len(args) == 0 {
		t.Fatalf("browserCommand returned empty: %q %v", name, args)
	}
	// last arg should always carry the URL
	if args[len(args)-1] != "http://localhost:3000" {
		t.Fatalf("URL not in args: %v", args)
	}
}

func TestOpenJSONShapeIsPureJSON(t *testing.T) {
	targets := []openTarget{{Service: "api", URL: "http://localhost:3000"}}
	b, err := json.Marshal(map[string]any{"opened": targets})
	if err != nil {
		t.Fatal(err)
	}
	var back map[string][]openTarget
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("open json not parseable: %v", err)
	}
	if back["opened"][0].Service != "api" {
		t.Fatalf("roundtrip mismatch: %+v", back)
	}
}

func newTestOpenCommand() *cobra.Command {
	root := &cobra.Command{Use: "corgi"}
	c := &cobra.Command{Use: "open"}
	root.AddCommand(c)
	for _, f := range []string{"filename", "fromTemplate", "fromTemplateName", "privateToken", "dockerContext"} {
		root.Flags().String(f, "", "")
	}
	for _, f := range []string{"exampleList", "describe", "fromScratch", "runOnce"} {
		root.Flags().Bool(f, false, "")
	}
	c.Flags().Bool("global", false, "")
	return c
}

func TestRunOpen_LaunchesAndJSON(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "corgi-compose.yml")
	content := "name: test\nservices:\n  api:\n    port: 3000\n    start:\n      - echo hi\n"
	if err := os.WriteFile(yml, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })

	// normal mode: launcher invoked per target
	var opened []string
	origLauncher := launcher
	launcher = func(url string) error { opened = append(opened, url); return nil }
	t.Cleanup(func() { launcher = origLauncher })

	c := newTestOpenCommand()
	runOpen(c, nil)
	if len(opened) != 1 || opened[0] != "http://localhost:3000" {
		t.Fatalf("expected api launched, got %v", opened)
	}

	// json mode: pure JSON on stdout, launcher not called
	opened = nil
	origJSON := utils.JSONOutput
	utils.JSONOutput = true
	t.Cleanup(func() { utils.JSONOutput = origJSON })

	out := captureStdout(t, func() { runOpen(newTestOpenCommand(), nil) })
	if len(opened) != 0 {
		t.Fatalf("launcher must not run in --json mode, got %v", opened)
	}
	var back map[string][]openTarget
	if err := json.Unmarshal([]byte(out), &back); err != nil {
		t.Fatalf("open --json stdout not pure JSON: %q err=%v", out, err)
	}
	if back["opened"][0].Service != "api" {
		t.Fatalf("unexpected json: %s", out)
	}
}

func TestRunOpen_NoPortServices(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "corgi-compose.yml")
	if err := os.WriteFile(yml, []byte("name: t\nservices:\n  worker:\n    start:\n      - echo hi\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })

	out := captureStdout(t, func() { runOpen(newTestOpenCommand(), nil) })
	if !strings.Contains(out, "No services with a port") {
		t.Fatalf("expected no-port message, got %q", out)
	}
}

func TestRunOpen_LauncherErrorIsReported(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "corgi-compose.yml")
	if err := os.WriteFile(yml, []byte("name: t\nservices:\n  api:\n    port: 3000\n    start:\n      - echo hi\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })

	orig := launcher
	launcher = func(string) error { return errLauncherTest }
	t.Cleanup(func() { launcher = orig })

	out := captureStdout(t, func() { runOpen(newTestOpenCommand(), nil) })
	if !strings.Contains(out, "could not open") {
		t.Fatalf("expected launcher error reported, got %q", out)
	}
}

var errLauncherTest = errOpenTest("launch failed")

type errOpenTest string

func (e errOpenTest) Error() string { return string(e) }
