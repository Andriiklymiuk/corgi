package watch

import (
	"regexp"
	"strings"
)

var pullRepository = regexp.MustCompile(`https?://[^/]+/(?:[^/]+/)*?([^/]+)/(?:pull|-/merge_requests)/\d+`)

// PullLines is the shape a person posts by hand: the title, then one line
// per pull request named by its repository.
func PullLines(title string, urls []string) string {
	lines := []string{strings.TrimSpace(title)}
	for _, u := range urls {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		if m := pullRepository.FindStringSubmatch(u); m != nil {
			lines = append(lines, m[1]+": "+u)
		} else {
			lines = append(lines, u)
		}
	}
	return strings.Join(lines, "\n")
}
