package sessions

import (
	"reflect"
	"testing"
)

func TestBoardPlacesLowestFreeAndNeverResorts(t *testing.T) {
	b := NewBoard(3)
	b.Place("a")
	b.Place("b")
	b.Place("c")
	b.Place("d") // overflow
	if !reflect.DeepEqual(b.Slots, []string{"a", "b", "c"}) || !reflect.DeepEqual(b.Overflow, []string{"d"}) {
		t.Fatalf("board = %v overflow %v", b.Slots, b.Overflow)
	}
	// The middle one ends: its slot frees and the oldest overflow takes it;
	// a and c do not move.
	b.Remove("b")
	if !reflect.DeepEqual(b.Slots, []string{"a", "d", "c"}) || len(b.Overflow) != 0 {
		t.Fatalf("after remove: board = %v overflow %v", b.Slots, b.Overflow)
	}
	b.Remove("a")
	b.Place("e")
	if !reflect.DeepEqual(b.Slots, []string{"e", "d", "c"}) {
		t.Fatalf("new session takes the lowest free index, got %v", b.Slots)
	}
	b.Place("e") // idempotent
	if b.Has("zzz") || !b.Has("e") {
		t.Fatal("Has is wrong")
	}
}

func TestBoardPinnedSlotKeepsItsSession(t *testing.T) {
	b := NewBoard(2)
	b.Place("a")
	b.Place("b")
	b.Place("c")
	if !b.Pin(0, true) || !b.IsPinned("a") {
		t.Fatal("pin a")
	}
	if kept := b.Remove("a"); !kept || b.Slots[0] != "a" {
		t.Fatalf("a pinned slot keeps its id, kept=%v slots=%v", kept, b.Slots)
	}
	// Unpin frees it and overflow c moves in.
	b.Pin(0, false)
	b.Free(0)
	if !reflect.DeepEqual(b.Slots, []string{"c", "b"}) || len(b.Overflow) != 0 {
		t.Fatalf("after unpin: %v %v", b.Slots, b.Overflow)
	}
	if b.Pin(5, true) || b.Pin(-1, true) {
		t.Fatal("off-board indexes cannot be pinned")
	}
	b.Remove("c")
	if b.Pin(0, true) {
		t.Fatal("an empty slot cannot be pinned")
	}
}

func TestBoardPagerAndPaging(t *testing.T) {
	b := NewBoard(3)
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		b.Place(id)
	}
	if b.PagerIndex() != 2 || b.Hidden() != 3 {
		t.Fatalf("pager should be the last unpinned slot standing for 3, got %d/%d", b.PagerIndex(), b.Hidden())
	}
	// Two visible (a, b), c hidden behind the pager, d and e in overflow.
	// Next page shows c and d; e and then a, b wait.
	if !b.Page(1) {
		t.Fatal("page should rotate")
	}
	if !reflect.DeepEqual(b.Slots, []string{"c", "d", "e"}) || !reflect.DeepEqual(b.Overflow, []string{"a", "b"}) {
		t.Fatalf("page 1: %v %v", b.Slots, b.Overflow)
	}
	if !b.Page(-1) || !reflect.DeepEqual(b.Slots, []string{"a", "b", "c"}) {
		t.Fatalf("page back: %v %v", b.Slots, b.Overflow)
	}
	// A pinned slot stays put while the others rotate around it.
	b.Pin(0, true)
	b.Page(1)
	if b.Slots[0] != "a" {
		t.Fatalf("pinned a must not move, got %v", b.Slots)
	}
	if b.PagerIndex() != 2 {
		t.Fatalf("pager = %d", b.PagerIndex())
	}
	// Everything pinned: no pager, but the overflow is still counted.
	b.Pin(1, true)
	b.Pin(2, true)
	if b.PagerIndex() != -1 || b.Page(1) {
		t.Fatal("no unpinned slot means no pager and no paging")
	}
	if b.Hidden() != len(b.Overflow) {
		t.Fatal("hidden without a pager is just the overflow")
	}
	empty := NewBoard(2)
	if empty.Page(1) || empty.PagerIndex() != -1 || empty.Hidden() != 0 {
		t.Fatal("an empty board does not page")
	}
}

func TestBoardResizeSpillsAndRefills(t *testing.T) {
	b := NewBoard(4)
	for _, id := range []string{"a", "b", "c", "d"} {
		b.Place(id)
	}
	b.Resize(2)
	if !reflect.DeepEqual(b.Slots, []string{"a", "b"}) || !reflect.DeepEqual(b.Overflow, []string{"c", "d"}) {
		t.Fatalf("shrink: %v %v", b.Slots, b.Overflow)
	}
	b.Resize(3)
	if !reflect.DeepEqual(b.Slots, []string{"a", "b", "c"}) || !reflect.DeepEqual(b.Overflow, []string{"d"}) {
		t.Fatalf("grow refills from overflow: %v %v", b.Slots, b.Overflow)
	}
	b.Resize(3)
	b.Resize(0)
	if b.Size != 3 {
		t.Fatal("no-op resizes")
	}
	if NewBoard(0).Size != DefaultSize {
		t.Fatal("zero means the default size")
	}
}

func TestBoardRename(t *testing.T) {
	b := NewBoard(1)
	b.Place("pid:1")
	b.Place("pid:2")
	b.Rename("pid:1", "s1")
	b.Rename("pid:2", "s2")
	if b.Slots[0] != "s1" || b.Overflow[0] != "s2" {
		t.Fatalf("rename: %v %v", b.Slots, b.Overflow)
	}
	b.Free(9)
	b.Remove("nobody")
}
