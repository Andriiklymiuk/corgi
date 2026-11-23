package watch

import (
	"regexp"
	"strings"
	"time"
)

var retroTitle = regexp.MustCompile(`(?i)^retro:\s*`)

func TaskHeadline(t Task) string { return retroTitle.ReplaceAllString(strings.TrimSpace(t.Title), "") }

func TasksFiledBy(tasks []Task, workspace string, since time.Time, report string) []Task {
	if since.IsZero() {
		return nil
	}
	var filed []Task
	for _, t := range tasks {
		named := regexp.MustCompile(regexp.QuoteMeta(t.Ref()) + `\b`)
		if t.Workspace == workspace && !t.CreatedAt.Before(since) && named.MatchString(report) {
			filed = append(filed, t)
		}
	}
	return filed
}
