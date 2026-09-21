package watch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils/atomicfile"
)

type Hand struct {
	At    time.Time `json:"at"`
	To    string    `json:"to"`
	Label string    `json:"label,omitempty"`
	By    string    `json:"by,omitempty"`
}

type HandLog struct {
	mu    sync.Mutex
	path  string
	Hands map[string]Hand `json:"hands,omitempty"`
}

const handKeep = 200

func handsPath(agentDir string) string { return filepath.Join(agentDir, "watch", "handed.json") }

func LoadHands(agentDir string) *HandLog {
	l := &HandLog{path: handsPath(agentDir), Hands: map[string]Hand{}}
	if data, err := os.ReadFile(l.path); err == nil {
		_ = json.Unmarshal(data, l)
	}
	if l.Hands == nil {
		l.Hands = map[string]Hand{}
	}
	return l
}

func (l *HandLog) Get(key string) (Hand, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	h, ok := l.Hands[key]
	return h, ok
}

func (l *HandLog) Set(key string, h Hand) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if key == "" {
		return nil
	}
	l.Hands[key] = h
	for len(l.Hands) > handKeep {
		oldestKey, oldest := "", time.Time{}
		for k, v := range l.Hands {
			if oldest.IsZero() || v.At.Before(oldest) {
				oldestKey, oldest = k, v.At
			}
		}
		delete(l.Hands, oldestKey)
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(l.path, data, 0o600)
}

func HandoverLine(e Event) string {
	from := ""
	if e.Author != "" {
		from = " from " + e.Author
	}
	at := ""
	if e.URL != "" {
		at = "\n" + e.URL
	}
	switch e.Kind {
	case KindPRReview, KindPRComment:
		if e.Body == "" {
			return ""
		}
		return "Review feedback on " + e.Ref + from + ":\n" + e.Body + "\n\nAddress it on this branch, run the tests, and push." + at
	case KindReviewRequested:
		return "A review was asked for on " + e.Ref + from + ". Give the diff a last read, make sure the tests pass, and say when it is ready." + at
	case KindCIFailed:
		return "The build went red on " + e.Ref + ": " + e.Title + ". Read the failing job, fix it on this branch, and push." + at
	case KindChatMention:
		link := PullLinkOf(e)
		if e.Body == "" || link == "" {
			return ""
		}
		return firstOr(e.Author, "Someone") + " wrote about " + link + " in chat:\n" + e.Body + "\n\nTake it into account; a nit or an ask is work on this branch, an approval is not." + at
	case KindIssueComment:
		if e.Body == "" {
			return ""
		}
		author := e.Author
		if author == "" {
			author = "Someone"
		}
		return author + " commented on " + e.Ref + ":\n" + e.Body + "\n\nTake it into account." + at
	}
	return ""
}

func PullLinkOf(e Event) string {
	link := e.URL
	if i := indexByte(link, '#'); i > 0 {
		link = link[:i]
	}
	if PullRef(link) != "" {
		return link
	}
	for _, l := range e.Links {
		if PullRef(l) != "" {
			return l
		}
	}
	return ""
}

func firstOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
