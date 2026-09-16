package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"andriiklymiuk/corgi/utils/agent/proc"
)

func readRecordPID(t *testing.T, dir string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "daemon.json"))
	if err != nil {
		t.Fatalf("daemon.json: %v", err)
	}
	var info Info
	if err := jsonUnmarshal(data, &info); err != nil {
		t.Fatal(err)
	}
	return info.PID
}

// Two daemons once overlapped on a machine; the one that exited removed the
// record the other had written, and from then on nothing could find the one
// still running. A daemon takes only its own record with it.
func TestCleanupLeavesAnotherDaemonsRecord(t *testing.T) {
	d := New("test", t.TempDir())
	other := os.Getpid() + 100000
	if err := writeJSONAtomic(d.InfoPath(), Info{PID: other}); err != nil {
		t.Fatal(err)
	}

	d.cleanup()

	if got := readRecordPID(t, d.Dir); got != other {
		t.Errorf("record pid = %d, want the other daemon's %d kept", got, other)
	}
}

func TestCleanupRemovesOwnRecord(t *testing.T) {
	d := New("test", t.TempDir())
	if err := d.writeInfoIDs(nil); err != nil {
		t.Fatal(err)
	}

	d.cleanup()

	if _, err := os.Stat(d.InfoPath()); !os.IsNotExist(err) {
		t.Error("own record should be gone")
	}
}

func TestReassertInfoRestoresAMissingRecord(t *testing.T) {
	d := New("test", t.TempDir())

	d.reassertInfo()

	if got := readRecordPID(t, d.Dir); got != os.Getpid() {
		t.Errorf("record pid = %d, want %d", got, os.Getpid())
	}
}

func TestReassertInfoReplacesADeadDaemonsRecord(t *testing.T) {
	d := New("test", t.TempDir())
	if err := writeJSONAtomic(d.InfoPath(), Info{PID: 0}); err != nil {
		t.Fatal(err)
	}

	d.reassertInfo()

	if got := readRecordPID(t, d.Dir); got != os.Getpid() {
		t.Errorf("record pid = %d, want %d", got, os.Getpid())
	}
}

func TestReassertInfoLeavesALiveDaemonsRecord(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pid 1 is a unix fixture")
	}
	d := New("test", t.TempDir())
	if err := writeJSONAtomic(d.InfoPath(), Info{PID: 1}); err != nil {
		t.Fatal(err)
	}

	d.reassertInfo()

	if got := readRecordPID(t, d.Dir); got != 1 {
		t.Errorf("record pid = %d, want the live daemon's record left alone", got)
	}
}

// kill(pid, 0) answers EPERM for a process that exists but is not ours. That
// is "alive", never "gone": reading it as gone deletes a live daemon's record.
func TestProcessAliveTreatsPermissionDeniedAsAlive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pid 1 is a unix fixture")
	}
	if !processAlive(1) {
		t.Error("pid 1 exists on every unix; a permission error must not read as dead")
	}
}

func TestIsServeProcess(t *testing.T) {
	names := map[string]bool{"corgi": true, "corgi-dev": true}
	self := 4242
	cases := []struct {
		name string
		p    proc.Process
		want bool
	}{
		{"the daemon", proc.Process{PID: 1, Name: "corgi", Args: "/opt/homebrew/bin/corgi agent serve"}, true},
		{"foreground daemon", proc.Process{PID: 2, Name: "corgi", Args: "corgi agent serve --foreground"}, true},
		{"a dev build", proc.Process{PID: 3, Name: "corgi-dev", Args: "/tmp/corgi-dev agent serve"}, true},
		{"another corgi command", proc.Process{PID: 4, Name: "corgi", Args: "/opt/homebrew/bin/corgi agent status"}, false},
		{"a neighbour binary", proc.Process{PID: 5, Name: "corgit", Args: "/usr/bin/corgit agent serve"}, false},
		{"this process", proc.Process{PID: self, Name: "corgi", Args: "corgi agent serve"}, false},
		{"no command line to judge by", proc.Process{PID: 6, Name: "corgi"}, false},
		{"a shell mentioning it", proc.Process{PID: 7, Name: "zsh", Args: "zsh -c corgi agent serve"}, false},
	}
	for _, c := range cases {
		if got := isServeProcess(c.p, self, names); got != c.want {
			t.Errorf("%s: isServeProcess = %v, want %v", c.name, got, c.want)
		}
	}
}

// A read of the process table; never a signal. Whatever else runs on the
// machine, the caller is not a stray of itself.
func TestOtherServersLeavesOutSelf(t *testing.T) {
	for _, pid := range OtherServers(os.Getpid()) {
		if pid == os.Getpid() {
			t.Error("self listed as another server")
		}
	}
	if !serveNames()["corgi"] {
		t.Error("the product name must always count")
	}
}
