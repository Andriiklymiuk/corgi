package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"andriiklymiuk/corgi/utils/agent/proc"
)

func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }

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

func (d *Daemon) removeOwnInfo() {
	if pid := recordPID(d.InfoPath()); pid == 0 || pid == os.Getpid() {
		_ = os.Remove(d.InfoPath())
	}
}

func (d *Daemon) reassertInfo() {
	pid := recordPID(d.InfoPath())
	if pid == os.Getpid() || (pid > 0 && processAlive(pid)) {
		return
	}
	_ = d.writeInfoIDs(d.runnerIDs())
}

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

func serveNames() map[string]bool {
	names := map[string]bool{"corgi": true}
	if exe, err := os.Executable(); err == nil {
		names[filepath.Base(exe)] = true
	}
	return names
}

func isServeProcess(p proc.Process, self int, names map[string]bool) bool {
	if p.PID <= 0 || p.PID == self || !names[filepath.Base(p.Name)] {
		return false
	}
	f := strings.Fields(p.Args)
	return len(f) >= 3 && names[filepath.Base(f[0])] && f[1] == "agent" && f[2] == "serve"
}
