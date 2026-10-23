package daemon

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestAnIsolatedRunIsToldAboutItsDatabasesAndCleanedUpAfter(t *testing.T) {
	for _, mode := range []string{"own", "shared"} {
		t.Run(mode, func(t *testing.T) {
			d := testDaemon(t)
			d.Isolate = func(dir, branch string) ([]string, error) { return []string{dir}, nil }
			var mu sync.Mutex
			var asked, released []string
			d.RunStack = func(dir, branch, m string) string {
				mu.Lock()
				defer mu.Unlock()
				asked = append(asked, branch+" "+m)
				return "\n\nTHE STACK NOTE"
			}
			d.ReleaseStack = func(dir, branch string) {
				mu.Lock()
				defer mu.Unlock()
				released = append(released, branch)
			}
			var prompt string
			prev := claudeCommand
			claudeCommand = func(ctx context.Context, dir string, env []string, args ...string) *exec.Cmd {
				mu.Lock()
				prompt = strings.Join(args, " ")
				mu.Unlock()
				return exec.CommandContext(ctx, "echo", `{"result":"Outcome: pushed","total_cost_usd":0.01}`)
			}
			t.Cleanup(func() { claudeCommand = prev })
			spec := WatchSpec{Workspace: "acme", Dir: t.TempDir(), AgentDir: d.Dir, Rules: watch.Rules{Enabled: true}, Action: "fix", Isolate: true, RunDB: mode}
			d.Watches = []WatchSpec{spec}
			d.loadWatchFiles()
			d.fixBusy = map[string]chan struct{}{"acme": make(chan struct{}, 1)}
			d.runFix(context.Background(), spec, watch.Event{Key: "k1", Workspace: "acme", Ref: "ABC-7", Kind: watch.KindIssueNew})
			mu.Lock()
			defer mu.Unlock()
			if len(asked) != 1 || asked[0] != "corgi/abc-7 "+mode || !strings.Contains(prompt, "THE STACK NOTE") {
				t.Fatalf("the run is told how to get its databases: %v %q", asked, prompt)
			}
			if mode == "own" && len(released) != 1 {
				t.Fatalf("its own databases are stopped after the run: %v", released)
			}
			if mode == "shared" && len(released) != 0 {
				t.Fatalf("the workspace's shared databases are never stopped by a run: %v", released)
			}
		})
	}
}
