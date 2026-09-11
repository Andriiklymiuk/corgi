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

// A task is a ticket of your own: written on the phone, the page or the
// command line for later, kept in <agentDir>/watch/tasks.json and shown on
// the same board as the tracker's tickets. It has the columns a small board
// needs and nothing else; the tracker never hears of it.

// KindTask is the inbox kind of a task. A task only leaves the inbox when it
// is finished — unlike a new issue, which leaves once anyone moves it.
const KindTask Kind = "task"

// TaskColumns are the columns a task can be in, in board order.
var TaskColumns = []string{"Todo", "Doing", "Review", "Done", "Canceled"}

type Task struct {
	ID        int       `json:"id"`
	Title     string    `json:"title"`
	Body      string    `json:"body,omitempty"`
	Workspace string    `json:"workspace,omitempty"`
	State     string    `json:"state"`
	By        string    `json:"by,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Ref is what the board shows: TASK-3.
func (t Task) Ref() string { return fmt.Sprintf("TASK-%d", t.ID) }

// Key is what every surface acts on: task:3.
func (t Task) Key() string { return fmt.Sprintf("task:%d", t.ID) }

// Event is the task as the inbox, the board and "Work on it" see it.
func (t Task) Event() Event {
	return Event{Key: t.Key(), Source: "task", Kind: KindTask, Workspace: t.Workspace, Ref: t.Ref(),
		Title: t.Title, Body: t.Body, State: t.State, Mine: true, At: t.UpdatedAt}
}

// TaskLog is the file of tasks.
type TaskLog struct {
	mu    sync.Mutex
	path  string
	Next  int    `json:"next"`
	Tasks []Task `json:"tasks"`
}

func tasksPath(agentDir string) string { return filepath.Join(agentDir, "watch", "tasks.json") }

// LoadTasks reads the file; missing or broken is empty, never an error.
func LoadTasks(agentDir string) *TaskLog {
	l := &TaskLog{path: tasksPath(agentDir), Next: 1}
	if data, err := os.ReadFile(l.path); err == nil {
		_ = json.Unmarshal(data, l)
	}
	if l.Next < 1 {
		l.Next = 1
	}
	for _, t := range l.Tasks {
		if t.ID >= l.Next {
			l.Next = t.ID + 1
		}
	}
	return l
}

func (l *TaskLog) save() error {
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(l.path, data, 0o600)
}

// Add writes a new task in Todo and returns it.
func (l *TaskLog) Add(title, body, workspace, by string, now time.Time) (Task, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Task{}, fmt.Errorf("a task needs a title")
	}
	if len(title) > 200 {
		return Task{}, fmt.Errorf("a title is one line, under 200 characters")
	}
	if len(body) > 20000 {
		return Task{}, fmt.Errorf("a description is under 20000 characters")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	t := Task{ID: l.Next, Title: title, Body: strings.TrimSpace(body), Workspace: strings.TrimSpace(workspace),
		State: TaskColumns[0], By: by, CreatedAt: now, UpdatedAt: now}
	l.Next++
	l.Tasks = append(l.Tasks, t)
	return t, l.save()
}

// Find is a task by ref (TASK-3), key (task:3) or bare number.
func (l *TaskLog) Find(arg string) (Task, bool) {
	id := TaskID(arg)
	if id == 0 {
		return Task{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, t := range l.Tasks {
		if t.ID == id {
			return t, true
		}
	}
	return Task{}, false
}

// TaskID reads a task's number from any spelling of it, 0 for anything else.
func TaskID(arg string) int {
	s := strings.TrimSpace(arg)
	for _, prefix := range []string{"task:", "TASK-", "task-", "Task-", "#"} {
		s = strings.TrimPrefix(s, prefix)
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0
	}
	return n
}

// Move puts a task in a column. The column is one of TaskColumns, matched
// without regard to case, so a phone's "done" and the board's "Done" agree.
func (l *TaskLog) Move(arg, column string, now time.Time) (Task, error) {
	col := TaskColumn(column)
	if col == "" {
		return Task{}, fmt.Errorf("a task's column is one of %s", strings.Join(TaskColumns, ", "))
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	id := TaskID(arg)
	for i := range l.Tasks {
		if l.Tasks[i].ID != id {
			continue
		}
		l.Tasks[i].State = col
		l.Tasks[i].UpdatedAt = now
		return l.Tasks[i], l.save()
	}
	return Task{}, fmt.Errorf("no task %s", arg)
}

// Edit changes a task's title, description or workspace; an empty field
// keeps what it had.
func (l *TaskLog) Edit(arg, title, body, workspace string, now time.Time) (Task, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	id := TaskID(arg)
	for i := range l.Tasks {
		if l.Tasks[i].ID != id {
			continue
		}
		if t := strings.TrimSpace(title); t != "" {
			l.Tasks[i].Title = t
		}
		if b := strings.TrimSpace(body); b != "" {
			l.Tasks[i].Body = b
		}
		if w := strings.TrimSpace(workspace); w != "" {
			l.Tasks[i].Workspace = w
		}
		l.Tasks[i].UpdatedAt = now
		return l.Tasks[i], l.save()
	}
	return Task{}, fmt.Errorf("no task %s", arg)
}

// Remove deletes a task for good.
func (l *TaskLog) Remove(arg string) (Task, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	id := TaskID(arg)
	for i, t := range l.Tasks {
		if t.ID != id {
			continue
		}
		l.Tasks = append(l.Tasks[:i], l.Tasks[i+1:]...)
		return t, l.save()
	}
	return Task{}, fmt.Errorf("no task %s", arg)
}

// TaskColumn is the canonical spelling of a column, "" for none.
func TaskColumn(s string) string {
	for _, c := range TaskColumns {
		if strings.EqualFold(strings.TrimSpace(s), c) {
			return c
		}
	}
	return ""
}

// finishedKeep is how long a finished task stays on the board's Done column.
const finishedKeep = 7 * 24 * time.Hour

// Events is every task as an inbox event, newest change first. A finished
// task leaves after a week; the file keeps it.
func (l *TaskLog) Events(now time.Time) []Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Event, 0, len(l.Tasks))
	for _, t := range l.Tasks {
		if finishedState(t.State) != "" && now.Sub(t.UpdatedAt) > finishedKeep {
			continue
		}
		out = append(out, t.Event())
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out
}

// TaskEvents is LoadTasks(dir).Events(now) for readers of the events log.
func TaskEvents(agentDir string, now time.Time) []Event {
	return LoadTasks(agentDir).Events(now)
}
