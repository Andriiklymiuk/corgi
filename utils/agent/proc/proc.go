package proc

import (
	"path/filepath"
	"strings"
)

type Process struct {
	PID  int    `json:"pid"`
	PPID int    `json:"ppid"`
	Name string `json:"name"`
	Args string `json:"args,omitempty"`
	TTY  uint64 `json:"tty,omitempty"`
}

const MaxAncestors = 32

var Lookup = lookup

func Ancestors(pid int) []Process {
	var chain []Process
	seen := map[int]bool{}
	for pid > 1 && len(chain) < MaxAncestors && !seen[pid] {
		seen[pid] = true
		p, ok := Lookup(pid)
		if !ok {
			break
		}
		chain = append(chain, p)
		pid = p.PPID
	}
	return chain
}

func PIDs(chain []Process) []int {
	out := make([]int, 0, len(chain))
	for _, p := range chain {
		out = append(out, p.PID)
	}
	return out
}

func Names(chain []Process) []string {
	out := make([]string, 0, len(chain))
	for _, p := range chain {
		out = append(out, p.Name)
	}
	return out
}

func HasCorgi(chain []Process) bool {
	for _, p := range chain {
		if filepath.Base(p.Name) == "corgi" {
			return true
		}
	}
	return false
}

var shells = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "dash": true, "fish": true,
	"ksh": true, "tcsh": true, "csh": true, "nu": true, "xonsh": true,
}

func IsShell(name string) bool {
	name = filepath.Base(strings.TrimPrefix(name, "-"))
	return shells[name]
}

func Owner(chain []Process) (Process, bool) {
	for _, p := range chain {
		if isClaude(p.Name) {
			return p, true
		}
	}
	for _, p := range chain {
		if IsShell(p.Name) || filepath.Base(p.Name) == "corgi" {
			continue
		}
		return p, true
	}
	return Process{}, false
}

func isClaude(name string) bool {
	base := filepath.Base(name)
	return base == "claude" || base == "codex" || base == "node" || base == "bun"
}

// HarnessOf names the coding agent a process is - "claude", "codex" - or
// "" when it is neither.
func HarnessOf(p Process) string {
	base := filepath.Base(p.Name)
	if base == "codex" {
		return "codex"
	}
	if base == "node" || base == "bun" {
		for _, arg := range strings.Fields(strings.ToLower(p.Args)) {
			if filepath.Base(arg) == "codex" {
				return "codex"
			}
		}
	}
	if LooksLikeClaude(p) {
		return "claude"
	}
	return ""
}

func LooksLikeClaude(p Process) bool {
	base := filepath.Base(p.Name)
	if base == "claude" || base == "codex" {
		return true
	}
	if base != "node" && base != "bun" {
		return false
	}
	for _, arg := range strings.Fields(strings.ToLower(p.Args)) {
		if b := filepath.Base(arg); b == "claude" || b == "codex" {
			return true
		}
		for _, dir := range strings.Split(filepath.Dir(arg), "/") {
			if dir == "claude-code" {
				return true
			}
		}
	}
	return false
}
