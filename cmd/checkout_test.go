package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils"
)

func checkoutCompose(names ...string) *utils.CorgiCompose {
	corgi := &utils.CorgiCompose{}
	for _, name := range names {
		corgi.Services = append(corgi.Services, utils.Service{ServiceName: name, AbsolutePath: "/repos/" + name})
	}
	return corgi
}

func resetCheckoutFlags(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		checkoutOnlyServices = nil
		checkoutSkipWorkspace = false
		checkoutAllowDirty = false
	})
}

func TestCheckoutServiceFilter(t *testing.T) {
	resetCheckoutFlags(t)
	corgi := checkoutCompose("api", "web", "mobile")

	checkoutOnlyServices = nil
	if only, err := checkoutServiceFilter(corgi); only != nil || err != nil {
		t.Errorf("no --service should mean no filter, got (%v, %v)", only, err)
	}

	checkoutOnlyServices = []string{"api,web", " mobile "}
	only, err := checkoutServiceFilter(corgi)
	if err != nil {
		t.Fatal(err)
	}
	if len(only) != 3 || !only["api"] || !only["web"] || !only["mobile"] {
		t.Errorf("filter = %v, want all three", only)
	}

	checkoutOnlyServices = []string{"nope"}
	if _, err := checkoutServiceFilter(corgi); err == nil {
		t.Error("an unknown service name must be an error")
	}
}

func TestCheckoutTargetsIncludeWorkspaceOnlyWhenUnfiltered(t *testing.T) {
	resetCheckoutFlags(t)
	previous := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = "/ws"
	t.Cleanup(func() { utils.CorgiComposePathDir = previous })

	corgi := checkoutCompose("api", "web")

	targets := checkoutTargets(corgi, nil)
	if len(targets) != 3 || targets[0].name != workspaceRepoName || targets[0].path != "/ws" {
		t.Fatalf("targets = %+v, want workspace + 2 services", targets)
	}
	if targets[1].name != "api" || targets[2].name != "web" {
		t.Errorf("services = %+v, want them sorted by name", targets[1:])
	}

	targets = checkoutTargets(corgi, map[string]bool{"web": true})
	if len(targets) != 1 || targets[0].name != "web" {
		t.Fatalf("filtered targets = %+v, want web only", targets)
	}

	checkoutSkipWorkspace = true
	targets = checkoutTargets(corgi, nil)
	if len(targets) != 2 {
		t.Fatalf("--skip-workspace targets = %+v, want the 2 services", targets)
	}
}

func TestCheckoutTargetsSkipServiceWithoutPath(t *testing.T) {
	resetCheckoutFlags(t)
	checkoutSkipWorkspace = true
	corgi := checkoutCompose("api")
	corgi.Services = append(corgi.Services, utils.Service{ServiceName: "noPath"})

	targets := checkoutTargets(corgi, nil)
	if len(targets) != 1 || targets[0].name != "api" {
		t.Fatalf("targets = %+v, want api only", targets)
	}
}

func TestCheckoutOneSkipsSecondServiceInSameRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	resetCheckoutFlags(t)
	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", repo}, args...)...)
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")
	nested := filepath.Join(repo, "packages", "web")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	handledBy := map[string]string{}
	first := checkoutOne(checkoutTarget{name: "api", path: repo}, "main", handledBy)
	if first.Status == utils.CheckoutSkipped {
		t.Fatalf("first service should be handled, got %+v", first)
	}
	second := checkoutOne(checkoutTarget{name: "web", path: nested}, "main", handledBy)
	if second.Status != utils.CheckoutSkipped || !strings.Contains(second.Message, "same repo as api") {
		t.Fatalf("second service in the same repo = %+v, want skipped", second)
	}
}

func TestCheckoutRowAndNote(t *testing.T) {
	row := checkoutRow(utils.RepoCheckout{Name: "api", Branch: "master", Status: utils.CheckoutUpdated, Fallback: true}, 6)
	if !strings.Contains(row, "✔ api   ") || !strings.Contains(row, "master") || !strings.Contains(row, "(default branch)") {
		t.Errorf("row = %q", row)
	}

	failed := checkoutRow(utils.RepoCheckout{Name: "web", Status: utils.CheckoutFailed, Message: "boom"}, 3)
	if !strings.HasPrefix(failed, "✖ web") || !strings.Contains(failed, "-") || !strings.Contains(failed, "failed: boom") {
		t.Errorf("failed row = %q", failed)
	}

	skipped := checkoutRow(utils.RepoCheckout{Name: "db", Status: utils.CheckoutSkipped, Message: "not a git repository"}, 3)
	if !strings.HasPrefix(skipped, "• db") {
		t.Errorf("skipped row = %q", skipped)
	}
}

func TestCountCheckoutStatus(t *testing.T) {
	results := []utils.RepoCheckout{
		{Status: utils.CheckoutUpdated},
		{Status: utils.CheckoutUpdated},
		{Status: utils.CheckoutFailed},
	}
	if got := countCheckoutStatus(results, utils.CheckoutUpdated); got != 2 {
		t.Errorf("updated = %d, want 2", got)
	}
	if got := countCheckoutStatus(results, utils.CheckoutSkipped); got != 0 {
		t.Errorf("skipped = %d, want 0", got)
	}
}
