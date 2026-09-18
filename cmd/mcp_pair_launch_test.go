package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/pairing"
)

func TestPairLaunchIsReadOnceAndOpensThatCode(t *testing.T) {
	dir := t.TempDir()
	code, _ := pairing.NewCode()
	if err := writePairLaunch(dir, code, 2*time.Hour); err != nil {
		t.Fatal(err)
	}
	session, ok := takePairLaunch(dir)
	if !ok || session.Code() != code {
		t.Fatalf("launch not taken: ok=%v code=%q", ok, session.Code())
	}
	if left := time.Until(session.ExpiresAt()); left < 119*time.Minute {
		t.Fatalf("ttl not honoured: %s", left)
	}
	if _, err := os.Stat(filepath.Join(dir, pairLaunchName)); !os.IsNotExist(err) {
		t.Fatal("launch file should be gone after one read")
	}
	if _, ok := takePairLaunch(dir); ok {
		t.Fatal("a second read must not reopen the window")
	}
}

func TestPairLaunchRejectsABadCodeBeforeWriting(t *testing.T) {
	dir := t.TempDir()
	if err := writePairLaunch(dir, "nope", 0); err == nil {
		t.Fatal("short code accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, pairLaunchName)); !os.IsNotExist(err) {
		t.Fatal("nothing should be written for a bad code")
	}
}
