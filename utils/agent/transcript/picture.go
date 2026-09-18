package transcript

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
)

var ErrNoPicture = errors.New("no picture there")

type imagePart struct {
	Type   string `json:"type"`
	Source struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
	} `json:"source"`
}

func imageParts(raw json.RawMessage) []imagePart {
	var parts []imagePart
	if json.Unmarshal(raw, &parts) != nil {
		return nil
	}
	var out []imagePart
	for _, p := range parts {
		if p.Type == "image" && p.Source.Type == "base64" && strings.HasPrefix(p.Source.MediaType, "image/") {
			out = append(out, p)
		}
	}
	return out
}

func resultPictureType(raw json.RawMessage) string {
	if parts := imageParts(raw); len(parts) > 0 {
		return parts[0].Source.MediaType
	}
	return ""
}

// Picture returns the first image in the tool result whose entry id is id:
// the row uuid, or uuid/<block index> past the first block.
func Picture(path, id string) (mime string, data []byte, err error) {
	uuid, index := id, 0
	if i := strings.LastIndex(id, "/"); i > 0 {
		n, err := strconv.Atoi(id[i+1:])
		if err != nil {
			return "", nil, ErrNoPicture
		}
		uuid, index = id[:i], n
	}
	if uuid == "" {
		return "", nil, ErrNoPicture
	}
	f, err := os.Open(path)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 256<<10)
	needle := []byte(`"` + uuid + `"`)
	for {
		line, readErr := r.ReadBytes('\n')
		if len(line) > 0 && len(line) <= maxLine && bytes.Contains(line, needle) {
			var row row
			if json.Unmarshal(line, &row) == nil && row.UUID == uuid {
				return pictureIn(row, index)
			}
		}
		if readErr != nil {
			return "", nil, ErrNoPicture
		}
	}
}

func pictureIn(r row, index int) (string, []byte, error) {
	var blocks []block
	if json.Unmarshal(r.Message.Content, &blocks) != nil || index >= len(blocks) || blocks[index].Type != "tool_result" {
		return "", nil, ErrNoPicture
	}
	parts := imageParts(blocks[index].Content)
	if len(parts) == 0 {
		return "", nil, ErrNoPicture
	}
	data, err := base64.StdEncoding.DecodeString(parts[0].Source.Data)
	if err != nil {
		return "", nil, ErrNoPicture
	}
	return parts[0].Source.MediaType, data, nil
}
