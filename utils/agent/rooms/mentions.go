package rooms

import (
	"regexp"
	"strings"
)

const MaxMentions = 6

var (
	mentionWord = regexp.MustCompile(`(?i)(?:^|[\s(,;:>])@([a-z0-9][a-z0-9._-]{0,63})`)
	notHandle   = regexp.MustCompile(`[^a-z0-9]+`)
)

func Handle(name string) string {
	return strings.Trim(notHandle.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-"), "-")
}

func Mentions(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range mentionWord.FindAllStringSubmatch(text, -1) {
		handle := Handle(m[1])
		if handle == "" || seen[handle] {
			continue
		}
		seen[handle] = true
		out = append(out, handle)
	}
	return out
}
