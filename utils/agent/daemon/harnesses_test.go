package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/harness"
	"andriiklymiuk/corgi/utils/agent/watch"
)

func installedOnly(names ...string) func() {
	prev := harnessInstalled
	harnessInstalled = func(h harness.Harness) bool {
		for _, n := range names {
			if n == h.Name {
				return true
			}
		}
		return false
	}
	prevSpent := codexWindowSpent
	codexWindowSpent = func(time.Time) bool { return false }
	return func() { harnessInstalled, codexWindowSpent = prev, prevSpent }
}

func TestASpentCodexWindowHandsTheRunBack(t *testing.T) {
	defer installedOnly("claude", "codex")()
	codexWindowSpent = func(time.Time) bool { return true }
	now := time.Now()
	spec := WatchSpec{Workspace: "api", Agents: []string{"codex", "claude"}}
	h, skipped := pickHarness(spec, &watch.FixLog{}, now)
	if h.Name != "claude" || len(skipped) != 1 || skipped[0] != "codex is at its limit" {
		t.Fatalf("claude should take it: %s %v", h.Name, skipped)
	}
	if hasFallback(WatchSpec{Workspace: "api", Agents: []string{"claude", "codex"}}, &watch.FixLog{}, now) {
		t.Fatal("a spent codex is no fallback")
	}
}

func TestTheNextAgentTakesARunWhenTheFirstCannot(t *testing.T) {
	defer installedOnly("claude", "codex")()
	now := time.Now()
	spec := WatchSpec{Workspace: "api", Agents: []string{"claude", "codex"}}

	h, skipped := pickHarness(spec, &watch.FixLog{}, now)
	if h.Name != "claude" || len(skipped) != 0 {
		t.Fatalf("nothing wrong: claude runs, got %s %v", h.Name, skipped)
	}

	// claude's newest run of the day hit its limit: codex takes the next one.
	log := &watch.FixLog{Started: []watch.FixRecord{
		{Key: "k1", Workspace: "api", Ref: "ENG-1", StartedAt: now.Add(-time.Hour), FinishedAt: now.Add(-30 * time.Minute), Failure: watch.FailureLimit},
	}}
	h, skipped = pickHarness(spec, log, now)
	if h.Name != "codex" || len(skipped) != 1 || skipped[0] != "claude is at its limit" {
		t.Fatalf("codex should take it: %s %v", h.Name, skipped)
	}
	if !hasFallback(spec, log, now) {
		t.Fatal("a spent window is no reason to defer when codex can run")
	}
	if laptopUnwell(spec, log, now) != "" {
		t.Fatal("the laptop can still run fixes, through codex")
	}

	// codex failed on its own login since: nobody can; the first is returned
	// and the peers hear claude's reason.
	log.Started = append(log.Started, watch.FixRecord{Key: "k2", Workspace: "api", Ref: "ENG-2", Harness: "codex", StartedAt: now.Add(-20 * time.Minute), FinishedAt: now.Add(-10 * time.Minute), Failure: watch.FailureNoAuth})
	h, skipped = pickHarness(spec, log, now)
	if h.Name != "claude" || len(skipped) != 2 {
		t.Fatalf("no agent can: %s %v", h.Name, skipped)
	}
	if laptopUnwell(spec, log, now) != "limit" {
		t.Fatalf("the peers hear the first agent's reason: %q", laptopUnwell(spec, log, now))
	}

	// a codex run that failed does not sideline claude
	only := &watch.FixLog{Started: []watch.FixRecord{log.Started[1]}}
	if h, _ := pickHarness(spec, only, now); h.Name != "claude" {
		t.Fatalf("codex's failure is codex's: %s", h.Name)
	}
	// a run from before this change has no harness on it: it was claude's
	if h, _ := pickHarness(spec, &watch.FixLog{Started: []watch.FixRecord{{Key: "k3", Workspace: "api", FinishedAt: now.Add(-time.Minute), Failure: watch.FailurePermission}}}, now); h.Name != "codex" {
		t.Fatalf("an untagged failure is claude's: %s", h.Name)
	}
}

func TestAMissingAgentIsSkippedAndTheOrderIsOneAgentByDefault(t *testing.T) {
	defer installedOnly("claude")()
	now := time.Now()
	spec := WatchSpec{Workspace: "api", Agents: []string{"codex", "claude"}}
	h, skipped := pickHarness(spec, nil, now)
	if h.Name != "claude" || len(skipped) != 1 || skipped[0] != "codex is not installed" {
		t.Fatalf("%s %v", h.Name, skipped)
	}
	if laptopUnwell(WatchSpec{Workspace: "api", Agents: []string{"codex"}}, nil, now) != "no-credential" {
		t.Fatal("an agent that is not installed cannot run fixes")
	}
	plain := WatchSpec{Workspace: "api", Kind: "codex"}
	if got := plain.agents(); len(got) != 1 || got[0] != "codex" {
		t.Fatalf("kind alone is a one-agent order: %v", got)
	}
	none := WatchSpec{}
	if got := none.agents(); len(got) != 1 || got[0] != "claude" {
		t.Fatalf("nothing set is claude: %v", got)
	}
}

func TestAFallbackRunsUnderItsOwnHome(t *testing.T) {
	spec := WatchSpec{Workspace: "api", ConfigDir: "/home/me/.claude-work", Agents: []string{"claude", "codex"}}
	env := []string{"CLAUDE_CONFIG_DIR=/home/me/.claude-work", "CORGI_OMIT=useAwsVpn"}
	if got := harnessEnv(spec, harness.For("claude", ""), env); len(got) != 2 {
		t.Fatalf("claude keeps its config dir: %v", got)
	}
	if got := harnessEnv(spec, harness.For("codex", ""), env); len(got) != 1 || got[0] != "CORGI_OMIT=useAwsVpn" {
		t.Fatalf("codex must not inherit claude's config dir: %v", got)
	}
}

func TestThePulseKnowsAFallbackCouldRun(t *testing.T) {
	defer installedOnly("claude", "codex")()
	dir := t.TempDir()
	now := time.Now()
	log := &watch.FixLog{Started: []watch.FixRecord{
		{Key: "k1", Workspace: "api", FinishedAt: now.Add(-time.Minute), Failure: watch.FailureNoAuth},
	}}
	if unwellFromFiles(dir, log, -1, now) != watch.FailureNoAuth {
		t.Fatal("no agents file: the old answer")
	}
	_ = os.MkdirAll(filepath.Join(dir, "watch"), 0o700)
	data, _ := json.Marshal(map[string]watchAgents{"api": {Agents: []string{"claude", "codex"}}})
	_ = os.WriteFile(WatchAgentsPath(dir), data, 0o600)
	if got := unwellFromFiles(dir, log, -1, now); got != "" {
		t.Fatalf("codex can take the runs, so the laptop is not unwell: %q", got)
	}
}

func TestAWebhookNobodyListsIsDropped(t *testing.T) {
	gh := namedSource{name: "github"}
	listed := WatchSpec{Workspace: "work", Repos: []string{"acme/api"}, Sources: []watch.Source{gh}}
	anyRepo := WatchSpec{Workspace: "open", Sources: []watch.Source{gh}}
	noGitHub := WatchSpec{Workspace: "im", Sources: []watch.Source{namedSource{name: "gitlab"}}}
	e := watch.Event{Source: "github", Kind: watch.KindPRComment, Ref: "stranger/repo#3"}
	if listed.mayTake(e) {
		t.Fatal("a repo list that does not name it says no")
	}
	if !anyRepo.mayTake(e) {
		t.Fatal("a workspace watching every repo takes it")
	}
	if noGitHub.mayTake(e) {
		t.Fatal("a workspace that does not watch github never takes a github webhook")
	}
	ticket := watch.Event{Source: "linear", Kind: watch.KindIssueComment, Ref: "ABC-1"}
	if (WatchSpec{Project: "HUM", Sources: []watch.Source{namedSource{name: "linear"}}}).mayTake(ticket) {
		t.Fatal("a ticket of another project stays out")
	}
}
