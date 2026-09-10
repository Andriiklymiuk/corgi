package usage

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strings"
)

// Summary is what the newest assistant turn said, reduced to one line, and
// the last pull request link it mentioned.
type Summary struct {
	Line string
	PR   string
}

// maxSummaryLen is what a phone row or a menu bar line has room for.
const maxSummaryLen = 140

var pullRequestLink = regexp.MustCompile(`https://(?:github\.com/[^\s)>"]+/pull/\d+|[^\s)>"]+/-/merge_requests/\d+)`)

// SummaryOf reads the transcript's tail for the newest assistant text block
// and the last PR link in that tail.
func SummaryOf(transcriptPath string) (Summary, bool) {
	f, err := os.Open(transcriptPath)
	if err != nil {
		return Summary{}, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return Summary{}, false
	}
	start := info.Size() - contextTail
	if start < 0 {
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return Summary{}, false
	}
	data, err := io.ReadAll(io.LimitReader(f, contextTail))
	if err != nil {
		return Summary{}, false
	}
	return summaryFromTail(data, start > 0)
}

func summaryFromTail(data []byte, truncated bool) (Summary, bool) {
	lines := bytes.Split(data, []byte{'\n'})
	first := 0
	if truncated {
		first = 1
	}
	var out Summary
	for i := len(lines) - 1; i >= first; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 || !bytes.Contains(line, []byte(`"assistant"`)) {
			continue
		}
		var row struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &row) != nil || row.Type != "assistant" {
			continue
		}
		for _, c := range row.Message.Content {
			if c.Type != "text" || strings.TrimSpace(c.Text) == "" {
				continue
			}
			if out.PR == "" {
				if links := pullRequestLink.FindAllString(c.Text, -1); len(links) > 0 {
					out.PR = links[len(links)-1]
				}
			}
			if out.Line == "" {
				out.Line = firstSentence(c.Text)
			}
		}
		if out.Line != "" && out.PR != "" {
			break
		}
	}
	return out, out.Line != ""
}

// firstSentence takes the first line that reads as prose: no headings,
// bullets, fences or bare links.
func firstSentence(text string) string {
	fenced := false
	for _, raw := range strings.Split(text, "\n") {
		s := strings.TrimSpace(raw)
		if strings.HasPrefix(s, "```") {
			fenced = !fenced
			continue
		}
		if fenced || s == "" || strings.HasPrefix(s, "#") || strings.HasPrefix(s, "|") {
			continue
		}
		s = strings.TrimLeft(s, "*-•> ")
		s = strings.ReplaceAll(s, "**", "")
		s = strings.TrimSpace(s)
		if s == "" || strings.HasPrefix(s, "http") {
			continue
		}
		if r := []rune(s); len(r) > maxSummaryLen {
			return string(r[:maxSummaryLen-1]) + "…"
		}
		return s
	}
	return ""
}
