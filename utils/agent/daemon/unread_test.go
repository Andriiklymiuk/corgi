package daemon

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/watch"
)

type unreadBench struct {
	d      *Daemon
	spec   WatchSpec
	link   string
	ref    string
	merged atomic.Int32
	ready  atomic.Int32
	judged atomic.Int32
	share  atomic.Int32
	diff   watch.PullChanges
}

// A checkout at one commit, the run that opened the pull request, and a forge
// that says it is small and green. Each test breaks one thing.
func lightsOut(t *testing.T) *unreadBench {
	t.Helper()
	d := testDaemon(t)
	d.watchState = watch.LoadState(d.Dir)
	tree := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.email=a@b.c", "-c", "user.name=a", "commit", "-q", "--allow-empty", "-m", "x"}} {
		if out, err := exec.Command("git", append([]string{"-C", tree}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %s", err, out)
		}
	}
	out, err := exec.Command("git", "-C", tree, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	head := strings.TrimSpace(string(out))
	b := &unreadBench{d: d, link: "https://github.com/a/b/pull/7", ref: "a/b#7"}
	b.spec = WatchSpec{Workspace: "w", Dir: tree, AgentDir: d.Dir, Action: "fix", MergeUnread: true}
	d.Watches = []WatchSpec{b.spec}
	b.diff = watch.PullChanges{Head: head, Body: "<!-- corgi-risk score=2 tier=trivial -->", Files: []watch.ChangedFile{{Path: "src/total.ts", Lines: 12}}}
	b.share.Store(100)
	d.PullChanges = func(context.Context, string, string) (watch.PullChanges, error) { return b.diff, nil }
	d.ReadyPull = func(context.Context, string, string) error { b.ready.Add(1); return nil }
	d.MergePull = func(context.Context, string, string) error { b.merged.Add(1); return nil }
	d.Isolate = func(string, string) ([]string, error) { return []string{tree}, nil }
	d.JudgeScenarios = func(context.Context, string, string, string) (int, int, error) {
		b.judged.Add(1)
		return int(b.share.Load()), 2, nil
	}
	b.run(t, watch.Event{Key: "task:1", Workspace: "w", Kind: watch.KindTask, Ref: "TASK-1"}, "")
	return b
}

func (b *unreadBench) run(t *testing.T, e watch.Event, failure string) {
	t.Helper()
	log := b.d.fixLog()
	log.StartFor(e, time.Now().Add(-time.Hour))
	log.SetBranch(e.Key, "corgi/task-1")
	log.Finish(e.Key, []string{b.link}, "", failure, time.Now().Add(-time.Minute))
	if failure == "" {
		records := watch.LoadFixLog(b.d.Dir).Records()
		if len(records) == 0 {
			t.Fatal("no run")
		}
	}
}

var green = watch.PullStatus{State: "draft", Checks: "passing", Review: "none", Mine: true}

// settle asks until the judge has answered, the way refreshes do.
func (b *unreadBench) settle(st watch.PullStatus) {
	for range 400 {
		before := b.merged.Load()
		b.d.pullChanged(context.Background(), b.spec, b.ref, b.link, st, st, true)
		if b.merged.Load() > before {
			return // the real loop stops asking about a merged pull request
		}
		v, judged := b.d.unreadJudged.Load(b.link + "|" + b.diff.Head)
		if judged && v.(int) != unreadJudging {
			b.d.pullChanged(context.Background(), b.spec, b.ref, b.link, st, st, true)
			return
		}
		if !judged && b.judged.Load() == 0 && b.d.JudgeScenarios != nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestASmallGreenPullRequestARunOpenedMergesWithNobodyReading(t *testing.T) {
	b := lightsOut(t)
	b.settle(green)
	if b.ready.Load() == 0 || b.merged.Load() != 1 || b.judged.Load() != 1 {
		t.Fatalf("ready %d merged %d judged %d", b.ready.Load(), b.merged.Load(), b.judged.Load())
	}
	got, _ := watch.LoadPullLog(b.d.Dir).Get(b.ref)
	if got.State != "merged" {
		t.Fatalf("%+v", got)
	}
}

func TestNothingMergesUntilTheJudgeHasAnswered(t *testing.T) {
	b := lightsOut(t)
	release := make(chan struct{})
	b.d.JudgeScenarios = func(context.Context, string, string, string) (int, int, error) {
		<-release
		return 100, 2, nil
	}
	b.d.pullChanged(context.Background(), b.spec, b.ref, b.link, green, green, true)
	b.d.pullChanged(context.Background(), b.spec, b.ref, b.link, green, green, true)
	if b.merged.Load() != 0 || b.ready.Load() != 0 {
		t.Fatal("merged before the verdict")
	}
	close(release)
	b.settle(green)
	if b.merged.Load() != 1 {
		t.Fatal("the verdict did not merge it")
	}
}

func TestAnythingShortOfTheGateWaitsForAPerson(t *testing.T) {
	cases := map[string]func(*unreadBench, *watch.PullStatus){
		"switch off":         func(b *unreadBench, _ *watch.PullStatus) { b.spec.MergeUnread = false },
		"not mine":           func(_ *unreadBench, st *watch.PullStatus) { st.Mine = false },
		"checks red":         func(_ *unreadBench, st *watch.PullStatus) { st.Checks = "failing" },
		"checks running":     func(_ *unreadBench, st *watch.PullStatus) { st.Checks = "pending" },
		"no checks at all":   func(_ *unreadBench, st *watch.PullStatus) { st.Checks = "none" },
		"changes asked":      func(_ *unreadBench, st *watch.PullStatus) { st.Review = "changes" },
		"touches auth":       func(b *unreadBench, _ *watch.PullStatus) { b.diff.Files[0].Path = "src/auth.ts" },
		"high risk":          func(b *unreadBench, _ *watch.PullStatus) { b.diff.Body = "<!-- corgi-risk score=7 tier=high -->" },
		"no risk card":       func(b *unreadBench, _ *watch.PullStatus) { b.diff.Body = "" },
		"scenarios missed":   func(b *unreadBench, _ *watch.PullStatus) { b.share.Store(50) },
		"commit not on disk": func(b *unreadBench, _ *watch.PullStatus) { b.diff.Head = "0000" },
		"later run failed": func(b *unreadBench, _ *watch.PullStatus) {
			log := b.d.fixLog()
			log.StartFor(watch.Event{Key: "rev", Workspace: "w", Kind: watch.KindPRComment, Ref: b.ref, URL: b.link}, time.Now())
			log.Finish("rev", nil, "", "boom", time.Now())
		},
		"forge cannot say": func(b *unreadBench, _ *watch.PullStatus) {
			b.d.PullChanges = func(context.Context, string, string) (watch.PullChanges, error) {
				return watch.PullChanges{}, errors.New("HTTP 500")
			}
		},
		"nothing to judge with": func(b *unreadBench, _ *watch.PullStatus) {
			b.d.JudgeScenarios = func(context.Context, string, string, string) (int, int, error) { return 0, 0, nil }
		},
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			b := lightsOut(t)
			st := green
			breakIt(b, &st)
			b.settle(st)
			if b.merged.Load() != 0 || b.ready.Load() != 0 {
				t.Fatalf("merged %d ready %d", b.merged.Load(), b.ready.Load())
			}
		})
	}
}

func TestAPullRequestAPersonOpenedIsNeverMergedUnread(t *testing.T) {
	b := lightsOut(t)
	b.link, b.ref = "https://github.com/a/b/pull/8", "a/b#8"
	b.settle(green)
	if b.merged.Load() != 0 || b.judged.Load() != 0 {
		t.Fatalf("merged %d judged %d", b.merged.Load(), b.judged.Load())
	}
}

func TestAPushAfterTheVerdictIsJudgedAgain(t *testing.T) {
	b := lightsOut(t)
	b.share.Store(50)
	b.settle(green)
	if b.merged.Load() != 0 || b.judged.Load() != 1 {
		t.Fatalf("merged %d judged %d", b.merged.Load(), b.judged.Load())
	}
	tree := b.spec.Dir
	if err := os.WriteFile(filepath.Join(tree, "f"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"-c", "user.email=a@b.c", "-c", "user.name=a", "commit", "-q", "-m", "y"}} {
		if out, err := exec.Command("git", append([]string{"-C", tree}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %s", err, out)
		}
	}
	out, _ := exec.Command("git", "-C", tree, "rev-parse", "HEAD").Output()
	b.diff.Head = strings.TrimSpace(string(out))
	b.share.Store(100)
	b.merged.Store(0)
	b.settle(green)
	if b.merged.Load() != 1 || b.judged.Load() != 2 {
		t.Fatalf("merged %d judged %d", b.merged.Load(), b.judged.Load())
	}
}

func TestARunAtLevelFiveIsToldToStampTheRiskCard(t *testing.T) {
	e := watch.Event{Key: "k", Kind: watch.KindTask, Ref: "TASK-1"}
	if s := unattendedSuffix(WatchSpec{Workspace: "w", MergeUnread: true}, e); !strings.Contains(s, "/corgi:risk") {
		t.Fatal(s)
	}
	if s := unattendedSuffix(WatchSpec{Workspace: "w"}, e); strings.Contains(s, "/corgi:risk") {
		t.Fatal(s)
	}
}

func TestAutoMergeAloneStillWantsAnApproval(t *testing.T) {
	b := lightsOut(t)
	b.spec.MergeUnread = false
	st := green
	st.State = "open"
	b.settle(st)
	if b.merged.Load() != 0 {
		t.Fatal("no approval, no switch, no merge")
	}
}

func TestEveryRunThatCanPostIsToldToProveWhatItPosts(t *testing.T) {
	for _, kind := range []watch.Kind{watch.KindTask, watch.KindIssueNew, watch.KindPRComment, watch.KindReviewRequested} {
		s := unattendedSuffix(WatchSpec{Workspace: "w"}, watch.Event{Key: "k", Kind: kind, Ref: "R"})
		if !strings.Contains(s, "not checked") || !strings.Contains(s, "never corrected by another comment") {
			t.Errorf("%s: %s", kind, s)
		}
	}
}
