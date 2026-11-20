package watch

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

const (
	GuestPrefix  = "guest:"
	maxGuestName = 40
)

func IsGuest(by string) bool { return strings.HasPrefix(by, GuestPrefix) }

func GuestName(raw string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(raw) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		}
	}
	name := strings.Join(strings.Fields(b.String()), " ")
	if len(name) > maxGuestName {
		name = strings.TrimSpace(name[:maxGuestName])
	}
	if name == "" {
		name = "someone"
	}
	return GuestPrefix + name
}

func (l *TaskLog) AddForTriage(title, body, workspace, by string, now time.Time) (Task, error) {
	t, err := l.Add(title, body, workspace, by, now)
	if err != nil {
		return t, err
	}
	return l.ToTriage(t.Ref(), now)
}

func (l *TaskLog) ToTriage(arg string, now time.Time) (Task, error) {
	return l.update(arg, func(t *Task) { t.Triage, t.UpdatedAt = true, now })
}

func (l *TaskLog) Accept(arg string, now time.Time) (Task, error) {
	return l.update(arg, func(t *Task) { t.Triage, t.Snoozed, t.UpdatedAt = false, time.Time{}, now })
}

func (l *TaskLog) Snooze(arg string, until, now time.Time) (Task, error) {
	if !until.After(now) {
		return Task{}, fmt.Errorf("snooze until a moment that is still ahead")
	}
	return l.update(arg, func(t *Task) { t.Snoozed, t.UpdatedAt = until, now })
}

func (t Task) Asleep(now time.Time) bool { return !t.Snoozed.IsZero() && now.Before(t.Snoozed) }

func (l *TaskLog) TriageQueue(now time.Time) []Task {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := []Task{}
	for _, t := range l.Tasks {
		if t.Triage && finishedState(t.State) == "" && !t.Asleep(now) {
			out = append(out, t)
		}
	}
	return out
}

func (l *TaskLog) SetCycle(arg, cycle string, now time.Time) (Task, error) {
	return l.update(arg, func(t *Task) { t.Cycle, t.UpdatedAt = strings.TrimSpace(cycle), now })
}

func (l *TaskLog) InCycle(cycle string) []Task {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := []Task{}
	for _, t := range l.Tasks {
		if t.Cycle == cycle {
			out = append(out, t)
		}
	}
	return out
}
