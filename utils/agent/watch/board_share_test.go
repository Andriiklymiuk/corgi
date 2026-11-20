package watch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBoardShareOpensOnlyWithItsKeyAndUntilItExpires(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	share, key, err := LoadBoardShares(dir).Add("api", "for Anna", time.Hour, false, now)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := LoadBoardShares(dir).Find(key, now); !ok || got.Workspace != "api" || got.ID != share.ID {
		t.Fatalf("find = %+v %v", got, ok)
	}
	wrong := key[:47] + string("0123456789abcdef"[(strings.IndexByte("0123456789abcdef", key[47])+1)%16])
	for _, bad := range []string{"", "short", wrong, strings.Repeat("0", 48)} {
		if _, ok := LoadBoardShares(dir).Find(bad, now); ok {
			t.Fatalf("%q opened the board", bad)
		}
	}
	if _, ok := LoadBoardShares(dir).Find(key, now.Add(2*time.Hour)); ok {
		t.Fatal("an expired link opened the board")
	}
}

func TestBoardShareKeyIsNotKeptOnDisk(t *testing.T) {
	dir := t.TempDir()
	_, key, err := LoadBoardShares(dir).Add("", "", 0, false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "watch", "board_shares.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), key[8:]) {
		t.Fatal("the key is on disk in the clear")
	}
	if info, _ := os.Stat(filepath.Join(dir, "watch", "board_shares.json")); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
}

func TestBoardShareCloses(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	log := LoadBoardShares(dir)
	first, firstKey, _ := log.Add("api", "", 0, false, now)
	_, secondKey, _ := log.Add("web", "", 0, false, now)
	if n, err := log.Close(first.ID); err != nil || n != 1 {
		t.Fatalf("close = %d %v", n, err)
	}
	if _, ok := LoadBoardShares(dir).Find(firstKey, now); ok {
		t.Fatal("a closed link opened the board")
	}
	if _, ok := LoadBoardShares(dir).Find(secondKey, now); !ok {
		t.Fatal("closing one link closed another")
	}
	if _, err := log.Close("nope"); err == nil {
		t.Fatal("closing a link that is not there said nothing")
	}
	if n, _ := log.Close("all"); n != 1 || len(LoadBoardShares(dir).List(now)) != 0 {
		t.Fatalf("close all left %d", len(LoadBoardShares(dir).List(now)))
	}
}

func TestBoardSharesAreCapped(t *testing.T) {
	log := LoadBoardShares(t.TempDir())
	now := time.Now()
	for i := 0; i < maxBoardShares; i++ {
		if _, _, err := log.Add("", "", 0, false, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := log.Add("", "", 0, false, now); err == nil {
		t.Fatal("one link too many was made")
	}
}
