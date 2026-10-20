package daemon

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/sessions"
)

const ioregBattery = `+-o AppleSmartBattery  <class AppleSmartBattery, id 0x100000123, registered, matched, active, busy 0 (0 ms), retain 8>
    {
      "CurrentCapacity" = 62
      "ExternalConnected" = No
      "Temperature" = 4312
      "MaxCapacity" = 100
      "IsCharging" = No
    }
`

func TestParseBatteryReadsPercentPowerAndDegrees(t *testing.T) {
	pct, onAC, temp, ok := parseBattery(ioregBattery)
	if !ok || pct != 62 || onAC || temp != 43.12 {
		t.Fatalf("got pct=%d ac=%v temp=%v ok=%v", pct, onAC, temp, ok)
	}
	if _, _, _, ok := parseBattery("nothing here"); ok {
		t.Fatal("no battery lines is not a reading")
	}
}

func TestHeatLevelsHaveHysteresis(t *testing.T) {
	cool := Machine{TempC: 32, SpeedLimit: 100}
	if got := heatOf(cool, ""); got != "" {
		t.Fatalf("32°C is fine, got %q", got)
	}
	if got := heatOf(Machine{TempC: 41, SpeedLimit: 100}, ""); got != "warm" {
		t.Fatalf("41°C is warm, got %q", got)
	}
	if got := heatOf(Machine{TempC: 30, SpeedLimit: 50}, ""); got != "warm" {
		t.Fatalf("a CPU held at 50%% is warm, got %q", got)
	}
	if got := heatOf(Machine{TempC: 46, SpeedLimit: 100}, "warm"); got != "hot" {
		t.Fatalf("46°C is hot, got %q", got)
	}
	if got := heatOf(Machine{TempC: 39, SpeedLimit: 100}, "hot"); got != "warm" {
		t.Fatalf("39°C after hot is still warm until it is under %v, got %q", coolC, got)
	}
	if got := heatOf(Machine{TempC: 37, SpeedLimit: 100}, "warm"); got != "" {
		t.Fatalf("37°C is cool again, got %q", got)
	}
}

func TestTheMachineWatchPausesOnHotAndResumesOnCool(t *testing.T) {
	var w machineWatch
	now := time.Now()
	if steps := w.observe(now, Machine{TempC: 30, SpeedLimit: 100, Charging: true, Battery: 90}, "", true); len(steps) != 0 {
		t.Fatalf("nothing to say when cool: %v", steps)
	}
	steps := w.observe(now, Machine{TempC: 41, SpeedLimit: 100, Charging: true, Battery: 90}, "warm", true)
	if !slices.Equal(steps, []string{"ring warm"}) {
		t.Fatalf("warm rings once: %v", steps)
	}
	if steps := w.observe(now, Machine{TempC: 42, SpeedLimit: 100, Charging: true, Battery: 90}, "warm", true); len(steps) != 0 {
		t.Fatalf("still warm is not news: %v", steps)
	}
	steps = w.observe(now, Machine{TempC: 46, SpeedLimit: 100, Charging: true, Battery: 90}, "hot", false)
	if !slices.Equal(steps, []string{"ring hot"}) {
		t.Fatalf("hot with the lid open rings but does not pause: %v", steps)
	}
	steps = w.observe(now, Machine{TempC: 47, SpeedLimit: 100, Charging: true, Battery: 90}, "hot", true)
	if !slices.Equal(steps, []string{"pause", "ring hot"}) {
		t.Fatalf("the lid closing while hot pauses: %v", steps)
	}
	steps = w.observe(now, Machine{TempC: 51, SpeedLimit: 100, Charging: true, Battery: 90}, "hot", true)
	if !slices.Equal(steps, []string{"critical"}) {
		t.Fatalf("past %v once: %v", criticalC, steps)
	}
	steps = w.observe(now, Machine{TempC: 39, SpeedLimit: 100, Charging: true, Battery: 90}, "warm", true)
	if len(steps) != 0 {
		t.Fatalf("hot to warm resumes nothing yet: %v", steps)
	}
	steps = w.observe(now, Machine{TempC: 36, SpeedLimit: 100, Charging: true, Battery: 90}, "", true)
	if !slices.Equal(steps, []string{"resume", "ring cool"}) {
		t.Fatalf("cool resumes and says so: %v", steps)
	}
}

func TestTheMachineWatchSaysLowBatteryOnce(t *testing.T) {
	lidClosedNow.Store(true)
	defer lidClosedNow.Store(false)
	var w machineWatch
	now := time.Now()
	steps := w.observe(now, Machine{TempC: 30, SpeedLimit: 100, Battery: 18}, "", false)
	if !slices.Equal(steps, []string{"ring low"}) {
		t.Fatalf("18%% on battery: %v", steps)
	}
	if steps := w.observe(now, Machine{TempC: 30, SpeedLimit: 100, Battery: 15}, "", false); len(steps) != 0 {
		t.Fatalf("15%% is the same news: %v", steps)
	}
	steps = w.observe(now, Machine{TempC: 30, SpeedLimit: 100, Battery: 8}, "", false)
	if !slices.Equal(steps, []string{"ring flat"}) {
		t.Fatalf("8%% is news again: %v", steps)
	}
	w.observe(now, Machine{TempC: 30, SpeedLimit: 100, Battery: 8, Charging: true}, "", false)
	steps = w.observe(now, Machine{TempC: 30, SpeedLimit: 100, Battery: 8}, "", false)
	if !slices.Equal(steps, []string{"ring flat"}) {
		t.Fatalf("unplugged again rings again: %v", steps)
	}
}

func TestPauseWritesTheJournalBeforeStoppingAndResumeClearsIt(t *testing.T) {
	oldStop, oldCont := stopProcess, continueProcess
	defer func() { stopProcess, continueProcess = oldStop, oldCont }()
	var stopped, continued []int
	stopProcess = func(pid int) error {
		if readPaused(testDaemonDir).PIDs == nil {
			t.Error("the journal must be on disk before the SIGSTOP")
		}
		stopped = append(stopped, pid)
		return nil
	}
	continueProcess = func(pid int) error { continued = append(continued, pid); return nil }
	d := testDaemon(t)
	testDaemonDir = d.Dir
	d.Alive = func(int) bool { return true }
	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "a", ClaudePID: 101, Cwd: t.TempDir()})
	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "b", ClaudePID: 102, Cwd: t.TempDir()})
	if n := d.pauseSessions(Machine{TempC: 46}); n != 2 {
		t.Fatalf("paused %d", n)
	}
	if n := d.pauseSessions(Machine{TempC: 46}); n != 0 {
		t.Fatalf("already paused sessions are not stopped twice, got %d", n)
	}
	if d.pausedCount() != 2 || len(stopped) != 2 {
		t.Fatalf("count=%d stopped=%v", d.pausedCount(), stopped)
	}
	if n := d.resumeSessions(); n != 2 || len(continued) != 2 {
		t.Fatalf("resumed %d, continued %v", n, continued)
	}
	if _, err := os.Stat(pausedPath(d.Dir)); !os.IsNotExist(err) {
		t.Fatal("the journal is gone once everything runs again")
	}
	if d.laptopStatus() != nil {
		t.Fatal("nothing read yet, nothing published")
	}
	d.setLaptop(Machine{Battery: 50, Known: true}, "warm")
	if l := d.laptopStatus(); l == nil || l.Heat != "warm" || l.Battery != 50 {
		t.Fatalf("the phone reads the machine from the status: %+v", l)
	}
}

var testDaemonDir string

func TestADaemonBackFromADeathThawsWhatItLeftPaused(t *testing.T) {
	oldCont := continueProcess
	defer func() { continueProcess = oldCont }()
	var continued []int
	continueProcess = func(pid int) error { continued = append(continued, pid); return nil }
	d := testDaemon(t)
	if err := os.WriteFile(pausedPath(d.Dir), []byte(`{"pids":[7,8]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	d.thawLeftovers()
	if !slices.Equal(continued, []int{7, 8}) {
		t.Fatalf("continued %v", continued)
	}
}

func TestMachineWordsNameTheCause(t *testing.T) {
	m := Machine{TempC: 46.4, SpeedLimit: 50}
	if got := m.why(); !strings.Contains(got, "46°C") || !strings.Contains(got, "50%") {
		t.Fatalf("got %q", got)
	}
	if got := (Machine{SpeedLimit: 40}).degrees(); got != "CPU held at 40%" {
		t.Fatalf("no battery reading falls back to the CPU: %q", got)
	}
}
