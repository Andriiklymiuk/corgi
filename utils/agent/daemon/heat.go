package daemon

import (
	"context"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"sync"
	"time"
)

const (
	hotSpeedLimit = 60
	heatCacheFor  = time.Minute
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

var onHot func()

var heat struct {
	mu      sync.Mutex
	checked time.Time
	hot     bool
	told    bool
}

func resetHeat() {
	heat.mu.Lock()
	defer heat.mu.Unlock()
	heat.checked, heat.hot, heat.told = time.Time{}, false, false
}

func tooHot(now time.Time) bool {
	heat.mu.Lock()
	defer heat.mu.Unlock()
	if !heat.checked.IsZero() && now.Sub(heat.checked) < heatCacheFor {
		return heat.hot
	}
	heat.checked = now
	limit, known := readThermal()
	heat.hot = known && limit < hotSpeedLimit
	if !heat.hot {
		heat.told = false
		return false
	}
	if !heat.told && onHot != nil {
		heat.told = true
		onHot()
	}
	return true
}
