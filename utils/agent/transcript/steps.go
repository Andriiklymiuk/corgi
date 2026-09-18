package transcript

import (
	"strings"
	"time"
)

type Step struct {
	ID    string    `json:"id"`
	Kind  string    `json:"kind"`
	At    time.Time `json:"at,omitzero"`
	Text  string    `json:"text,omitempty"`
	Tools []string  `json:"tools,omitempty"`
	Files []string  `json:"files,omitempty"`
}

const maxStepTools = 12

func Steps(entries []Entry) []Step {
	var out []Step
	for _, e := range entries {
		switch e.Kind {
		case "user", "assistant":
			out = append(out, Step{ID: e.ID, Kind: e.Kind, At: e.At, Text: firstLineOf(e.Text, 200)})
		case "tool":
			if len(out) == 0 || out[len(out)-1].Kind == "user" {
				out = append(out, Step{ID: e.ID, Kind: "assistant", At: e.At})
			}
			s := &out[len(out)-1]
			s.Tools = appendCapped(s.Tools, strings.TrimSpace(e.Tool+" "+e.Subject), maxStepTools)
			if editsFile(e.Tool) && e.Subject != "" {
				s.Files = appendUnique(s.Files, e.Subject)
			}
		}
	}
	return out
}

func editsFile(tool string) bool {
	switch tool {
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		return true
	}
	return false
}

func appendCapped(list []string, v string, limit int) []string {
	if v == "" {
		return list
	}
	if len(list) >= limit {
		last := list[len(list)-1]
		if strings.HasPrefix(last, "+") && strings.HasSuffix(last, " more") {
			var n int
			for _, r := range strings.TrimSuffix(strings.TrimPrefix(last, "+"), " more") {
				n = n*10 + int(r-'0')
			}
			list[len(list)-1] = "+" + itoa(n+1) + " more"
			return list
		}
		return append(list, "+1 more")
	}
	return append(list, v)
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}
