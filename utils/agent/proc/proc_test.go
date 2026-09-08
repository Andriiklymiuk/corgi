package proc

import (
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func fakeTable(t *testing.T, table map[int]Process) {
	t.Helper()
	orig := Lookup
	Lookup = func(pid int) (Process, bool) {
		p, ok := table[pid]
		return p, ok
	}
	t.Cleanup(func() { Lookup = orig })
}

func TestAncestorsWalksToInitAndStopsOnUnknown(t *testing.T) {
	fakeTable(t, map[int]Process{
		50: {PID: 50, PPID: 40, Name: "sh"},
		40: {PID: 40, PPID: 30, Name: "claude"},
		30: {PID: 30, PPID: 20, Name: "zsh"},
		20: {PID: 20, PPID: 1, Name: "Code Helper"},
	})
	got := PIDs(Ancestors(50))
	if want := []int{50, 40, 30, 20}; !reflect.DeepEqual(got, want) {
		t.Fatalf("chain = %v, want %v", got, want)
	}
	if got := Ancestors(99); len(got) != 0 {
		t.Fatalf("an unknown pid must yield an empty chain, got %v", got)
	}
}

func TestAncestorsBreaksCycles(t *testing.T) {
	fakeTable(t, map[int]Process{
		5: {PID: 5, PPID: 6, Name: "a"},
		6: {PID: 6, PPID: 5, Name: "b"},
	})
	if got := Ancestors(5); len(got) != 2 {
		t.Fatalf("a pid cycle must terminate, got %d entries", len(got))
	}
}

func TestOwnerPrefersClaudeThenFirstNonShell(t *testing.T) {
	chain := []Process{{PID: 3, Name: "sh"}, {PID: 2, Name: "zsh"}, {PID: 1, Name: "claude"}}
	if p, ok := Owner(chain); !ok || p.PID != 1 {
		t.Fatalf("claude must win even behind two shells, got %+v %v", p, ok)
	}
	chain = []Process{{PID: 3, Name: "-bash"}, {PID: 2, Name: "corgi"}, {PID: 1, Name: "python"}}
	if p, ok := Owner(chain); !ok || p.PID != 1 {
		t.Fatalf("shells and corgi are skipped, got %+v %v", p, ok)
	}
	if _, ok := Owner([]Process{{Name: "sh"}}); ok {
		t.Fatal("a chain of only shells has no owner")
	}
	if _, ok := Owner(nil); ok {
		t.Fatal("an empty chain has no owner")
	}
}

func TestIsShell(t *testing.T) {
	for _, name := range []string{"sh", "-zsh", "/bin/bash", "fish"} {
		if !IsShell(name) {
			t.Errorf("%q should be a shell", name)
		}
	}
	for _, name := range []string{"claude", "node", "Code Helper"} {
		if IsShell(name) {
			t.Errorf("%q should not be a shell", name)
		}
	}
}

func TestLooksLikeClaude(t *testing.T) {
	yes := []Process{
		{Name: "claude"},
		{Name: "node", Args: "/usr/local/bin/node /usr/local/lib/node_modules/@anthropic-ai/claude-code/cli.js"},
		{Name: "bun", Args: "bun /home/me/.bun/bin/claude"},
	}
	for _, p := range yes {
		if !LooksLikeClaude(p) {
			t.Errorf("%+v should look like claude", p)
		}
	}
	no := []Process{
		{Name: "node", Args: "node server.js"},
		{Name: "zsh"},
		{Name: "Code Helper"},
		{Name: "node", Args: "/opt/homebrew/bin/node /Users/me/.npm/_npx/e5b3/node_modules/claude-stats-hook/index.js"},
		{Name: "node", Args: "node /home/me/claude/notes/server.js"},
	}
	for _, p := range no {
		if LooksLikeClaude(p) {
			t.Errorf("%+v should not look like claude", p)
		}
	}
}

func TestRealLookupSeesSelf(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("no process probe on this platform")
	}
	p, ok := Lookup(os.Getpid())
	if !ok || p.PID != os.Getpid() || p.PPID != os.Getppid() {
		t.Fatalf("lookup(self) = %+v %v, want ppid %d", p, ok, os.Getppid())
	}
	if !Alive(os.Getpid()) {
		t.Fatal("the test process is alive")
	}
	if Alive(0) || Alive(-1) {
		t.Fatal("pid 0 and negatives are never alive")
	}
	chain := Ancestors(os.Getpid())
	if len(chain) == 0 || chain[0].PID != os.Getpid() {
		t.Fatalf("chain should start at self, got %v", chain)
	}
	if cwd := Cwd(os.Getpid()); cwd == "" {
		t.Fatal("cwd of self should resolve")
	}
	list, err := List()
	if err != nil || len(list) == 0 {
		t.Fatalf("List() = %d, %v", len(list), err)
	}
}

func TestNamesAndHasCorgi(t *testing.T) {
	chain := []Process{{PID: 3, Name: "sh"}, {PID: 2, Name: "claude"}, {PID: 1, Name: "/opt/homebrew/bin/corgi"}}
	if got := Names(chain); len(got) != 3 || got[1] != "claude" {
		t.Fatalf("Names = %v", got)
	}
	if !HasCorgi(chain) || HasCorgi(chain[:2]) || HasCorgi(nil) {
		t.Fatal("HasCorgi")
	}
}

func TestTTYNameOfSelfOrNone(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip()
	}
	if TTYName(0) != "" {
		t.Fatal("0 is no terminal")
	}
	p, _ := Lookup(os.Getpid())
	if p.TTY == 0 {
		return // no controlling terminal under the test runner
	}
	if name := TTYName(p.TTY); !strings.HasPrefix(name, "/dev/") {
		t.Fatalf("tty %d resolved to %q", p.TTY, name)
	}
}
