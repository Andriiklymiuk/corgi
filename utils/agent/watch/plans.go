package watch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils/atomicfile"
)

const (
	PlanPlanned = "planned"
	PlanRunning = "running"
	PlanStopped = "stopped"
	PlanDone    = "done"
)

type PlanTask struct {
	ID    int   `json:"id"`
	After []int `json:"after,omitempty"`
}

type Plan struct {
	ID        int        `json:"id"`
	Goal      string     `json:"goal"`
	Summary   string     `json:"summary,omitempty"`
	Workspace string     `json:"workspace"`
	Model     string     `json:"model,omitempty"`
	Tasks     []PlanTask `json:"tasks"`
	Slots     int        `json:"slots"`
	State     string     `json:"state"`
	By        string     `json:"by,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
	Notes     []string   `json:"notes,omitempty"`
}

func (p Plan) Ref() string { return fmt.Sprintf("P-%d", p.ID) }

func (p Plan) TaskIDs() []int {
	out := make([]int, 0, len(p.Tasks))
	for _, t := range p.Tasks {
		out = append(out, t.ID)
	}
	return out
}

func (p Plan) Has(taskID int) bool {
	for _, t := range p.Tasks {
		if t.ID == taskID {
			return true
		}
	}
	return false
}

type PlanLog struct {
	mu    sync.Mutex
	path  string
	Next  int    `json:"next"`
	Plans []Plan `json:"plans"`
}

func plansPath(agentDir string) string { return filepath.Join(agentDir, "watch", "plans.json") }

func LoadPlans(agentDir string) *PlanLog {
	l := &PlanLog{path: plansPath(agentDir), Next: 1}
	if data, err := os.ReadFile(l.path); err == nil {
		_ = json.Unmarshal(data, l)
	}
	if l.Next < 1 {
		l.Next = 1
	}
	return l
}

func (l *PlanLog) save() error {
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	return atomicfile.Write(l.path, data, 0o600)
}

func (l *PlanLog) Add(goal, summary, workspace, model, by string, tasks []PlanTask, slots int, now time.Time) (Plan, error) {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return Plan{}, fmt.Errorf("a plan needs a goal")
	}
	if len(tasks) == 0 {
		return Plan{}, fmt.Errorf("a plan needs at least one task")
	}
	if slots < 1 {
		slots = 1
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	p := Plan{ID: l.Next, Goal: goal, Summary: strings.TrimSpace(summary), Workspace: workspace, Model: model, Tasks: tasks,
		Slots: slots, State: PlanPlanned, By: by, CreatedAt: now, UpdatedAt: now}
	l.Next++
	l.Plans = append(l.Plans, p)
	return p, l.save()
}

func PlanID(arg string) int {
	s := strings.TrimSpace(strings.ToUpper(arg))
	s = strings.TrimPrefix(s, "P-")
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0
	}
	return n
}

func (l *PlanLog) Find(arg string) (Plan, bool) {
	id := PlanID(arg)
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, p := range l.Plans {
		if p.ID == id {
			return p, true
		}
	}
	return Plan{}, false
}

func (l *PlanLog) Update(id int, now time.Time, f func(p *Plan)) (Plan, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := range l.Plans {
		if l.Plans[i].ID == id {
			f(&l.Plans[i])
			l.Plans[i].UpdatedAt = now
			return l.Plans[i], l.save()
		}
	}
	return Plan{}, fmt.Errorf("no plan P-%d", id)
}

func (l *PlanLog) SetState(id int, state string, now time.Time) (Plan, error) {
	return l.Update(id, now, func(p *Plan) { p.State = state })
}

func (l *PlanLog) Running() []Plan {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []Plan
	for _, p := range l.Plans {
		if p.State == PlanRunning {
			out = append(out, p)
		}
	}
	return out
}

func (l *PlanLog) All() []Plan {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := append([]Plan(nil), l.Plans...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
}

type PlanProgress struct {
	Todo, Doing, Review, Done, Canceled int
	Ready                               []Task
	Missing                             []int
}

func taskFinished(state string) bool {
	return state == "Done" || state == "Review" || state == "Canceled"
}

func (p Plan) Progress(tasks *TaskLog) PlanProgress {
	var out PlanProgress
	state := map[int]string{}
	byID := map[int]Task{}
	for _, t := range p.Tasks {
		task, ok := tasks.Find(fmt.Sprintf("task:%d", t.ID))
		if !ok {
			out.Missing = append(out.Missing, t.ID)
			continue
		}
		state[t.ID] = task.State
		byID[t.ID] = task
		switch task.State {
		case "Todo":
			out.Todo++
		case "Doing":
			out.Doing++
		case "Review":
			out.Review++
		case "Done":
			out.Done++
		case "Canceled":
			out.Canceled++
		}
	}
	for _, t := range p.Tasks {
		if state[t.ID] != "Todo" {
			continue
		}
		ready := true
		for _, dep := range t.After {
			if s, ok := state[dep]; ok && !taskFinished(s) {
				ready = false
				break
			}
		}
		if ready {
			out.Ready = append(out.Ready, byID[t.ID])
		}
	}
	return out
}

func (pr PlanProgress) Over() bool { return pr.Todo == 0 && pr.Doing == 0 }
