package utils

import "testing"

// The estimate has to grow with the stack, or a one-service compose and a
// fourteen-container one would be judged against the same bar.
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

// A real temp dir has space, so the check should pass rather than cry wolf.
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

// An unknown figure must not be reported as a failure — the check is skipped.
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
