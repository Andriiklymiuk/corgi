package cmd

import (
	"context"
	"errors"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/watch"
	"andriiklymiuk/corgi/utils/agent/workspace"
)

func TestTheDoctorNamesARevokedSlackToken(t *testing.T) {
	dir := t.TempDir()
	if err := watch.SaveSecrets(dir, watch.Secrets{SlackUser: "xoxp-dead", SlackBot: "xoxb-fine"}); err != nil {
		t.Fatal(err)
	}
	if err := watch.SaveWorkspaceSecrets(dir, "acme", watch.Secrets{SlackUser: "xoxp-acme-dead"}); err != nil {
		t.Fatal(err)
	}
	reg := &workspace.Registry{}
	for _, id := range []string{"acme", "lab", "quiet"} {
		reg.Upsert(workspace.Workspace{ID: id, AbsPath: t.TempDir(), Status: workspace.StatusOK})
	}
	user := &config.UserConfig{Workspaces: map[string]config.WorkspaceConfig{
		"acme":  {Watch: &config.WatchConfig{Enabled: true}},
		"lab":   {Watch: &config.WatchConfig{Enabled: true}},
		"quiet": {},
	}}
	works := func(_ context.Context, token string) error {
		if strings.HasSuffix(token, "-dead") {
			return errors.New("slack auth.test: token_revoked")
		}
		return nil
	}
	checks := slackTokenChecks(dir, reg, user, works)
	byName := map[string]agentCheck{}
	for _, c := range checks {
		byName[c.Name] = c
	}
	if len(checks) != 3 {
		t.Fatalf("one row per distinct token of a watched workspace: %+v", checks)
	}
	if c := byName["slack user token · acme"]; c.OK || c.Fix != "corgi agent watch auth slack --token <new token> --workspace acme" {
		t.Fatalf("the workspace's own dead token: %+v", c)
	}
	if c := byName["slack user token"]; c.OK || c.Fix != "corgi agent watch auth slack --token <new token>" {
		t.Fatalf("the machine-wide dead token: %+v", c)
	}
	if c := byName["slack bot token"]; !c.OK {
		t.Fatalf("a live bot token: %+v", c)
	}
}
