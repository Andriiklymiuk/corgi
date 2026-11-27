package watch

import (
	"regexp"
	"strings"
)

const bulletPrefix = "• "

// Rich text shows these as typed, so a text that carries them stays plain.
var slackMarkup = regexp.MustCompile("[<`*]|&(?:amp|lt|gt);|:[a-z0-9_+-]+:")

const linkTrailingPunctuation = ".,;:!?)"

func richTextElements(line string) []map[string]any {
	var out []map[string]any
	addText := func(s string) {
		if s != "" {
			out = append(out, map[string]any{"type": "text", "text": s})
		}
	}
	rest := line
	for {
		at := linkInText.FindStringIndex(rest)
		if at == nil {
			break
		}
		link := strings.TrimRight(rest[at[0]:at[1]], linkTrailingPunctuation)
		addText(rest[:at[0]])
		out = append(out, map[string]any{"type": "link", "url": link})
		rest = rest[at[0]+len(link):]
	}
	addText(rest)
	return out
}

func richTextSection(text string) map[string]any {
	return map[string]any{"type": "rich_text_section", "elements": richTextElements(text)}
}

func slackBulletListBlocks(text string) []map[string]any {
	if !strings.Contains(text, bulletPrefix) || slackMarkup.MatchString(text) {
		return nil
	}
	var elements, items []map[string]any
	var plain []string
	afterList, hasList := false, false
	flushPlain := func() {
		body := strings.Trim(strings.Join(plain, "\n"), "\n")
		gap := afterList && len(plain) > 0 && strings.TrimSpace(plain[0]) == ""
		plain = nil
		if body == "" {
			return
		}
		if gap {
			body = "\n" + body
		}
		elements = append(elements, richTextSection(body))
	}
	flushList := func() {
		if len(items) == 0 {
			return
		}
		elements = append(elements, map[string]any{"type": "rich_text_list", "style": "bullet", "indent": 0, "elements": items})
		items, afterList, hasList = nil, true, true
	}
	for line := range strings.SplitSeq(text, "\n") {
		if item, ok := strings.CutPrefix(strings.TrimLeft(line, " \t"), bulletPrefix); ok && strings.TrimSpace(item) != "" {
			flushPlain()
			items = append(items, richTextSection(strings.TrimSpace(item)))
			continue
		}
		flushList()
		plain = append(plain, line)
	}
	flushList()
	flushPlain()
	if !hasList {
		return nil
	}
	return []map[string]any{{"type": "rich_text", "elements": elements}}
}
