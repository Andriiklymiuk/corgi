package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"andriiklymiuk/corgi/utils/agent/proc"
)

// The daemon's record, daemon.json, is how every other corgi process finds
// it. Two daemons once overlapped after an upgrade: the one that exited took
// the record with it, the survivor ran on unfindable, and the next `agent up`
// started a third. So a daemon removes only a record that names itself, puts
// its own back when it goes missing, and the CLI can see a daemon that has no
// record at all.

func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }

// recordPID is the pid daemon.json names, 0 when there is no readable record.
func recordPID(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var info Info
	if json.Unmarshal(data, &info) != nil {
		return 0
	}
	return info.PID
}

// removeOwnInfo deletes daemon.json only while it still names this process.
func (d *Daemon) removeOwnInfo() {
	if pid := recordPID(d.InfoPath()); pid == 0 || pid == os.Getpid() {
		_ = os.Remove(d.InfoPath())
	}
}

// reassertInfo puts this daemon's record back when it is missing or names a
// process that is gone. A record naming another live process is left alone:
// that is a second daemon, and `agent stop` is the one to sort that out.
func (d *Daemon) reassertInfo() {
	pid := recordPID(d.InfoPath())
	if pid == os.Getpid() || (pid > 0 && processAlive(pid)) {
		return
	}
	_ = d.writeInfoIDs(d.runnerIDs())
}

// OtherServers lists the pids of every `corgi agent serve` process on the
// machine other than self, record or no record. Empty where the platform
// cannot list processes.
func OtherServers(self int) []int {
	list, err := proc.List()
	if err != nil {
		return nil
	}
	names := serveNames()
	var pids []int
	for _, p := range list {
		if isServeProcess(p, self, names) {
			pids = append(pids, p.PID)
		}
	}
	return pids
}

// serveNames is what a corgi binary is called: the product name, and this
// process's own name for a differently-named build.
func serveNames() map[string]bool {
	names := map[string]bool{"corgi": true}
	if exe, err := os.Executable(); err == nil {
		names[filepath.Base(exe)] = true
	}
	return names
}

// isServeProcess is an exact match on the binary name and on the words
// `agent serve` as the command's first arguments — never a shell that
// mentions them, never a neighbour binary that embeds the name.
func isServeProcess(p proc.Process, self int, names map[string]bool) bool {
	if p.PID <= 0 || p.PID == self || !names[filepath.Base(p.Name)] {
		return false
	}
	f := strings.Fields(p.Args)
	return len(f) >= 3 && names[filepath.Base(f[0])] && f[1] == "agent" && f[2] == "serve"
}
