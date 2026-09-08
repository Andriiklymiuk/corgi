//go:build linux

package proc

import "testing"

func TestParseStatReadsNameWithSpacesPpidAndTTY(t *testing.T) {
	p, ok := parseStat(42, "42 (Code Helper (Plugin)) S 7 42 42 34816 42 0 0")
	if !ok || p.Name != "Code Helper (Plugin)" || p.PPID != 7 || p.TTY != 34816 {
		t.Fatalf("parseStat = %+v %v", p, ok)
	}
	if _, ok := parseStat(1, "garbage"); ok {
		t.Fatal("garbage")
	}
	if _, ok := parseStat(1, "1 (x) S"); ok {
		t.Fatal("too short")
	}
}
