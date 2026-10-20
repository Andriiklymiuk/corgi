package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// What corgi may do as root, and nothing else: the four pmset lines a sudoers
// rule allows without a password. Without the rule every call here is a no-op
// that says so, never a password prompt.
const pmsetBin = "/usr/bin/pmset"

var pmsetRules = []string{
	pmsetBin + " -a disablesleep 1",
	pmsetBin + " -a disablesleep 0",
	pmsetBin + " -b lowpowermode 1",
	pmsetBin + " -b lowpowermode 0",
}

func SudoersRule() string {
	name := "$USER"
	if u, err := user.Current(); err == nil && u.Username != "" {
		name = u.Username
	}
	return name + " ALL=(root) NOPASSWD: " + strings.Join(pmsetRules, ", ")
}

// SudoersInstall is the one line that writes the rule; only the person can run it.
func SudoersInstall() string {
	return fmt.Sprintf("echo '%s' | sudo tee /etc/sudoers.d/corgi >/dev/null && sudo chmod 440 /etc/sudoers.d/corgi", SudoersRule())
}

var runSudo = func(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "sudo", append([]string{"-n", pmsetBin}, args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("sudo pmset %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

var checkSudo = func() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "sudo", "-n", "-l", pmsetBin, "-a", "disablesleep", "1").Run() == nil
}

var (
	allowedMu sync.Mutex
	allowedAt time.Time
	allowed   bool
)

// PmsetAllowed says whether the sudoers rule is in place; asked once an hour.
func PmsetAllowed() bool {
	allowedMu.Lock()
	defer allowedMu.Unlock()
	if allowedAt.IsZero() || time.Since(allowedAt) > time.Hour {
		allowedAt = time.Now()
		allowed = checkSudo()
	}
	return allowed
}

func ResetPmsetAllowed() {
	allowedMu.Lock()
	defer allowedMu.Unlock()
	allowedAt = time.Time{}
}

// The journal: what corgi changed, so it undoes only that.
type powerRecord struct {
	DisableSleep bool `json:"disableSleep,omitempty"`
	LowPower     bool `json:"lowPower,omitempty"`
}

func powerPath(dir string) string { return filepath.Join(dir, "power.json") }

func readPower(dir string) powerRecord {
	var r powerRecord
	if data, err := os.ReadFile(powerPath(dir)); err == nil {
		_ = json.Unmarshal(data, &r)
	}
	return r
}

func writePower(dir string, r powerRecord) {
	if !r.DisableSleep && !r.LowPower {
		_ = os.Remove(powerPath(dir))
		return
	}
	if data, err := json.Marshal(r); err == nil {
		_ = os.WriteFile(powerPath(dir), data, 0o600)
	}
}

// DisableSleep lets the lid close without sleeping the machine, and writes
// down that corgi did it. Already on by hand: nothing is written, nothing is
// undone later.
func DisableSleep(dir string, on bool) error {
	r := readPower(dir)
	if on {
		if SleepDisabled() {
			return nil
		}
		if err := runSudo("-a", "disablesleep", "1"); err != nil {
			return err
		}
		r.DisableSleep = true
		writePower(dir, r)
		return nil
	}
	if !r.DisableSleep {
		return nil
	}
	if err := runSudo("-a", "disablesleep", "0"); err != nil {
		return err
	}
	r.DisableSleep = false
	writePower(dir, r)
	return nil
}

func LowPowerMode(dir string, on bool) error {
	r := readPower(dir)
	if r.LowPower == on {
		return nil
	}
	v := "0"
	if on {
		v = "1"
	}
	if err := runSudo("-b", "lowpowermode", v); err != nil {
		return err
	}
	r.LowPower = on
	writePower(dir, r)
	return nil
}

// SleepNow needs no root: the machine sleeps, SleepDisabled or not, since
// 'pmset sleepnow' is what the Apple menu's Sleep does.
func SleepNow() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, pmsetBin, "sleepnow").Run()
}

var readPmsetG = func() string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, pmsetBin, "-g").Output()
	return string(out)
}

func SleepDisabled() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	for _, line := range strings.Split(readPmsetG(), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[0] == "SleepDisabled" {
			return f[1] == "1"
		}
	}
	return false
}
