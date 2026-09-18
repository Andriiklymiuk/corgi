package daemon

import (
	"context"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils"
)

const (
	hotSpeedLimit = 60
	lowDiskGB     = 10
	gateCacheFor  = time.Minute
)

var speedLimitLine = regexp.MustCompile(`CPU_Speed_Limit\s*=\s*(\d+)`)

// A laptop closed in a bag throttles itself; a fix run on top of that is
// heat for nothing, so new runs wait until macOS lifts the limit.
var readThermal = func() (int, bool) {
	if runtime.GOOS != "darwin" {
		return 0, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "pmset", "-g", "therm").Output()
	if err != nil {
		return 0, false
	}
	m := speedLimitLine.FindStringSubmatch(string(out))
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	return n, err == nil
}

// Worktrees and images pile up over weeks with nobody pruning by hand.
var readFreeGB = func(dir string) (int, bool) {
	free, ok := utils.FreeDiskBytes(dir)
	return int(free / (1 << 30)), ok
}

var onHot, onLowDisk func()

// A gate holds new runs while a machine condition lasts, re-reads it at most
// once a minute, and tells the person once per stretch.
type gate struct {
	mu      sync.Mutex
	checked time.Time
	closed  bool
	told    bool
}

func (g *gate) reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.checked, g.closed, g.told = time.Time{}, false, false
}

func (g *gate) holds(now time.Time, read func() bool, tell func()) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.checked.IsZero() && now.Sub(g.checked) < gateCacheFor {
		return g.closed
	}
	g.checked = now
	g.closed = read()
	if !g.closed {
		g.told = false
		return false
	}
	if !g.told && tell != nil {
		g.told = true
		tell()
	}
	return true
}

var heat, disk gate

func resetHeat() { heat.reset() }

func resetDisk() { disk.reset() }

func tooHot(now time.Time) bool {
	return heat.holds(now, func() bool {
		limit, known := readThermal()
		return known && limit < hotSpeedLimit
	}, onHot)
}

func lowDisk(now time.Time, dir string) bool {
	return disk.holds(now, func() bool {
		free, known := readFreeGB(dir)
		return known && free < lowDiskGB
	}, onLowDisk)
}
