package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/proc"
	"andriiklymiuk/corgi/utils/agent/push"
	"andriiklymiuk/corgi/utils/agent/supervisor"
	"andriiklymiuk/corgi/utils/atomicfile"
)

// A laptop closed in a bag on a hotspot cooks its battery. The machine watch
// reads the battery and the thermal state once a minute, holds new runs while
// it is warm, pauses the sessions while it is hot, and says so once per
// stretch. Apple stops charging past 45°C; 40 is where a bag gets to.
const (
	warmC         = 40.0
	hotC          = 45.0
	coolC         = 38.0
	criticalC     = 50.0
	hotSpeedLimit = 60
	hotSpeedPause = 30
	lowBatteryPct = 20
	flatBattery   = 10
	machineTick   = time.Minute
)

type Machine struct {
	Battery    int     `json:"battery"`
	Charging   bool    `json:"charging"`
	TempC      float64 `json:"tempC,omitempty"`
	SpeedLimit int     `json:"speedLimit,omitempty"`
	Known      bool    `json:"-"`
}

// Laptop is what the status file says about the machine, for the phone.
type Laptop struct {
	Machine
	Heat    string `json:"heat,omitempty"`
	Lid     string `json:"lid,omitempty"`
	Network string `json:"network,omitempty"`
	Hotspot string `json:"hotspot,omitempty"`
	Paused  int    `json:"paused,omitempty"`
}

var batteryLine = regexp.MustCompile(`"(CurrentCapacity|MaxCapacity|Temperature|ExternalConnected)"\s*=\s*(\S+)`)

func parseBattery(out string) (pct int, onAC bool, tempC float64, ok bool) {
	current, max := -1, -1
	for _, m := range batteryLine.FindAllStringSubmatch(out, -1) {
		switch m[1] {
		case "CurrentCapacity":
			current, _ = strconv.Atoi(m[2])
		case "MaxCapacity":
			max, _ = strconv.Atoi(m[2])
		case "Temperature":
			if t, err := strconv.Atoi(m[2]); err == nil {
				tempC = float64(t) / 100
			}
		case "ExternalConnected":
			onAC = m[2] == "Yes"
		}
	}
	if current < 0 || max <= 0 {
		return -1, onAC, tempC, false
	}
	return current * 100 / max, onAC, tempC, true
}

var readBattery = func() (pct int, onAC bool, tempC float64, ok bool) {
	if runtime.GOOS != "darwin" {
		return -1, true, 0, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ioreg", "-rn", "AppleSmartBattery").Output()
	if err != nil {
		return -1, true, 0, false
	}
	return parseBattery(string(out))
}

var readMachine = func() Machine {
	m := Machine{Battery: -1, Charging: true, SpeedLimit: 100}
	if pct, onAC, temp, ok := readBattery(); ok {
		m.Battery, m.Charging, m.TempC, m.Known = pct, onAC, temp, true
	}
	if limit, ok := readThermal(); ok {
		m.SpeedLimit, m.Known = limit, true
	}
	return m
}

// heatOf is the level the machine is at, given the one it was at: a laptop
// that got hot is warm until it is cool again, not the moment it dips.
func heatOf(m Machine, was string) string {
	switch {
	case m.TempC >= hotC || m.SpeedLimit <= hotSpeedPause:
		return "hot"
	case m.TempC >= warmC || m.SpeedLimit < hotSpeedLimit:
		return "warm"
	case was != "" && (m.TempC > coolC || m.SpeedLimit < hotSpeedLimit):
		return "warm"
	}
	return ""
}

func (m Machine) degrees() string {
	if m.TempC > 0 {
		return fmt.Sprintf("%.0f°C", m.TempC)
	}
	return fmt.Sprintf("CPU held at %d%%", m.SpeedLimit)
}

func (m Machine) why() string {
	parts := []string{}
	if m.TempC >= warmC {
		parts = append(parts, fmt.Sprintf("the battery is at %.0f°C", m.TempC))
	}
	if m.SpeedLimit < hotSpeedLimit {
		parts = append(parts, fmt.Sprintf("macOS holds the CPU at %d%%", m.SpeedLimit))
	}
	return strings.Join(parts, " and ")
}

// One reading a minute, shared by the gate and the watch.
type machineState struct {
	mu      sync.Mutex
	checked time.Time
	m       Machine
	heat    string
}

var machine machineState

func resetMachine() {
	machine.mu.Lock()
	defer machine.mu.Unlock()
	machine.checked, machine.m, machine.heat = time.Time{}, Machine{}, ""
}

func currentMachine(now time.Time) (Machine, string) {
	machine.mu.Lock()
	defer machine.mu.Unlock()
	if machine.checked.IsZero() || now.Sub(machine.checked) >= gateCacheFor {
		machine.checked = now
		machine.m = readMachine()
		machine.heat = heatOf(machine.m, machine.heat)
	}
	return machine.m, machine.heat
}

func tooHot(now time.Time) bool {
	_, heat := currentMachine(now)
	return heat != ""
}

func lowBattery(now time.Time) bool {
	m, _ := currentMachine(now)
	return m.Known && m.Battery >= 0 && !m.Charging && m.Battery < lowBatteryPct
}

// HeatPause says when hot pauses the sessions: "" with the lid closed only,
// "pause" always, "off" never.
func (d *Daemon) mayPause() bool {
	switch strings.ToLower(strings.TrimSpace(d.HeatPause)) {
	case "off", "false", "no":
		return false
	case "pause", "always", "on":
		return true
	}
	return d.lidIsClosed()
}

func (d *Daemon) watchMachine(ctx context.Context) {
	d.thawLeftovers()
	t := time.NewTicker(machineTick)
	defer t.Stop()
	var w machineWatch
	for {
		now := time.Now()
		m, heat := currentMachine(now)
		if m.Known {
			d.setLaptop(m, heat)
			for _, step := range w.observe(now, m, heat, d.mayPause()) {
				d.actOnMachine(ctx, step, m)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

type machineWatch struct {
	heat        string
	paused      bool
	lowTold     bool
	flatTold    bool
	criticalAt  time.Time
	lastHotRing time.Time
}

// observe is one tick: the steps the daemon takes for it.
func (w *machineWatch) observe(now time.Time, m Machine, heat string, mayPause bool) []string {
	var steps []string
	was := w.heat
	w.heat = heat
	switch {
	case heat == "hot" && was != "hot":
		if mayPause && !w.paused {
			w.paused = true
			steps = append(steps, "pause")
		}
		steps = append(steps, "ring hot")
		w.lastHotRing = now
	case heat == "hot" && mayPause && !w.paused:
		w.paused = true
		steps = append(steps, "pause", "ring hot")
	case heat == "warm" && was == "":
		steps = append(steps, "ring warm")
	case heat == "" && was != "":
		if w.paused {
			w.paused = false
			steps = append(steps, "resume")
		}
		steps = append(steps, "ring cool")
	}
	if heat == "hot" && m.TempC >= criticalC && w.criticalAt.IsZero() {
		w.criticalAt = now
		steps = append(steps, "critical")
	}
	if heat != "hot" {
		w.criticalAt = time.Time{}
	}
	onBattery := m.Battery >= 0 && !m.Charging
	switch {
	case !onBattery:
		w.lowTold, w.flatTold = false, false
	case m.Battery < flatBattery && !w.flatTold:
		w.lowTold, w.flatTold = true, true
		steps = append(steps, "ring flat")
	case m.Battery < lowBatteryPct && !w.lowTold:
		w.lowTold = true
		steps = append(steps, "ring low")
	}
	return steps
}

func (d *Daemon) actOnMachine(ctx context.Context, step string, m Machine) {
	switch step {
	case "pause":
		n := d.pauseSessions(m)
		utils.Infof("agent: heat: %s - paused %d session(s)\n", m.why(), n)
		d.setLowPower(true)
	case "resume":
		n := d.resumeSessions()
		utils.Infof("agent: heat: cooled to %s - resumed %d session(s)\n", m.degrees(), n)
		d.setLowPower(false)
	case "ring hot":
		if n := d.pausedCount(); n > 0 {
			d.ringMachine("heat", fmt.Sprintf("too hot: %s. %s paused until it cools - give the laptop air", m.why(), plural(n, "session")))
		} else {
			d.ringMachine("heat", "too hot: "+m.why()+". Nothing new starts until it cools - give the laptop air, or corgi agent heat pause lets it pause the sessions")
		}
	case "ring warm":
		d.setLowPower(true)
		d.ringMachine("heat", "the laptop is warm: "+m.why()+". No new fix starts until it cools")
	case "ring cool":
		d.setLowPower(false)
		d.ringMachine("heat", "cooled to "+m.degrees()+" - back to work")
	case "critical":
		if supervisor.PmsetAllowed() && d.lidIsClosed() {
			d.ringMachine("heat", fmt.Sprintf("%.0f°C with the lid closed - putting the laptop to sleep; open it when it is cool", m.TempC))
			utils.Infof("agent: heat: %.0f°C - sleeping the machine\n", m.TempC)
			go func() {
				time.Sleep(5 * time.Second)
				_ = supervisor.SleepNow()
			}()
		}
	case "ring low":
		d.ringMachine("battery", fmt.Sprintf("on battery at %d%% - no new fix starts until it is plugged in", m.Battery))
	case "ring flat":
		d.ringMachine("battery", fmt.Sprintf("%d%% battery left - plug it in, or the sessions die with it", m.Battery))
	}
}

func (d *Daemon) ringMachine(kind, body string) {
	utils.Infof("agent: %s: %s\n", kind, body)
	if d.muted() {
		return
	}
	if d.Push != nil {
		go d.Push(push.Message{Title: "corgi agent · laptop", Body: body, Category: "laptop", Data: map[string]string{"laptop": kind, "at": time.Now().Format("15:04")}, Thread: "laptop"})
	}
	if d.Notify != nil {
		d.Notify("corgi agent · laptop", body)
	}
}

func (d *Daemon) setLowPower(on bool) {
	if !supervisor.PmsetAllowed() {
		return
	}
	if err := supervisor.LowPowerMode(d.Dir, on); err != nil {
		utils.Infof("agent: low power mode: %v\n", err)
	}
}

func (d *Daemon) setLaptop(m Machine, heat string) {
	d.laptopMu.Lock()
	changed := d.laptop.Machine != m || d.laptop.Heat != heat
	d.laptop.Machine, d.laptop.Heat = m, heat
	d.laptopMu.Unlock()
	if changed {
		d.requestPublish()
	}
}

func (d *Daemon) laptopStatus() *Laptop {
	d.laptopMu.Lock()
	defer d.laptopMu.Unlock()
	if !d.laptop.Known && d.laptop.Lid == "" && d.laptop.Network == "" {
		return nil
	}
	l := d.laptop
	l.Paused = d.pausedCount()
	return &l
}

// Paused sessions are written down before the SIGSTOP, so a daemon that dies
// with them stopped thaws them when it is back.
type pausedRecord struct {
	PIDs  []int     `json:"pids"`
	Since time.Time `json:"since"`
	TempC float64   `json:"tempC,omitempty"`
}

func pausedPath(dir string) string { return filepath.Join(dir, "paused.json") }

func readPaused(dir string) pausedRecord {
	var r pausedRecord
	if data, err := os.ReadFile(pausedPath(dir)); err == nil {
		_ = json.Unmarshal(data, &r)
	}
	return r
}

func (d *Daemon) pausedCount() int { return len(readPaused(d.Dir).PIDs) }

func (d *Daemon) pauseSessions(m Machine) int {
	if d.Sessions == nil {
		return 0
	}
	alive := d.Alive
	if alive == nil {
		alive = proc.Alive
	}
	rec := readPaused(d.Dir)
	seen := map[int]bool{}
	for _, pid := range rec.PIDs {
		seen[pid] = true
	}
	var fresh []int
	for _, s := range d.Sessions.Sessions() {
		if s.ClaudePID <= 0 || seen[s.ClaudePID] || !alive(s.ClaudePID) {
			continue
		}
		fresh = append(fresh, s.ClaudePID)
	}
	if len(fresh) == 0 {
		return 0
	}
	rec.PIDs = append(rec.PIDs, fresh...)
	if rec.Since.IsZero() {
		rec.Since = time.Now()
	}
	rec.TempC = m.TempC
	if data, err := json.Marshal(rec); err == nil {
		_ = atomicfile.Write(pausedPath(d.Dir), data, 0o600)
	}
	for _, pid := range fresh {
		_ = stopProcess(pid)
	}
	d.requestPublish()
	return len(fresh)
}

func (d *Daemon) resumeSessions() int {
	rec := readPaused(d.Dir)
	for _, pid := range rec.PIDs {
		_ = continueProcess(pid)
	}
	_ = os.Remove(pausedPath(d.Dir))
	d.requestPublish()
	return len(rec.PIDs)
}

func (d *Daemon) thawLeftovers() {
	if n := d.pausedCount(); n > 0 {
		utils.Infof("agent: heat: %d session(s) were left paused - resuming them\n", d.resumeSessions())
	}
}

func ReadMachineNow() Machine { return readMachine() }

// Words is the reading in one line: the degrees, the level, the CPU limit.
func (m Machine) Words() string {
	level := heatOf(m, "")
	if level == "" {
		level = "cool"
	}
	parts := []string{level}
	if m.TempC > 0 {
		parts = append(parts, fmt.Sprintf("%.0f°C", m.TempC))
	}
	if m.SpeedLimit < 100 {
		parts = append(parts, fmt.Sprintf("CPU at %d%%", m.SpeedLimit))
	}
	return strings.Join(parts, ", ")
}
