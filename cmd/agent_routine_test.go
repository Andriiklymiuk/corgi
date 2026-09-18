package cmd

import (
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils/agent/bots"
)

func TestARoutineBotMustLiveInTheWorkspace(t *testing.T) {
	dir := t.TempDir()
	store := &bots.Store{Version: 1}
	store.Put(bots.Bot{Name: "proactive", Workspace: "api"})
	if err := bots.Save(bots.Path(dir), store); err != nil {
		t.Fatal(err)
	}
	if err := routineBotExists(dir, "proactive", "api"); err != nil {
		t.Fatalf("the bot is there: %v", err)
	}
	if err := routineBotExists(dir, "proactive", "web"); err == nil || !strings.Contains(err.Error(), "is in api, not web") {
		t.Fatalf("another workspace: %v", err)
	}
	if err := routineBotExists(dir, "ghost", "api"); err == nil || !strings.Contains(err.Error(), "corgi agent bot add ghost --template ghost") {
		t.Fatalf("a missing bot says how to add it: %v", err)
	}
}
