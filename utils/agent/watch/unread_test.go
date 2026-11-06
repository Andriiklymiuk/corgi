package watch

import (
	"strings"
	"testing"
	"time"
)

const lowRisk = "does a thing\n<!-- corgi-risk score=2 tier=trivial -->\ncard\n<!-- /corgi-risk -->"

func oneFile(path string) PullChanges {
	return PullChanges{Files: []ChangedFile{{Path: path, Lines: 10}}, Body: lowRisk, Head: "abc"}
}

func TestASmallPlainChangeWithALowCardMayMergeUnread(t *testing.T) {
	for _, path := range []string{"src/cart/total.ts", "docs/guide.md", "pkg/authors/list.go", "web/Home.test.tsx"} {
		if why := UnreadRefusal(oneFile(path)); why != "" {
			t.Errorf("%s: %s", path, why)
		}
	}
}

func TestWhatBreaksBadlyAlwaysWaitsForAPerson(t *testing.T) {
	for _, path := range []string{
		"src/auth/login.ts", "src/AuthService.ts", "api/sessions.go", "lib/jwt.go", "config/.env.example", "pkg/secrets.go",
		"web/checkout/total.ts", "api/billing/plan.go", "PaymentForm.tsx",
		"db/migrations/001.sql", "prisma/schema.prisma", "schema.graphql",
		".github/workflows/ci.yml", ".gitlab-ci.yml", "Dockerfile", "deploy/k8s/app.yaml", "fastlane/Fastfile", "Makefile", "corgi-compose.yml",
		"go.mod", "package.json", "package-lock.json", "ios/Podfile", "android/app/build.gradle", "app.json",
		"vitest.config.ts", "eslint.config.mjs", "sonar-project.properties",
	} {
		if why := UnreadRefusal(oneFile(path)); why == "" {
			t.Errorf("%s went through", path)
		}
	}
}

func TestACardCanHoldAChangeBackButNotWaveItThrough(t *testing.T) {
	c := oneFile("src/cart/total.ts")
	c.Body = "no card"
	if why := UnreadRefusal(c); why != "no risk card" {
		t.Fatal(why)
	}
	c.Body = "<!-- corgi-risk score=3 tier=low -->"
	if why := UnreadRefusal(c); why != "risk 3" {
		t.Fatal(why)
	}
	auth := oneFile("src/auth/login.ts")
	auth.Body = "<!-- corgi-risk score=1 tier=trivial -->"
	if why := UnreadRefusal(auth); !strings.Contains(why, "auth") {
		t.Fatalf("a low card does not pass auth: %q", why)
	}
}

func TestSizeAndMissingFilesWait(t *testing.T) {
	big := PullChanges{Files: []ChangedFile{{Path: "a.go", Lines: 401}}, Body: lowRisk}
	if why := UnreadRefusal(big); !strings.Contains(why, "401") {
		t.Fatal(why)
	}
	wide := PullChanges{Body: lowRisk}
	for range 26 {
		wide.Files = append(wide.Files, ChangedFile{Path: "a.go", Lines: 1})
	}
	if why := UnreadRefusal(wide); !strings.Contains(why, "26 files") {
		t.Fatal(why)
	}
	if why := UnreadRefusal(PullChanges{Body: lowRisk}); why == "" {
		t.Fatal("no files listed")
	}
	partial := oneFile("a.go")
	partial.Partial = true
	if why := UnreadRefusal(partial); why == "" {
		t.Fatal("not all listed")
	}
}

func TestRemovingATestWaits(t *testing.T) {
	c := PullChanges{Files: []ChangedFile{{Path: "web/Home.test.tsx", Lines: 40, Removed: true}}, Body: lowRisk}
	if why := UnreadRefusal(c); !strings.Contains(why, "removes") {
		t.Fatal(why)
	}
}

func TestOnlyACleanRunMayMergeUnread(t *testing.T) {
	now := time.Now()
	for want, r := range map[string]FixRecord{
		"still going": {},
		"failed":      {FinishedAt: now, Error: "boom"},
		"login":       {FinishedAt: now, Failure: "no-credential"},
		"skipped":     {FinishedAt: now, Gaps: []Gap{GapTestsSkip}},
	} {
		if why := UnreadRunRefusal(r); why == "" {
			t.Errorf("%s went through", want)
		}
	}
	if why := UnreadRunRefusal(FixRecord{FinishedAt: now}); why != "" {
		t.Fatal(why)
	}
}

func TestOnlyAPullRequestARunOpenedCounts(t *testing.T) {
	log := LoadFixLog(t.TempDir())
	now := time.Now()
	link := "https://github.com/a/b/pull/7"
	log.StartFor(Event{Key: "rev", Workspace: "w", Kind: KindPRComment, Ref: "a/b#7"}, now)
	log.Finish("rev", []string{link}, "", "", now)
	if _, ok := log.RunThatStartedPull("w", link); ok {
		t.Fatal("a run that fixed review comments did not start it")
	}
	log.StartFor(Event{Key: "task:1", Workspace: "w", Kind: KindTask, Ref: "TASK-1"}, now)
	log.Finish("task:1", []string{link + "/files"}, "", "", now)
	if r, ok := log.RunThatStartedPull("w", link); !ok || r.Key != "task:1" {
		t.Fatalf("%+v", r)
	}
	if _, ok := log.RunThatStartedPull("other", link); ok {
		t.Fatal("another workspace")
	}
}

func TestParsingWhatTheForgesSay(t *testing.T) {
	pull := `{"body":"b","head":{"sha":"abc"},"changed_files":2}`
	files := `[{"filename":"a.go","status":"modified","changes":5},{"filename":"new.go","previous_filename":"src/auth.go","status":"renamed","changes":0}]`
	c, err := parseGitHubChanges([]byte(pull), []byte(files))
	if err != nil || c.Head != "abc" || c.Partial || len(c.Files) != 3 {
		t.Fatalf("%+v %v", c, err)
	}
	if why := UnreadRefusal(PullChanges{Files: c.Files, Body: lowRisk}); !strings.Contains(why, "auth") {
		t.Fatalf("a rename out of auth still touches auth: %q", why)
	}
	c, _ = parseGitHubChanges([]byte(`{"changed_files":200}`), []byte(`[{"filename":"a.go"}]`))
	if !c.Partial {
		t.Fatal("the forge listed fewer than it has")
	}
	mr := `{"description":"d","sha":"def","changes":[{"new_path":"a.go","old_path":"a.go","diff":"--- a\n+++ b\n+one\n-two\n ctx\n"},{"new_path":"gone.go","old_path":"gone.go","deleted_file":true,"diff":"-x\n"}]}`
	c, err = parseGitLabChanges([]byte(mr))
	if err != nil || c.Head != "def" || c.Files[0].Lines != 2 || !c.Files[1].Removed {
		t.Fatalf("%+v %v", c, err)
	}
	c, _ = parseGitLabChanges([]byte(`{"overflow":true}`))
	if !c.Partial {
		t.Fatal("overflow")
	}
}
