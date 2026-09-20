package cmd

import (
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils/agent/config"
)

func TestAgentsOrderIsParsedSetAndShown(t *testing.T) {
	agentD, _, _ := claudeHome(t)

	if _, err := parseAgents([]string{"claude,grok"}); err == nil || !strings.Contains(err.Error(), "grok") {
		t.Fatalf("an unknown agent is refused by name: %v", err)
	}
	order, err := parseAgents([]string{"Claude", "codex,claude"})
	if err != nil || len(order) != 2 || order[0] != "claude" || order[1] != "codex" {
		t.Fatalf("%v %v", order, err)
	}

	if err := setAgents("client", order, false); err != nil {
		t.Fatal(err)
	}
	user, err := config.LoadUser(agentUserConfigPath(agentD))
	if err != nil {
		t.Fatal(err)
	}
	if got := user.Workspaces["client"].Agents; len(got) != 2 {
		t.Fatalf("the order is written: %v", got)
	}
	if line := workspaceSettingsLine(user, "client"); !strings.HasPrefix(line, "claude → codex") || !strings.Contains(line, "account claude-client") {
		t.Fatalf("the listing shows the order and the account: %q", line)
	}

	// one agent that is not claude is kind, the old field; claude alone is nothing
	if err := setAgents("client", []string{"codex"}, false); err != nil {
		t.Fatal(err)
	}
	user, _ = config.LoadUser(agentUserConfigPath(agentD))
	if w := user.Workspaces["client"]; w.Kind != "codex" || len(w.Agents) != 0 {
		t.Fatalf("one agent is kind: %+v", w)
	}
	if err := setAgents("client", []string{"claude"}, false); err != nil {
		t.Fatal(err)
	}
	user, _ = config.LoadUser(agentUserConfigPath(agentD))
	if w := user.Workspaces["client"]; w.Kind != "" || len(w.Agents) != 0 {
		t.Fatalf("claude alone is the default, nothing written: %+v", w)
	}

	if err := setAgents("", []string{"claude", "codex"}, true); err != nil {
		t.Fatal(err)
	}
	user, _ = config.LoadUser(agentUserConfigPath(agentD))
	if got := config.Resolve("mine", nil, user).AgentOrder(); len(got) != 2 {
		t.Fatalf("--default reaches every workspace without its own: %v", got)
	}
	if err := setAgents("nope", order, false); err == nil {
		t.Fatal("an unknown workspace is refused")
	}
	if ws := workspaceAgents("mine"); len(ws) != 2 {
		t.Fatalf("the launcher tells the phone the order: %v", ws)
	}
	if err := setAgents("", []string{"claude"}, true); err != nil {
		t.Fatal(err)
	}
	if ws := workspaceAgents("mine"); ws != nil {
		t.Fatalf("claude alone is not worth a field: %v", ws)
	}
}
