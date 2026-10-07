package watch

import (
	"regexp"
	"strings"
)

var pullRepository = regexp.MustCompile(`https?://[^/]+/(?:[^/]+/)*?([^/]+)/(?:pull|-/merge_requests)/\d+`)

var linkInText = regexp.MustCompile(`https?://\S+`)

// PullLines is the shape a person posts by hand: the title, then the link
// alone when there is one pull request, or one line per pull request named
// by its repository when there are several.
func PullLines(title string, urls []string) string {
	links := make([]string, 0, len(urls))
	for _, u := range urls {
		if u = strings.TrimSpace(u); u == "" {
			continue
		}
		if link := linkInText.FindString(u); link != "" {
			u = link
		}
		links = append(links, u)
	}
	lines := []string{strings.TrimSpace(title)}
	if len(links) == 1 {
		return strings.Join(append(lines, links[0]), "\n")
	}
	for _, u := range links {
		if m := pullRepository.FindStringSubmatch(u); m != nil {
			lines = append(lines, m[1]+": "+u)
		} else {
			lines = append(lines, u)
		}
	}
	return strings.Join(lines, "\n")
}
