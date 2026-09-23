package usage

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"
)

type Context struct {
	Tokens  int64     `json:"tokens"`
	Window  int64     `json:"window"`
	Percent int       `json:"percent"`
	Model   string    `json:"model,omitempty"`
	At      time.Time `json:"at"`
}

const contextTail = 256 << 10

const DefaultWindow = 200_000

func WindowFor(model string) int64 {
	if v := strings.TrimSpace(os.Getenv("CORGI_CONTEXT_WINDOW")); v != "" {
		var n int64
		for _, c := range v {
			if c < '0' || c > '9' {
				n = 0
				break
			}
			n = n*10 + int64(c-'0')
		}
		if n > 0 {
			return n
		}
	}
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "[1m]"), strings.Contains(m, "-1m"),
		strings.Contains(m, "fable"), strings.Contains(m, "mythos"),
		strings.Contains(m, "opus-5"), strings.Contains(m, "sonnet-5"):
		return 1_000_000
	}
	return DefaultWindow
}

func ContextOf(transcriptPath string) (Context, bool) {
	f, err := os.Open(transcriptPath)
	if err != nil {
		return Context{}, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return Context{}, false
	}
	start := info.Size() - contextTail
	if start < 0 {
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return Context{}, false
	}
	data, err := io.ReadAll(io.LimitReader(f, contextTail))
	if err != nil {
		return Context{}, false
	}
	return contextFromTail(data, start > 0)
}

func contextFromTail(data []byte, truncated bool) (Context, bool) {
	lines := bytes.Split(data, []byte{'\n'})
	first := 0
	if truncated {
		first = 1
	}
	for i := len(lines) - 1; i >= first; i-- {
		line := bytes.TrimSpace(lines[i])
		if bytes.Contains(line, []byte(`"token_count"`)) {
			if c, ok := codexContext(line); ok {
				return c, true
			}
			continue
		}
		if len(line) == 0 || !bytes.Contains(line, []byte(`"usage"`)) {
			continue
		}
		var row struct {
			Type      string    `json:"type"`
			Timestamp time.Time `json:"timestamp"`
			Message   struct {
				Model string    `json:"model"`
				Usage *rawUsage `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &row) != nil || row.Type != "assistant" || row.Message.Usage == nil {
			continue
		}
		u := row.Message.Usage
		tokens := u.Input + u.CacheRead + u.CacheWrite
		if tokens <= 0 {
			continue
		}
		c := Context{Tokens: tokens, Model: row.Message.Model, At: row.Timestamp, Window: WindowFor(row.Message.Model)}
		c.Percent = int(tokens * 100 / c.Window)
		if c.Percent > 100 {
			c.Percent = 100
		}
		return c, true
	}
	return Context{}, false
}

// codexContext reads a codex rollout's token_count row: the last turn's
// tokens against the window codex itself reports.
func codexContext(line []byte) (Context, bool) {
	var row struct {
		Timestamp time.Time `json:"timestamp"`
		Payload   struct {
			Type string `json:"type"`
			Info *struct {
				Last struct {
					Total int64 `json:"total_tokens"`
				} `json:"last_token_usage"`
				Window int64 `json:"model_context_window"`
			} `json:"info"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &row) != nil || row.Payload.Type != "token_count" || row.Payload.Info == nil {
		return Context{}, false
	}
	info := row.Payload.Info
	if info.Last.Total <= 0 || info.Window <= 0 {
		return Context{}, false
	}
	c := Context{Tokens: info.Last.Total, Window: info.Window, At: row.Timestamp}
	c.Percent = int(c.Tokens * 100 / c.Window)
	if c.Percent > 100 {
		c.Percent = 100
	}
	return c, true
}
