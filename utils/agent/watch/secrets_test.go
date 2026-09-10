package watch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceTokensBeatTheMachineWideOnesAndTheEnvironment(t *testing.T) {
	dir := t.TempDir()

	if err := SaveSecrets(dir, Secrets{Linear: "global-linear", GitHub: "global-gh", HookSecret: "hook"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveWorkspaceSecrets(dir, "acme", Secrets{
		JiraURL: "https://acme.atlassian.net", JiraEmail: "me@acme.com", JiraToken: "acme-jira", Me: "andrii",
	}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LINEAR_API_KEY", "env-linear")

	global := LoadSecrets(dir)
	if global.Linear != "env-linear" || global.JiraToken != "" {
		t.Fatalf("machine-wide = %+v", global)
	}

	acme := LoadSecretsFor(dir, "acme")
	if acme.JiraToken != "acme-jira" || acme.JiraURL != "https://acme.atlassian.net" || acme.Me != "andrii" {
		t.Errorf("the workspace's own tokens are missing: %+v", acme)
	}
	if acme.Linear != "env-linear" || acme.GitHub != "global-gh" {
		t.Errorf("a workspace should still fall back: %+v", acme)
	}

	if err := SaveWorkspaceSecrets(dir, "acme", Secrets{Linear: "acme-linear"}); err != nil {
		t.Fatal(err)
	}
	if got := LoadSecretsFor(dir, "acme").Linear; got != "acme-linear" {
		t.Errorf("a workspace token must beat the environment, got %q", got)
	}

	other := LoadSecretsFor(dir, "hobby")
	if other.Linear != "env-linear" || other.JiraToken != "" {
		t.Errorf("one workspace's tokens must not leak to another: %+v", other)
	}
}

func TestSavingMachineWideTokensKeepsEveryOverride(t *testing.T) {
	dir := t.TempDir()
	SaveWorkspaceSecrets(dir, "acme", Secrets{JiraToken: "acme"})
	SaveWorkspaceSecrets(dir, "bravo", Secrets{GitLab: "bravo", GitLabURL: "https://git.bravo.io"})

	if err := SaveSecrets(dir, Secrets{Linear: "new-global"}); err != nil {
		t.Fatal(err)
	}
	if got := WorkspacesWithSecrets(dir); len(got) != 2 || got[0] != "acme" || got[1] != "bravo" {
		t.Fatalf("overrides after a machine-wide write = %v", got)
	}
	if got := LoadSecretsFor(dir, "bravo"); got.GitLabURL != "https://git.bravo.io" || got.Linear != "new-global" {
		t.Errorf("bravo = %+v", got)
	}

	if err := SaveWorkspaceSecrets(dir, "acme", Secrets{}); err != nil {
		t.Fatal(err)
	}
	if got := WorkspacesWithSecrets(dir); len(got) != 1 || got[0] != "bravo" {
		t.Fatalf("a cleared override should be dropped, got %v", got)
	}
	if got := LoadSecretsFor(dir, "acme").JiraToken; got != "" {
		t.Errorf("acme still has a jira token: %q", got)
	}
}

func TestSecretsFileStaysPrivateAndReadsAnOlderFile(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "watch"), 0o700)
	path := filepath.Join(dir, "watch", "secrets.json")
	if err := os.WriteFile(path, []byte(`{"linear":"old","hookSecret":"h"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := LoadSecrets(dir); got.Linear != "old" || got.HookSecret != "h" {
		t.Fatalf("a file written before overrides existed = %+v", got)
	}
	if got := LoadSecretsFor(dir, "anything").Linear; got != "old" {
		t.Errorf("workspace read of an older file = %q", got)
	}

	SaveWorkspaceSecrets(dir, "acme", Secrets{JiraToken: "x"})
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("secrets file: %v %v", err, info.Mode())
	}
	if got := LoadSecrets(dir); got.Linear != "old" || got.HookSecret != "h" {
		t.Errorf("writing an override rewrote the machine-wide tokens: %+v", got)
	}
}
