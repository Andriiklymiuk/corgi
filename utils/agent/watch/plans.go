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

// A plan is a goal broken into tasks by a planner, then worked through by
// the daemon: each task is a task on the board (TASK-n), run unattended in
// its own worktree when its turn comes, a few at a time. The plan holds
// the order — which tasks wait for which — and how many run at once. The
// tasks say how they are doing, the way any task does: Doing when a run is
// on it, Review when a pull request is up, Done, Canceled.

const (
	PlanPlanned = "planned"
	PlanRunning = "running"
	PlanStopped = "stopped"
	PlanDone    = "done"
)

// PlanTask is one task of a plan: its board id and which of the plan's
// tasks it waits for.
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
	// Slots is how many of its tasks run at once.
	Slots     int       `json:"slots"`
	State     string    `json:"state"`
	By        string    `json:"by,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	// Notes is what the planner and the runs said worth keeping.
	Notes []string `json:"notes,omitempty"`
}

func (p Plan) Ref() string { return fmt.Sprintf("P-%d", p.ID) }

// TaskIDs is the plan's tasks in order.
func (p Plan) TaskIDs() []int {
	out := make([]int, 0, len(p.Tasks))
	for _, t := range p.Tasks {
		out = append(out, t.ID)
	}
	return out
}

// Has says whether a task belongs to the plan.
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

// Add records a new plan, planned and not yet running.
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

// PlanID reads "P-3", "p-3" or "3".
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

// Update rewrites one plan through f and saves.
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

// SetState moves a plan; it is a no-op when it is already there.
func (l *PlanLog) SetState(id int, state string, now time.Time) (Plan, error) {
	return l.Update(id, now, func(p *Plan) { p.State = state })
}

// Running is every plan the daemon should be moving along.
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

// All is every plan, newest first.
func (l *PlanLog) All() []Plan {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := append([]Plan(nil), l.Plans...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
}

// PlanProgress is where a plan's tasks stand, from the task board.
type PlanProgress struct {
	Todo, Doing, Review, Done, Canceled int
	// Ready is the Todo tasks whose every predecessor is Done or Review.
	Ready []Task
	// Missing lists task ids the board no longer has.
	Missing []int
}

// Finished says a task no longer needs a run.
func taskFinished(state string) bool {
	return state == "Done" || state == "Review" || state == "Canceled"
}

// Progress reads the plan's tasks off the board and says which may start.
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

// Over says whether every task has come to rest.
func (pr PlanProgress) Over() bool { return pr.Todo == 0 && pr.Doing == 0 }
