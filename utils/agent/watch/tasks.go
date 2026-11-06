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

const KindTask Kind = "task"

var TaskColumns = []string{"Todo", "Doing", "Review", "Done", "Canceled"}

type Task struct {
	ID        int    `json:"id"`
	Title     string `json:"title"`
	Body      string `json:"body,omitempty"`
	Workspace string `json:"workspace,omitempty"`
	State     string `json:"state"`
	By        string `json:"by,omitempty"`
	// Idea is the idea page (its slug) this task was cut from; Tags are the
	// page's tags, so the board can group what belongs together.
	Idea string   `json:"idea,omitempty"`
	Tags []string `json:"tags,omitempty"`
	// Run asks the daemon to start it unattended; it clears once started.
	Run bool `json:"run,omitempty"`
	// Comments is the thread; a person's comment after AnsweredAt wakes a run.
	Comments   []TaskComment `json:"comments,omitempty"`
	AnsweredAt time.Time     `json:"answeredAt,omitzero"`
	// Origin is where a task that came from another machine started; MovedTo
	// says this copy went elsewhere ("build-1 as TASK-7").
	Origin    *TaskOrigin `json:"origin,omitempty"`
	MovedTo   string      `json:"movedTo,omitempty"`
	CreatedAt time.Time   `json:"createdAt"`
	UpdatedAt time.Time   `json:"updatedAt"`
}

// TaskOrigin names a task on the machine it started on.
type TaskOrigin struct {
	Peer      string    `json:"peer"`
	ID        int       `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
}

func (t Task) Ref() string { return fmt.Sprintf("TASK-%d", t.ID) }

func (t Task) Key() string { return fmt.Sprintf("task:%d", t.ID) }

func (t Task) Event() Event {
	return Event{Key: t.Key(), Source: "task", Kind: KindTask, Workspace: t.Workspace, Ref: t.Ref(),
		Title: t.Title, Body: t.WithThread(), State: t.State, Mine: true, At: t.UpdatedAt, Labels: t.Tags}
}

// TaskAgent is who the runs comment as. Its own comments wake nothing.
const TaskAgent = "agent"

const maxTaskComment = 8000

type TaskComment struct {
	By   string    `json:"by"`
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

// WithThread is the description and the comments under it, oldest first.
func (t Task) WithThread() string {
	if len(t.Comments) == 0 {
		return t.Body
	}
	var b strings.Builder
	b.WriteString(t.Body)
	b.WriteString("\n\nComments on this task, oldest first. Do what the newest ones ask:\n")
	for _, c := range t.Comments {
		fmt.Fprintf(&b, "- %s: %s\n", c.By, c.Text)
	}
	return strings.TrimSpace(b.String())
}

// WantsRun says the daemon should start a run on it: it was asked to, or a
// person commented since the last run began.
func (t Task) WantsRun() bool {
	if t.State == "Done" || t.State == "Canceled" {
		return false
	}
	if t.Run {
		return true
	}
	for _, c := range t.Comments {
		if c.By != TaskAgent && c.At.After(t.AnsweredAt) {
			return true
		}
	}
	return false
}

// AskRun marks a task to start unattended.
func (l *TaskLog) AskRun(arg string, now time.Time) (Task, error) {
	return l.update(arg, func(t *Task) { t.Run, t.UpdatedAt = true, now })
}

// Started moves a task to Doing and marks everything said so far as seen.
func (l *TaskLog) Started(arg string, now time.Time) (Task, error) {
	return l.update(arg, func(t *Task) { t.Run, t.State, t.AnsweredAt, t.UpdatedAt = false, "Doing", now, now })
}

// Comment adds to the thread.
func (l *TaskLog) Comment(arg, by, text string, now time.Time) (Task, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Task{}, fmt.Errorf("a comment needs words")
	}
	if len(text) > maxTaskComment {
		return Task{}, fmt.Errorf("a comment is under %d characters", maxTaskComment)
	}
	if by = strings.TrimSpace(by); by == "" {
		by = "someone"
	}
	return l.update(arg, func(t *Task) {
		t.Comments = append(t.Comments, TaskComment{By: by, Text: text, At: now})
		t.UpdatedAt = now
	})
}

func (l *TaskLog) update(arg string, change func(*Task)) (Task, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	id := TaskID(arg)
	for i := range l.Tasks {
		if l.Tasks[i].ID == id {
			change(&l.Tasks[i])
			return l.Tasks[i], l.save()
		}
	}
	return Task{}, fmt.Errorf("no task %s", arg)
}

// Tag files a task under an idea page with its tags.
func (l *TaskLog) Tag(arg, idea string, tags []string, now time.Time) (Task, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	id := TaskID(arg)
	for i := range l.Tasks {
		if l.Tasks[i].ID == id {
			l.Tasks[i].Idea, l.Tasks[i].Tags, l.Tasks[i].UpdatedAt = idea, tags, now
			return l.Tasks[i], l.save()
		}
	}
	return Task{}, fmt.Errorf("no task %s", arg)
}

// ByIdea is every task cut from that idea page, oldest first.
func (l *TaskLog) ByIdea(idea string) []Task {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []Task
	for _, t := range l.Tasks {
		if t.Idea == idea {
			out = append(out, t)
		}
	}
	return out
}

type TaskLog struct {
	mu    sync.Mutex
	path  string
	Next  int    `json:"next"`
	Tasks []Task `json:"tasks"`
}

func tasksPath(agentDir string) string { return filepath.Join(agentDir, "watch", "tasks.json") }

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

func TaskColumn(s string) string {
	for _, c := range TaskColumns {
		if strings.EqualFold(strings.TrimSpace(s), c) {
			return c
		}
	}
	return ""
}

const finishedKeep = 7 * 24 * time.Hour

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

func TaskEvents(agentDir string, now time.Time) []Event {
	return LoadTasks(agentDir).Events(now)
}

const (
	// taskRunSettles gives a run's own "task done" call time to land.
	taskRunSettles = 10 * time.Minute
	// A person who moved the task after the run knows better than the sweep.
	taskMovedByPerson = 2 * time.Minute
)

// Settle keeps the board honest about tasks whose run is over: merged is done,
// closed is canceled, an open pull request is in review, a run that left
// nothing goes to review, and one that failed goes back to Todo with the
// reason. It returns what it moved, for the log.
func (l *TaskLog) Settle(records []FixRecord, pulls map[string]PullStatus, skip func(Task) bool, now time.Time) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var moved []string
	for i := range l.Tasks {
		t := &l.Tasks[i]
		if (t.State != "Doing" && t.State != "Review") || t.WantsRun() || (skip != nil && skip(*t)) {
			continue
		}
		rec, ok := lastRunOfTask(records, t.Key())
		if !ok {
			continue
		}
		column, why := settledColumn(*t, rec, pulls, now)
		if column == "" || column == t.State {
			continue
		}
		t.State, t.UpdatedAt = column, now
		t.Comments = append(t.Comments, TaskComment{By: TaskAgent, Text: why, At: now})
		moved = append(moved, t.Ref()+" → "+column+": "+why)
	}
	if len(moved) > 0 {
		_ = l.save()
	}
	return moved
}

func lastRunOfTask(records []FixRecord, key string) (FixRecord, bool) {
	for i := len(records) - 1; i >= 0; i-- {
		if records[i].Key == key {
			return records[i], records[i].Done()
		}
	}
	return FixRecord{}, false
}

func settledColumn(t Task, rec FixRecord, pulls map[string]PullStatus, now time.Time) (string, string) {
	if len(rec.PRs) > 0 {
		if merged, _, settled := pullsSettled(FixRecord{PRs: rec.PRs}, pulls); settled {
			if merged {
				return "Done", "Its pull request merged."
			}
			return "Canceled", "Its pull request was closed without merging."
		}
		if t.State == "Doing" {
			return "Review", "The run is over and its pull request is open."
		}
		return "", ""
	}
	if t.State != "Doing" || now.Sub(rec.FinishedAt) < taskRunSettles || t.UpdatedAt.After(rec.FinishedAt.Add(taskMovedByPerson)) {
		return "", ""
	}
	if why := strings.TrimSpace(rec.Error); why != "" {
		return "Todo", "The last run failed: " + why
	}
	return "Review", "The run is over and opened no pull request. Read what it said above."
}

// Wanted are the tasks waiting for a run to start: what another machine can
// pick up if this one goes quiet. One that is running stays here.
func (l *TaskLog) Wanted() []Task {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := []Task{}
	for _, t := range l.Tasks {
		if t.WantsRun() && t.MovedTo == "" {
			out = append(out, t)
		}
	}
	return out
}

// HasOrigin says this board already holds that machine's task.
func (l *TaskLog) HasOrigin(peer string, id int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, t := range l.Tasks {
		if t.Origin != nil && t.Origin.ID == id && strings.EqualFold(t.Origin.Peer, peer) {
			return true
		}
	}
	return false
}

// AddCopy puts another machine's task on this board, thread and all. It wants
// a run if it wanted one there.
func (l *TaskLog) AddCopy(from Task, peer string, now time.Time) (Task, error) {
	t, err := l.Add(from.Title, from.Body, from.Workspace, peer, now)
	if err != nil {
		return Task{}, err
	}
	return l.update(t.Ref(), func(c *Task) {
		c.Idea, c.Tags, c.Comments, c.AnsweredAt, c.Run = from.Idea, from.Tags, from.Comments, from.AnsweredAt, from.Run
		c.Origin = &TaskOrigin{Peer: peer, ID: from.ID, CreatedAt: from.CreatedAt}
	})
}

// WithThreadFrom gives a task sent here the thread it had. What was said
// there was already answered there, so it wakes nothing by itself.
func (l *TaskLog) WithThreadFrom(arg string, comments []TaskComment, now time.Time) (Task, error) {
	return l.update(arg, func(t *Task) {
		t.Comments = comments
		t.AnsweredAt = now
	})
}

// MoveAway marks a task as gone to another machine. A run already going here
// keeps its column; the rest leave the board's work.
func (l *TaskLog) MoveAway(id int, createdAt time.Time, to string, now time.Time) (Task, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := range l.Tasks {
		t := &l.Tasks[i]
		if t.ID != id || t.MovedTo != "" || (!createdAt.IsZero() && !t.CreatedAt.Equal(createdAt)) {
			continue
		}
		t.MovedTo, t.Run, t.UpdatedAt = to, false, now
		if t.State == "Todo" || t.State == "Review" {
			t.State = "Canceled"
		}
		t.Comments = append(t.Comments, TaskComment{By: TaskAgent, Text: "Moved to " + to + ".", At: now})
		t.AnsweredAt = now
		_ = l.save()
		return *t, true
	}
	return Task{}, false
}

// TaskCopies is what one machine keeps of another's waiting tasks, so they
// can be picked up when that machine is gone.
type TaskCopies struct {
	Fixes []string  `json:"fixes"`
	Tasks []Task    `json:"tasks"`
	At    time.Time `json:"at"`
}

// TakenTask tells a machine another one took its task.
type TakenTask struct {
	ID        int       `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	Ref       string    `json:"ref"`
}

// TakenFrom are the tasks here that started on that machine.
func (l *TaskLog) TakenFrom(peer string) []TakenTask {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := []TakenTask{}
	for _, t := range l.Tasks {
		if t.Origin != nil && strings.EqualFold(t.Origin.Peer, peer) {
			out = append(out, TakenTask{ID: t.Origin.ID, CreatedAt: t.Origin.CreatedAt, Ref: t.Ref()})
		}
	}
	return out
}

func taskCopiesPath(agentDir, peer string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(peer) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return filepath.Join(agentDir, "watch", "peer-tasks", b.String()+".json")
}

func SaveTaskCopies(agentDir, peer string, c TaskCopies) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	path := taskCopiesPath(agentDir, peer)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return atomicfile.Write(path, data, 0o600)
}

func LoadTaskCopies(agentDir, peer string) (TaskCopies, bool) {
	var c TaskCopies
	data, err := os.ReadFile(taskCopiesPath(agentDir, peer))
	if err != nil || json.Unmarshal(data, &c) != nil {
		return TaskCopies{}, false
	}
	return c, true
}
