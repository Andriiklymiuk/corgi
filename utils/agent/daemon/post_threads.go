package daemon

import (
	"strings"
	"sync"
	"time"
)

const (
	postThreadWindow   = 12 * time.Hour
	postThreadMaxLines = 6
	postThreadOldLine  = 200
)

type postThread struct {
	lines   []string
	retract func()
	at      time.Time
}

type postThreads struct {
	mu   sync.Mutex
	byID map[string]*postThread
}

func postThreadID(a attention) string {
	if a.session != "" || a.run != "" {
		return ""
	}
	if !strings.HasPrefix(a.link, "https://") && !strings.HasPrefix(a.link, "http://") {
		return ""
	}
	return a.workspace + "\x00" + a.link
}

func (p *postThreads) story(id, body string, now time.Time) (story string, earlier func()) {
	for key, thread := range p.byID {
		if now.Sub(thread.at) > postThreadWindow {
			delete(p.byID, key)
		}
	}
	thread := p.byID[id]
	if thread == nil || thread.retract == nil {
		return body, nil
	}
	lines := make([]string, 0, len(thread.lines)+1)
	for _, line := range thread.lines {
		lines = append(lines, clipText(line, postThreadOldLine))
	}
	lines = append(lines, body)
	if len(lines) > postThreadMaxLines {
		lines = lines[len(lines)-postThreadMaxLines:]
	}
	return strings.Join(lines, "\n\n"), thread.retract
}

func (p *postThreads) keep(id, body string, retract func(), now time.Time) {
	if p.byID == nil {
		p.byID = map[string]*postThread{}
	}
	thread := p.byID[id]
	if thread == nil {
		thread = &postThread{}
		p.byID[id] = thread
	}
	thread.lines = append(thread.lines, body)
	if len(thread.lines) > postThreadMaxLines {
		thread.lines = thread.lines[len(thread.lines)-postThreadMaxLines:]
	}
	thread.retract, thread.at = retract, now
}

func (d *Daemon) ringAwayThreaded(a attention, link string) {
	id := postThreadID(a)
	if id == "" || d.NotifyAway == nil {
		d.ringAway(a, link)
		return
	}
	d.postThreads.mu.Lock()
	defer d.postThreads.mu.Unlock()
	now := time.Now()
	told := a
	var earlier func()
	told.story, earlier = d.postThreads.story(id, a.body, now)
	retract := d.ringAway(told, link)
	if retract == nil && earlier != nil {
		return
	}
	d.postThreads.keep(id, a.body, retract, now)
	if earlier != nil {
		go earlier()
	}
}
