package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

func TestFilterEnvCheckRowsKeepsRequestedOrder(t *testing.T) {
	rows := []utils.EnvCheckRow{
		{Service: "api"}, {Service: "web"}, {Service: "worker"},
	}
	got, err := filterEnvCheckRows(rows, []string{"worker", "api"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Service != "worker" || got[1].Service != "api" {
		t.Fatalf("got %+v", got)
	}
}

func TestFilterEnvCheckRowsNoNamesReturnsAll(t *testing.T) {
	rows := []utils.EnvCheckRow{{Service: "api"}}
	got, err := filterEnvCheckRows(rows, nil)
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, err %v", got, err)
	}
}

// A typo'd service name must error — silently checking everything (or
// nothing) would let the caller read the exit code as their service's verdict.
func TestFilterEnvCheckRowsRejectsUnknownService(t *testing.T) {
	rows := []utils.EnvCheckRow{{Service: "api"}}
	_, err := filterEnvCheckRows(rows, []string{"apo"})
	if err == nil || !strings.Contains(err.Error(), "apo") {
		t.Fatalf("expected an error naming the unknown service, got %v", err)
	}
}

// envCheckCommandFixture writes a compose dir with one service repo, chdirs
// into it, and returns a cobra command wired like the real `env check`.
func envCheckCommandFixture(t *testing.T, example, source string) *cobra.Command {
	t.Helper()
	dir := t.TempDir()
	repo := filepath.Join(dir, "api")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	compose := "name: test\nservices:\n  api:\n    path: ./api\n    copyEnvFromFilePath: api.env\n    start:\n      - echo hi\n"
	if err := os.WriteFile(filepath.Join(dir, "corgi-compose.yml"), []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}
	if example != "" {
		if err := os.WriteFile(filepath.Join(repo, ".env-example"), []byte(example), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if source != "" {
		if err := os.WriteFile(filepath.Join(dir, "api.env"), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(cwd) })

	_, c := newTestComposeCommand()
	c.Flags().String("file", "", "")
	origPayload := utils.PayloadOnStdout
	t.Cleanup(func() { utils.PayloadOnStdout = origPayload })
	return c
}

func stubExit(t *testing.T) *int {
	t.Helper()
	code := -1
	orig := osExit
	osExit = func(c int) { code = c }
	t.Cleanup(func() { osExit = orig })
	return &code
}

func TestRunEnvCheckJSONAllCovered(t *testing.T) {
	c := envCheckCommandFixture(t, "SECRET_KEY=x\nOTHER=1\n", "SECRET_KEY=real\nOTHER=1\n")
	utils.JSONOutput = true
	t.Cleanup(func() { utils.JSONOutput = false })
	code := stubExit(t)

	out := captureStdout(t, func() {
		if err := runEnvCheck(c, nil); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	var doc struct {
		OK       bool                `json:"ok"`
		Services []utils.EnvCheckRow `json:"services"`
		Reason   string              `json:"reason"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("stdout not pure JSON: %q err=%v", out, err)
	}
	if !doc.OK || len(doc.Services) != 1 || doc.Reason != "" {
		t.Fatalf("want ok with one service, got %+v", doc)
	}
	if *code != -1 {
		t.Fatalf("clean check must not exit, got code %d", *code)
	}
}

func TestRunEnvCheckHumanFindingExitsOne(t *testing.T) {
	c := envCheckCommandFixture(t, "SECRET_KEY=x\nOTHER=1\n", "OTHER=1\n")
	code := stubExit(t)

	out := captureStdout(t, func() {
		if err := runEnvCheck(c, nil); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	if !strings.Contains(out, "SECRET_KEY") {
		t.Fatalf("summary must name the missing key:\n%s", out)
	}
	if *code != 1 {
		t.Fatalf("missing key must exit 1, got %d", *code)
	}
}

// Zero checked services is a finding: the JSON carries a reason and the run
// exits non-zero, so a misconfigured CI gate cannot read vacuous as green.
func TestRunEnvCheckJSONNothingChecked(t *testing.T) {
	c := envCheckCommandFixture(t, "", "")
	utils.JSONOutput = true
	t.Cleanup(func() { utils.JSONOutput = false })
	code := stubExit(t)

	out := captureStdout(t, func() {
		if err := runEnvCheck(c, nil); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	if !strings.Contains(out, utils.EnvCheckNothingChecked) {
		t.Fatalf("want reason %q in:\n%s", utils.EnvCheckNothingChecked, out)
	}
	if *code != 1 {
		t.Fatalf("vacuous run must exit 1, got %d", *code)
	}
}

func TestRunEnvCheckUnknownServiceArg(t *testing.T) {
	c := envCheckCommandFixture(t, "KEY=1\n", "KEY=1\n")
	stubExit(t)
	err := runEnvCheck(c, []string{"nope"})
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("want unknown-service error, got %v", err)
	}
}

func TestRunEnvCheckNoComposeErrors(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(cwd) })

	_, c := newTestComposeCommand()
	c.Flags().String("file", "", "")
	err := runEnvCheck(c, nil)
	if err == nil || !strings.Contains(err.Error(), utils.ErrComposeNotFound) {
		t.Fatalf("want %s error, got %v", utils.ErrComposeNotFound, err)
	}
}

func TestRunEnvCheckFileOverride(t *testing.T) {
	c := envCheckCommandFixture(t, "SECRET_KEY=x\n", "")
	if err := os.WriteFile(filepath.Join("api", ".env.ci"), []byte("SECRET_KEY=ci\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.Flags().Set("file", ".env.ci"); err != nil {
		t.Fatal(err)
	}
	code := stubExit(t)

	out := captureStdout(t, func() {
		if err := runEnvCheck(c, nil); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	if !strings.Contains(out, "✅") {
		t.Fatalf("override file covers the example, want pass:\n%s", out)
	}
	if *code != -1 {
		t.Fatalf("clean override check must not exit, got %d", *code)
	}
}
