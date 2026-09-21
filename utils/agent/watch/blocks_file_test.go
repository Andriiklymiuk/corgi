package watch

import (
	"testing"
	"time"
)

func TestABlockFromTheCLISurvivesTheDaemonsNextSave(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	daemon := LoadFixLog(dir)
	daemon.Start("acme", "k0", now.Add(-time.Hour))

	cli := LoadFixLog(dir)
	cli.Block("acme", "acme/api#454", "Andrii is on it", BlockedByPerson, now)

	daemon.Start("acme", "k1", now)
	if _, ok := daemon.Blocked("acme", "acme/api#454"); !ok {
		t.Fatal("the daemon sees a block another process wrote, without reloading")
	}
	if _, ok := LoadFixLog(dir).Blocked("acme", "acme/api#454"); !ok {
		t.Fatal("the daemon's save must not wipe the block")
	}

	if !cli.Unblock("acme", "acme/api#454") {
		t.Fatal("unblock from the cli")
	}
	daemon.Start("acme", "k2", now)
	if _, ok := daemon.Blocked("acme", "acme/api#454"); ok {
		t.Fatal("the daemon sees the unblock too")
	}
}

func TestLegacyBlocksInsideFixesJSONStillCount(t *testing.T) {
	dir := t.TempDir()
	l := LoadFixLog(dir)
	l.Blocks = map[string]Block{"acme/ABC-1": {Reason: "old", By: BlockedByPerson, At: time.Now()}}
	if err := l.save(); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadFixLog(dir).Blocked("acme", "ABC-1"); !ok {
		t.Fatal("a block written by an older corgi is honoured")
	}
}
