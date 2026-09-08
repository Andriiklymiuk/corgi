package usage

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"strings"
)

// TitleOf is the chat's title as Claude Code shows it on its panel tab: the
// name the user gave it with /rename, else the one Claude generated. Empty
// when the transcript has neither yet. The whole file is scanned, so call
// it once per turn, not per tool.
func TitleOf(transcriptPath string) string {
	f, err := os.Open(transcriptPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), maxLineBytes)
	custom, ai := "", ""
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.Contains(line, []byte(`-title"`)) {
			continue
		}
		var row struct {
			Type        string `json:"type"`
			AITitle     string `json:"aiTitle"`
			CustomTitle string `json:"customTitle"`
		}
		if json.Unmarshal(line, &row) != nil {
			continue
		}
		switch row.Type {
		case "custom-title":
			custom = strings.TrimSpace(row.CustomTitle)
		case "ai-title":
			ai = strings.TrimSpace(row.AITitle)
		}
	}
	if custom != "" {
		return custom
	}
	return ai
}
