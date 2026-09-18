package utils

import "testing"

func TestDiskHeadroomScalesWithTheStack(t *testing.T) {
	small, _, _, _ := DiskHeadroom(&CorgiCompose{}, t.TempDir())
	big, _, _, _ := DiskHeadroom(&CorgiCompose{
		DatabaseServices: []DatabaseService{{ServiceName: "a"}, {ServiceName: "b"}},
		Services:         []Service{{ServiceName: "api"}, {ServiceName: "web"}},
	}, t.TempDir())

	if big <= small {
		t.Errorf("a bigger stack must need more disk: %d vs %d", big, small)
	}
}

func TestDiskHeadroomPassesOnAHostWithSpace(t *testing.T) {
	_, free, ok, known := DiskHeadroom(&CorgiCompose{}, t.TempDir())
	if !known {
		t.Skip("platform cannot report free disk")
	}
	if free == 0 {
		t.Fatal("expected a non-zero free figure")
	}
	if !ok {
		t.Skip("this machine genuinely has less than the base estimate free")
	}
}

func TestDiskHeadroomTreatsUnknownAsFine(t *testing.T) {
	_, _, ok, known := DiskHeadroom(&CorgiCompose{}, "/definitely/not/a/path/corgi")
	if known {
		t.Skip("this platform answered for a missing path")
	}
	if !ok {
		t.Error("an unanswerable check must not fail the run")
	}
}

func TestFormatGigabytes(t *testing.T) {
	if got := FormatGigabytes(2 << 30); got != "2.0G" {
		t.Errorf("got %q", got)
	}
}
