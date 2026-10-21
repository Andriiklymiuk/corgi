package usage

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strings"
)

const backgroundTail = 16 << 20

var (
	backgroundLaunches = []string{"Command running in background", "Async agent launched", "Monitor started"}
	notifiedTool       = regexp.MustCompile(`<tool-use-id>([^<\s]+)</tool-use-id>`)
	notifiedStatus     = regexp.MustCompile(`<status>(completed|failed|stopped|killed|error|cancelled)</status>`)
)

func BackgroundOf(transcriptPath string) int {
	f, err := os.Open(transcriptPath)
	if err != nil {
		return 0
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0
	}
	start := info.Size() - backgroundTail
	if start < 0 {
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return 0
	}
	return backgroundFrom(f, start > 0)
}

func backgroundFrom(r io.Reader, truncated bool) int {
	br := bufio.NewReaderSize(r, 1<<20)
	if truncated {
		_, _ = br.ReadBytes('\n')
	}
	running := map[string]bool{}
	for {
		line, err := br.ReadBytes('\n')
		switch {
		case bytes.Contains(line, []byte("<task-notification>")):
			if notifiedStatus.Match(line) {
				for _, m := range notifiedTool.FindAllSubmatch(line, -1) {
					delete(running, string(m[1]))
				}
			}
		case bytes.Contains(line, []byte(`"tool_result"`)):
			for _, id := range launchedIn(line) {
				running[id] = true
			}
		}
		if err != nil {
			return len(running)
		}
	}
}

func launchedIn(line []byte) []string {
	found := false
	for _, s := range backgroundLaunches {
		found = found || bytes.Contains(line, []byte(s))
	}
	if !found {
		return nil
	}
	var row struct {
		Message struct {
			Content []struct {
				Type      string          `json:"type"`
				ToolUseID string          `json:"tool_use_id"`
				Content   json.RawMessage `json:"content"`
			} `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &row) != nil {
		return nil
	}
	var ids []string
	for _, c := range row.Message.Content {
		if c.Type == "tool_result" && c.ToolUseID != "" && startsWithLaunch(c.Content) {
			ids = append(ids, c.ToolUseID)
		}
	}
	return ids
}

func startsWithLaunch(raw json.RawMessage) bool {
	var text string
	if json.Unmarshal(raw, &text) != nil {
		var parts []struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(raw, &parts) != nil || len(parts) == 0 {
			return false
		}
		text = parts[0].Text
	}
	text = strings.TrimSpace(text)
	for _, s := range backgroundLaunches {
		if strings.HasPrefix(text, s) {
			return true
		}
	}
	return false
}
