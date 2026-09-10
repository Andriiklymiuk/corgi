package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils"
)

func TestComposeCacheServesClonesAndInvalidates(t *testing.T) {
	dir := chdirToTempCompose(t, mcpComposeFixture)
	mcpCache.reset()

	first, err := loadComposeForMCP("")
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	cwd, _ := os.Getwd()
	key := composeLookup{arg: "", cwd: cwd, tier: utils.EnvTierFromFlag}
	if _, ok := mcpCache.compose[key]; !ok {
		t.Fatal("first load did not populate the cache")
	}

	// A handler that trims the compose in place must not poison later calls.
	first.Services = nil
	first.DatabaseServices[0].ServiceName = "mutated"

	utils.CorgiComposePath = ""
	second, err := loadComposeForMCP("")
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if len(second.Services) != 1 || second.DatabaseServices[0].ServiceName != "pg" {
		t.Fatalf("cached copy leaked a handler's mutation: %+v", second)
	}
	want, _ := filepath.EvalSymlinks(filepath.Join(dir, "corgi-compose.yml"))
	if got, _ := filepath.EvalSymlinks(utils.CorgiComposePath); got != want {
		t.Errorf("cache hit must restore CorgiComposePath: got %q want %q", got, want)
	}
	if utils.CorgiComposeFileContent != second {
		t.Error("cache hit must point CorgiComposeFileContent at the returned copy")
	}

	// Editing the file (size changes) invalidates the entry.
	if err := os.WriteFile(filepath.Join(dir, "corgi-compose.yml"), []byte("name: edited\n"+mcpComposeFixture[len("name: mcp-fixture\n"):]), 0o644); err != nil {
		t.Fatal(err)
	}
	third, err := loadComposeForMCP("")
	if err != nil {
		t.Fatalf("third load: %v", err)
	}
	if third.Name != "edited" {
		t.Errorf("edited compose served stale: name %q", third.Name)
	}
}

func TestComposeCacheKeyedByPath(t *testing.T) {
	mcpCache.reset()
	other := t.TempDir()
	otherPath := filepath.Join(other, "corgi-compose.yml")
	if err := os.WriteFile(otherPath, []byte("name: other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	chdirToTempCompose(t, "name: here\n")

	if c, err := loadComposeForMCP(otherPath); err != nil || c.Name != "other" {
		t.Fatalf("explicit path: %+v %v", c, err)
	}
	if c, err := loadComposeForMCP(""); err != nil || c.Name != "here" {
		t.Fatalf("cwd path after explicit: %+v %v", c, err)
	}
	if c, err := loadComposeForMCP(otherPath); err != nil || c.Name != "other" || filepath.Base(utils.CorgiComposePathDir) != filepath.Base(other) {
		t.Fatalf("explicit path again (cache hit): %+v %v dir=%q", c, err, utils.CorgiComposePathDir)
	}
}

func TestCloneComposeIsDeep(t *testing.T) {
	yes := true
	orig := &utils.CorgiCompose{
		Name: "x",
		Services: []utils.Service{{
			ServiceName:      "api",
			Start:            []string{"go run ."},
			WaitForDatabases: &yes,
			Scripts:          []utils.Script{{Name: "test", Commands: []string{"true"}}},
		}},
		DatabaseServices: []utils.DatabaseService{{ServiceName: "pg", Driver: "postgres"}},
		EnvTiers:         map[string]utils.EnvTier{"staging": {Dir: "env/staging"}},
		E2E:              &utils.E2ESuite{Run: "make e2e"},
	}
	clone := cloneCompose(orig)
	if !reflect.DeepEqual(orig, clone) {
		t.Fatalf("clone differs:\n%+v\n%+v", orig, clone)
	}

	clone.Services[0].ServiceName = "changed"
	clone.Services[0].Start[0] = "changed"
	clone.Services[0].Scripts[0].Commands[0] = "false"
	*clone.Services[0].WaitForDatabases = false
	clone.EnvTiers["prod"] = utils.EnvTier{}
	clone.E2E.Run = "changed"
	clone.Services = clone.Services[:0]

	if orig.Services[0].ServiceName != "api" || orig.Services[0].Start[0] != "go run ." ||
		orig.Services[0].Scripts[0].Commands[0] != "true" || !*orig.Services[0].WaitForDatabases ||
		len(orig.EnvTiers) != 1 || orig.E2E.Run != "make e2e" || len(orig.Services) != 1 {
		t.Errorf("mutating the clone reached the original: %+v", orig)
	}
	if cloneCompose(nil) != nil {
		t.Error("nil in, nil out")
	}
}

func TestStatusCacheTTL(t *testing.T) {
	c := newMCPCacheStore()
	entries := []statusEntry{{Label: "services.api", Port: 1, Healthy: true}}
	c.storeStatus("/p", entries)

	got, ok := c.cachedStatus("/p")
	if !ok || len(got) != 1 {
		t.Fatalf("fresh entry missed: %v %v", got, ok)
	}
	got[0].Healthy = false
	if again, _ := c.cachedStatus("/p"); !again[0].Healthy {
		t.Error("cached slice must be copied out, not shared")
	}
	if _, ok := c.cachedStatus("/other"); ok {
		t.Error("unknown path must miss")
	}

	c.mu.Lock()
	c.status["/p"] = statusCacheEntry{at: time.Now().Add(-2 * mcpStatusCacheTTL), entries: entries}
	c.mu.Unlock()
	if _, ok := c.cachedStatus("/p"); ok {
		t.Error("expired entry must miss")
	}

	c.storeStatus("/p", entries)
	c.invalidateStatus()
	if _, ok := c.cachedStatus("/p"); ok {
		t.Error("invalidated entry must miss")
	}
}
