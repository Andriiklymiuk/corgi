// Package proc answers the few questions corgi's session tracking asks about
// other processes: is this pid alive, who is its parent, what is it called,
// and what is its working directory. Each answer is a syscall or a /proc read
// where the platform allows, because the hook that asks them runs on every
// tool call and must cost nothing.
package proc

import (
	"path/filepath"
	"strings"
)

// Process is one entry in a parent chain or a process listing.
type Process struct {
	PID  int    `json:"pid"`
	PPID int    `json:"ppid"`
	Name string `json:"name"`
	// Args is the full command line where the platform hands it out cheaply
	// (Linux /proc). Empty on macOS, whose sysctl carries only the name.
	Args string `json:"args,omitempty"`
}

// MaxAncestors bounds a parent walk. A chain deeper than this is not a
// terminal session; it is a bug in the walk.
const MaxAncestors = 32

// Lookup is the platform's parent-and-name probe. A variable so tests can
// substitute a fake process table.
var Lookup = lookup

// Ancestors walks parents from pid (inclusive) up to init, oldest last. It
// stops at the first pid the platform cannot describe, so on a platform with
// no probe it returns nothing rather than guessing.
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

// PIDs flattens a chain to its pids, in the same order.
func PIDs(chain []Process) []int {
	out := make([]int, 0, len(chain))
	for _, p := range chain {
		out = append(out, p.PID)
	}
	return out
}

// shells are the interpreters a hook runs under before reaching the process
// that spawned it. Claude Code runs command hooks through a shell, and the
// integrated terminal runs claude under another one, so "the first ancestor
// that is not a shell" is the claude process.
var shells = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "dash": true, "fish": true,
	"ksh": true, "tcsh": true, "csh": true, "nu": true, "xonsh": true,
}

// IsShell reports whether a process name is a shell, tolerant of the login
// dash and a full path.
func IsShell(name string) bool {
	name = filepath.Base(strings.TrimPrefix(name, "-"))
	return shells[name]
}

// Owner picks the process a hook belongs to out of its parent chain: the
// nearest ancestor that is not a shell and not corgi itself. The chain starts
// at the hook's parent, so a hook run directly by claude finds claude first,
// and one run through `sh -c` skips the sh.
//
// Claude Code is either a native binary called claude or a node script, and
// the name check prefers those when they are in the chain: a hook launched
// through some wrapper still lands on the session rather than the wrapper.
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
	return base == "claude" || base == "node" || base == "bun"
}

// LooksLikeClaude reports whether a process listing entry is a Claude Code
// session: the native binary, or a runtime whose command line names the CLI.
// Used by rescan, which sees processes corgi got no hook for.
func LooksLikeClaude(p Process) bool {
	base := filepath.Base(p.Name)
	if base == "claude" {
		return true
	}
	if base != "node" && base != "bun" {
		return false
	}
	args := strings.ToLower(p.Args)
	return strings.Contains(args, "claude-code") || strings.Contains(args, "/claude") || strings.HasSuffix(args, " claude")
}
