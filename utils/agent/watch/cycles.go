package watch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils/atomicfile"
)

const (
	maxCycleName = 40
	maxCycleDays = 90
)

type Cycle struct {
	Name     string    `json:"name"`
	Start    string    `json:"start"`
	End      string    `json:"end"`
	ClosedAt time.Time `json:"closedAt,omitzero"`
	Done     []string  `json:"done,omitempty"`
	Slipped  []string  `json:"slipped,omitempty"`
}

func (c Cycle) Open() bool { return c.ClosedAt.IsZero() }

func (c Cycle) DaysLeft(now time.Time) int {
	end, err := time.ParseInLocation(taskDueLayout, c.End, now.Location())
	if err != nil {
		return 0
	}
	today, _ := time.ParseInLocation(taskDueLayout, now.Format(taskDueLayout), now.Location())
	return int(end.Sub(today).Hours()/24) + 1
}

type CycleLog struct {
	mu     sync.Mutex
	path   string
	Cycles []Cycle `json:"cycles"`
}

func LoadCycles(agentDir string) *CycleLog {
	l := &CycleLog{path: filepath.Join(agentDir, "watch", "cycles.json")}
	if data, err := os.ReadFile(l.path); err == nil {
		_ = json.Unmarshal(data, l)
	}
	return l
}

func (l *CycleLog) save() error {
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(l.path, data, 0o600)
}

func (l *CycleLog) Current() (Cycle, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.Cycles) - 1; i >= 0; i-- {
		if l.Cycles[i].Open() {
			return l.Cycles[i], true
		}
	}
	return Cycle{}, false
}

func (l *CycleLog) Find(name string) (Cycle, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, c := range l.Cycles {
		if strings.EqualFold(c.Name, strings.TrimSpace(name)) {
			return c, true
		}
	}
	return Cycle{}, false
}

func (l *CycleLog) Start(name string, days int, now time.Time) (Cycle, error) {
	if _, open := l.Current(); open {
		return Cycle{}, fmt.Errorf("a cycle is open already - close it first")
	}
	if days < 1 || days > maxCycleDays {
		return Cycle{}, fmt.Errorf("a cycle lasts 1 to %d days", maxCycleDays)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		year, week := now.ISOWeek()
		name = fmt.Sprintf("%d-W%02d", year, week)
	}
	if len(name) > maxCycleName {
		return Cycle{}, fmt.Errorf("a cycle's name is under %d characters", maxCycleName)
	}
	if _, taken := l.Find(name); taken {
		return Cycle{}, fmt.Errorf("a cycle named %s exists already", name)
	}
	c := Cycle{Name: name, Start: now.Format(taskDueLayout), End: now.AddDate(0, 0, days-1).Format(taskDueLayout)}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Cycles = append(l.Cycles, c)
	return c, l.save()
}

func (l *CycleLog) Close(tasks *TaskLog, now time.Time) (Cycle, error) {
	current, open := l.Current()
	if !open {
		return Cycle{}, fmt.Errorf("no cycle is open")
	}
	for _, t := range tasks.InCycle(current.Name) {
		if finishedState(t.State) != "" {
			current.Done = append(current.Done, t.Ref())
			continue
		}
		current.Slipped = append(current.Slipped, t.Ref())
		if _, err := tasks.SetCycle(t.Ref(), "", now); err != nil {
			return Cycle{}, err
		}
	}
	current.ClosedAt = now
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := range l.Cycles {
		if l.Cycles[i].Name == current.Name {
			l.Cycles[i] = current
		}
	}
	return current, l.save()
}

func (l *CycleLog) All() []Cycle {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Cycle(nil), l.Cycles...)
}
