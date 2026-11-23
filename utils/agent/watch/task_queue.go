package watch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	DefaultTaskLease = time.Hour
	MaxTaskLease     = 24 * time.Hour
	taskLockStale    = 30 * time.Second
)

type NextQuery struct {
	Owner     string
	Tags      []string
	Workspace string
	Lease     time.Duration
}

func (t Task) LeaseOver(now time.Time) bool {
	return !t.LeaseUntil.IsZero() && !now.Before(t.LeaseUntil)
}

func (l *TaskLog) Ready(q NextQuery, now time.Time) []Task {
	var out []Task
	for _, t := range l.ByPriority() {
		if !l.readyFor(t, q, now) {
			continue
		}
		out = append(out, t)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := priorityRank(out[i].Priority), priorityRank(out[j].Priority); a != b {
			return a < b
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (l *TaskLog) readyFor(t Task, q NextQuery, now time.Time) bool {
	free := t.State == "Todo" && t.Owner == ""
	lapsed := t.State == "Doing" && t.LeaseOver(now)
	if !free && !lapsed {
		return false
	}
	if t.Triage || t.Snoozed.After(now) || len(l.OpenBlockers(t)) > 0 {
		return false
	}
	if q.Workspace != "" && !strings.EqualFold(t.Workspace, q.Workspace) {
		return false
	}
	for _, want := range q.Tags {
		if !containsFold(t.Tags, strings.TrimSpace(want)) {
			return false
		}
	}
	return true
}

func cleanLease(d time.Duration) time.Duration {
	switch {
	case d <= 0:
		return DefaultTaskLease
	case d > MaxTaskLease:
		return MaxTaskLease
	}
	return d
}

func TakeNextTask(agentDir string, q NextQuery, now time.Time) (Task, bool, error) {
	q.Owner = strings.TrimSpace(q.Owner)
	if q.Owner == "" || len(q.Owner) > maxTaskOwner {
		return Task{}, false, fmt.Errorf("a claim needs a name under %d characters", maxTaskOwner)
	}
	unlock, err := lockTasks(agentDir)
	if err != nil {
		return Task{}, false, err
	}
	defer unlock()
	tasks := LoadTasks(agentDir)
	ready := tasks.Ready(q, now)
	if len(ready) == 0 {
		return Task{}, false, nil
	}
	lease := cleanLease(q.Lease)
	t, err := tasks.update(fmt.Sprint(ready[0].ID), func(t *Task) {
		t.Owner, t.ClaimedAt, t.LeaseUntil, t.State, t.UpdatedAt = q.Owner, now, now.Add(lease), "Doing", now
	})
	return t, err == nil, err
}

func RenewTask(agentDir, arg, who string, lease time.Duration, now time.Time) (Task, error) {
	unlock, err := lockTasks(agentDir)
	if err != nil {
		return Task{}, err
	}
	defer unlock()
	tasks := LoadTasks(agentDir)
	t, ok := tasks.Find(arg)
	switch {
	case !ok:
		return Task{}, fmt.Errorf("no task %s", arg)
	case !strings.EqualFold(t.Owner, strings.TrimSpace(who)):
		return t, fmt.Errorf("%s is not %s's: it is held by %s", t.Ref(), who, firstSet(t.Owner, "nobody"))
	case finishedState(t.State) != "":
		return t, fmt.Errorf("%s is already %s", t.Ref(), t.State)
	}
	return tasks.update(arg, func(t *Task) { t.LeaseUntil, t.UpdatedAt = now.Add(cleanLease(lease)), now })
}

func lockTasks(agentDir string) (func(), error) {
	path := filepath.Join(agentDir, "watch", "tasks.lock")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = f.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		if info, serr := os.Stat(path); serr == nil && time.Since(info.ModTime()) > taskLockStale {
			_ = os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, errors.New("the task list is busy, try again")
		}
		time.Sleep(15 * time.Millisecond)
	}
}

func FinishTask(agentDir, arg, by, result string, now time.Time) (Task, error) {
	result = strings.TrimSpace(result)
	if len(result) > maxTaskComment {
		return Task{}, fmt.Errorf("a result is under %d characters", maxTaskComment)
	}
	unlock, err := lockTasks(agentDir)
	if err != nil {
		return Task{}, err
	}
	defer unlock()
	return LoadTasks(agentDir).update(arg, func(t *Task) {
		if result != "" {
			t.Comments = append(t.Comments, TaskComment{By: firstSet(strings.TrimSpace(by), TaskAgent), Text: result, At: now})
		}
		t.State, t.Run, t.LeaseUntil, t.AnsweredAt, t.UpdatedAt = "Done", false, time.Time{}, now, now
	})
}
