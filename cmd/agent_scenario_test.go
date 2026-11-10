package cmd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestScenariosAreJudgedOncePerRunAndAFailedJudgeIsAMiss(t *testing.T) {
	orig := judgeScenario
	defer func() { judgeScenario = orig }()
	var prompts []string
	judgeScenario = func(_ context.Context, _, _, prompt string, _ []string) (string, error) {
		prompts = append(prompts, prompt)
		switch {
		case strings.Contains(prompt, "pay"):
			return `{"satisfied":true,"why":"paid"}`, nil
		case strings.Contains(prompt, "refund"):
			return `{"satisfied":false,"why":"no refund page"}`, nil
		}
		return "", errors.New("timed out")
	}
	list := []watch.Scenario{{Name: "a", Text: "pay"}, {Name: "b", Text: "refund"}, {Name: "c", Text: "other"}}
	got := runScenarios(context.Background(), list, scenarioRun{Dir: "/tmp", Model: "sonnet", Ref: "ABC-1", Times: 2}, time.Now)
	if len(got) != 6 || len(prompts) != 6 {
		t.Fatalf("got %d results from %d prompts", len(got), len(prompts))
	}
	if share := watch.Satisfaction(got); share != 33 {
		t.Fatalf("share %d", share)
	}
	if got[5].Satisfied || !strings.Contains(got[5].Why, "did not finish") || got[5].Ref != "ABC-1" {
		t.Fatalf("a dead judge: %+v", got[5])
	}
}

func TestPickScenariosKeepsOnlyTheNamed(t *testing.T) {
	all := []watch.Scenario{{Name: "a"}, {Name: "b"}}
	if got := pickScenarios(all, nil); len(got) != 2 {
		t.Fatalf("no names means all: %v", got)
	}
	if got := pickScenarios(all, []string{"b"}); len(got) != 1 || got[0].Name != "b" {
		t.Fatalf("got %v", got)
	}
}
