package watch

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	maxTaskLinks    = 20
	maxTaskLinkSize = 500
	maxTaskOwner    = 80
	taskDueLayout   = "2006-01-02"
)

var TaskPriorities = []string{"urgent", "high", "medium", "low"}

func TaskPriority(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || s == "none" {
		return "", true
	}
	for _, p := range TaskPriorities {
		if s == p {
			return p, true
		}
	}
	return "", false
}

func priorityRank(p string) int {
	for i, known := range TaskPriorities {
		if p == known {
			return i
		}
	}
	return len(TaskPriorities)
}

func CleanTaskLink(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) > maxTaskLinkSize {
		return "", fmt.Errorf("a link is under %d characters", maxTaskLinkSize)
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", fmt.Errorf("a link is an http(s) address: %q is not one", raw)
	}
	if u.User != nil {
		return "", fmt.Errorf("a link must not carry a name or password")
	}
	return u.String(), nil
}

func LinkSource(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return "link"
	}
	host, path := strings.ToLower(u.Hostname()), u.Path
	switch {
	case host == "linear.app" || strings.HasSuffix(host, ".linear.app"):
		return "linear"
	case strings.HasSuffix(host, ".atlassian.net") && strings.HasPrefix(path, "/wiki"):
		return "confluence"
	case strings.HasSuffix(host, ".atlassian.net") || strings.Contains(host, "jira") || strings.HasPrefix(path, "/browse/"):
		return "jira"
	case host == "github.com" || strings.HasPrefix(host, "github."):
		return "github"
	case strings.Contains(host, "gitlab") || strings.Contains(path, "/-/merge_requests/") || strings.Contains(path, "/-/issues/"):
		return "gitlab"
	}
	return "link"
}

type TaskLink struct {
	URL    string `json:"url"`
	Source string `json:"source"`
	Label  string `json:"label"`
}

func LinkLabel(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return link
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	last := parts[len(parts)-1]
	switch LinkSource(link) {
	case "github":
		if len(parts) >= 4 && (parts[2] == "pull" || parts[2] == "issues") {
			return parts[0] + "/" + parts[1] + "#" + parts[3]
		}
	case "gitlab":
		for i, p := range parts {
			if p == "-" && i+2 < len(parts) && i > 0 {
				mark := "#"
				if parts[i+1] == "merge_requests" {
					mark = "!"
				}
				return strings.Join(parts[:i], "/") + mark + parts[i+2]
			}
		}
	case "jira":
		for i, p := range parts {
			if p == "browse" && i+1 < len(parts) {
				return parts[i+1]
			}
		}
	case "linear":
		for i, p := range parts {
			if p == "issue" && i+1 < len(parts) {
				return parts[i+1]
			}
		}
	}
	if last == "" {
		return u.Hostname()
	}
	return u.Hostname() + "/" + last
}

func (t Task) LinkRows() []TaskLink {
	rows := make([]TaskLink, 0, len(t.Links))
	for _, link := range t.Links {
		rows = append(rows, TaskLink{URL: link, Source: LinkSource(link), Label: LinkLabel(link)})
	}
	return rows
}

func (l *TaskLog) Link(arg string, links []string, now time.Time) (Task, error) {
	clean := make([]string, 0, len(links))
	for _, raw := range links {
		link, err := CleanTaskLink(raw)
		if err != nil {
			return Task{}, err
		}
		clean = append(clean, link)
	}
	if t, ok := l.Find(arg); ok && len(uniqueLinks(append(t.Links, clean...))) > maxTaskLinks {
		return Task{}, fmt.Errorf("a task holds up to %d links", maxTaskLinks)
	}
	return l.update(arg, func(t *Task) {
		t.Links, t.UpdatedAt = uniqueLinks(append(t.Links, clean...)), now
	})
}

func (l *TaskLog) Unlink(arg, link string, now time.Time) (Task, error) {
	link = strings.TrimSpace(link)
	return l.update(arg, func(t *Task) {
		kept := t.Links[:0:0]
		for _, have := range t.Links {
			if have != link {
				kept = append(kept, have)
			}
		}
		t.Links, t.UpdatedAt = kept, now
	})
}

func (l *TaskLog) Claim(arg, who string, force bool, now time.Time) (Task, error) {
	who = strings.TrimSpace(who)
	if who == "" || len(who) > maxTaskOwner {
		return Task{}, fmt.Errorf("a claim needs a name under %d characters", maxTaskOwner)
	}
	if t, ok := l.Find(arg); ok && !force && t.Owner != "" && !strings.EqualFold(t.Owner, who) && finishedState(t.State) == "" {
		return t, fmt.Errorf("%s is %s's since %s - they release it, or you take it with --force", t.Ref(), t.Owner, t.ClaimedAt.Local().Format("Jan 2 15:04"))
	}
	return l.update(arg, func(t *Task) { t.Owner, t.ClaimedAt, t.UpdatedAt = who, now, now })
}

func (l *TaskLog) Release(arg string, now time.Time) (Task, error) {
	return l.update(arg, func(t *Task) { t.Owner, t.ClaimedAt, t.LeaseUntil, t.UpdatedAt = "", time.Time{}, time.Time{}, now })
}

func (l *TaskLog) SetPriority(arg, priority string, now time.Time) (Task, error) {
	p, ok := TaskPriority(priority)
	if !ok {
		return Task{}, fmt.Errorf("a priority is one of %s, or none", strings.Join(TaskPriorities, ", "))
	}
	return l.update(arg, func(t *Task) { t.Priority, t.UpdatedAt = p, now })
}

func (l *TaskLog) SetDue(arg, due string, now time.Time) (Task, error) {
	due = strings.TrimSpace(due)
	if due != "" && due != "none" {
		if _, err := time.Parse(taskDueLayout, due); err != nil {
			return Task{}, fmt.Errorf("a due date is a day like 2026-12-01, or none")
		}
	} else {
		due = ""
	}
	return l.update(arg, func(t *Task) { t.Due, t.UpdatedAt = due, now })
}

func (l *TaskLog) SetBlockedBy(arg string, blockers []string, now time.Time) (Task, error) {
	self := TaskID(arg)
	ids := []int{}
	for _, raw := range blockers {
		id := TaskID(raw)
		if _, ok := l.Find(raw); !ok {
			return Task{}, fmt.Errorf("no task %s to wait for", raw)
		}
		if id == self {
			return Task{}, fmt.Errorf("a task cannot wait for itself")
		}
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		if l.waitsFor(id, self, map[int]bool{}) {
			return Task{}, fmt.Errorf("TASK-%d already waits for TASK-%d: that would be a circle", id, self)
		}
	}
	return l.update(arg, func(t *Task) { t.BlockedBy, t.UpdatedAt = ids, now })
}

func (l *TaskLog) waitsFor(from, target int, seen map[int]bool) bool {
	if seen[from] {
		return false
	}
	seen[from] = true
	t, ok := l.Find(fmt.Sprint(from))
	if !ok {
		return false
	}
	for _, id := range t.BlockedBy {
		if id == target || l.waitsFor(id, target, seen) {
			return true
		}
	}
	return false
}

func (l *TaskLog) OpenBlockers(t Task) []string {
	open := []string{}
	for _, id := range t.BlockedBy {
		if blocker, ok := l.Find(fmt.Sprint(id)); ok && finishedState(blocker.State) == "" {
			open = append(open, blocker.Ref())
		}
	}
	return open
}

func (l *TaskLog) ByPriority() []Task {
	l.mu.Lock()
	out := append([]Task(nil), l.Tasks...)
	l.mu.Unlock()
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := priorityRank(out[i].Priority), priorityRank(out[j].Priority); a != b {
			return a < b
		}
		if a, b := out[i].Due, out[j].Due; a != b && a != "" && b != "" {
			return a < b
		} else if a != b {
			return a != ""
		}
		return false
	})
	return out
}

func (t Task) Overdue(now time.Time) bool {
	if t.Due == "" || finishedState(t.State) != "" {
		return false
	}
	return t.Due < now.Local().Format(taskDueLayout)
}

func (t Task) HeldByAnother(me string) bool {
	return t.Owner != "" && !strings.EqualFold(t.Owner, me)
}
