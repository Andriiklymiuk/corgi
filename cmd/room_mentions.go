package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/pairing"
	"andriiklymiuk/corgi/utils/agent/push"
	"andriiklymiuk/corgi/utils/agent/rooms"
	"andriiklymiuk/corgi/utils/agent/sessions"
)

const (
	roomViaDevice    = "device"
	roomViaAgent     = "agent"
	roomHere         = "here"
	roomMentionSpan  = 10 * time.Minute
	roomMentionLimit = 30
	roomTextInPrompt = 2000
)

var roomPeople = map[string]bool{"you": true, "me": true, "human": true, "owner": true}

type roomAgent struct {
	Handle    string `json:"handle"`
	Name      string `json:"name"`
	Workspace string `json:"workspace,omitempty"`
	Branch    string `json:"branch,omitempty"`
	Ticket    string `json:"ticket,omitempty"`
	Status    string `json:"status"`
	Busy      string `json:"busy,omitempty"`
	id        string
}

func roomAgentsOf(list []sessions.Session) []roomAgent {
	out := []roomAgent{}
	seen := map[string]bool{}
	for _, s := range list {
		if s.Status == sessions.StatusGone || s.Status == sessions.StatusStale {
			continue
		}
		name := firstNonEmpty(s.Display, s.Label, s.ID)
		handle := rooms.Handle(name)
		if handle == "" || seen[handle] || handle == roomHere || roomPeople[handle] {
			short := s.ID
			if len(short) > 4 {
				short = short[:4]
			}
			handle = strings.Trim(handle+"-"+rooms.Handle(short), "-")
		}
		if seen[handle] {
			continue
		}
		seen[handle] = true
		agent := roomAgent{Handle: handle, Name: name, Workspace: s.Label, Branch: s.Branch, Ticket: s.Ticket, Status: string(s.Status), id: s.ID}
		switch {
		case s.Pending != nil:
			agent.Busy = "waits on a permission"
		case s.Status == sessions.StatusLimited:
			agent.Busy = "is at a limit"
		}
		out = append(out, agent)
	}
	return out
}

var roomAgents = func(dir string) []roomAgent {
	rep, err := readBoard(dir)
	if err != nil {
		return []roomAgent{}
	}
	return roomAgentsOf(rep.Sessions)
}

func roomAuthor(dir string) (string, bool) {
	rep, err := readBoard(dir)
	if err != nil {
		return "", false
	}
	s, ok := sessionFromEnv(rep.State)
	if !ok {
		return "", false
	}
	for _, a := range roomAgentsOf(rep.Sessions) {
		if a.id == s.ID {
			return a.Handle, true
		}
	}
	return "", false
}

type roomDelivery struct {
	Message rooms.Message     `json:"message"`
	Sent    []string          `json:"sent,omitempty"`
	Missed  map[string]string `json:"missed,omitempty"`
}

func roomWriters(dir, room string) map[string]bool {
	out := map[string]bool{}
	list, _ := rooms.Read(dir, rooms.Query{Room: room})
	for _, m := range list {
		out[rooms.Handle(m.By)] = true
	}
	return out
}

func roomMentionsLately(dir, room string, now time.Time) int {
	n := 0
	list, _ := rooms.Read(dir, rooms.Query{Room: room})
	for _, m := range list {
		if len(m.To) > 0 && now.Sub(m.At) < roomMentionSpan {
			n++
		}
	}
	return n
}

func roomTargets(dir string, m rooms.Message, mayType bool, now time.Time) ([]roomAgent, map[string]string, bool) {
	missed := map[string]string{}
	named := rooms.Mentions(m.Text)
	people := false
	var asked []string
	for _, h := range named {
		if roomPeople[h] {
			people = true
			continue
		}
		asked = append(asked, h)
	}
	agents := roomAgents(dir)
	var inPlace []roomAgent
	if len(asked) == 0 && !people && m.Via != roomViaAgent {
		var isPlace bool
		if inPlace, isPlace = agentsOfPlace(m.Room, agents); isPlace && len(inPlace) == 0 {
			missed[m.Room] = "no live session there now; it is kept for the next one"
		}
	}
	if len(asked) == 0 && len(inPlace) == 0 {
		return nil, missed, people
	}
	if !mayType {
		for _, a := range inPlace {
			missed[a.Handle] = "a read-only device cannot write to a session"
		}
		for _, h := range asked {
			missed[h] = "a read-only device cannot write to a session"
		}
		return nil, missed, people
	}
	byHandle := map[string]roomAgent{}
	for _, a := range agents {
		byHandle[a.Handle] = a
	}
	author := rooms.Handle(m.By)
	var want []roomAgent
	picked := map[string]bool{}
	add := func(a roomAgent) {
		if a.Handle == author || picked[a.Handle] {
			return
		}
		picked[a.Handle] = true
		want = append(want, a)
	}
	for _, a := range inPlace {
		add(a)
	}
	for _, h := range asked {
		if h == roomHere {
			writers := roomWriters(dir, m.Room)
			for _, a := range agents {
				if writers[a.Handle] {
					add(a)
				}
			}
			continue
		}
		a, ok := byHandle[h]
		if !ok {
			missed[h] = "no live session by that name"
			continue
		}
		add(a)
	}
	var out []roomAgent
	for _, a := range want {
		switch {
		case a.Busy != "":
			missed[a.Handle] = a.Name + " " + a.Busy
		case len(out) >= rooms.MaxMentions:
			missed[a.Handle] = fmt.Sprintf("a message reaches up to %d agents", rooms.MaxMentions)
		default:
			out = append(out, a)
		}
	}
	if len(out) > 0 && roomMentionsLately(dir, m.Room, now) >= roomMentionLimit {
		for _, a := range out {
			missed[a.Handle] = "too many mentions in this room in ten minutes"
		}
		return nil, missed, people
	}
	return out, missed, people
}

func roomPrompt(m rooms.Message) string {
	thread := firstNonEmpty(m.Thread, m.ID)
	text := strings.Join(strings.Fields(m.Text), " ")
	if len(text) > roomTextInPrompt {
		text = text[:roomTextInPrompt] + "…"
	}
	return fmt.Sprintf("Message %s in room %s from %s: %s (Answer in the room, not here: corgi agent room post %s --thread %s \"your answer\". The talk so far: corgi agent room read %s --thread %s. Keep the answer short; it is read on a phone.)",
		m.ID, m.Room, m.By, text, m.Room, thread, m.Room, thread)
}

var roomPush = func(dir string, m push.Message) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	_ = push.Load(dir).Send(ctx, m)
}

func roomRootFromDevice(dir string, m rooms.Message) bool {
	if m.Thread == "" {
		return false
	}
	list, _ := rooms.Read(dir, rooms.Query{Room: m.Room, Thread: m.Thread})
	for _, old := range list {
		if old.ID == m.Thread {
			return old.Via == roomViaDevice
		}
	}
	return false
}

func roomDeviceNamed(dir string, handles []string) bool {
	store, err := pairing.Load(pairing.StorePath(dir))
	if err != nil {
		return false
	}
	for _, h := range handles {
		for _, d := range store.Devices {
			if !d.Peer() && rooms.Handle(d.Name) == h {
				return true
			}
		}
	}
	return false
}

var roomDaemon = func(dir string) (func(), bool) {
	info, err := daemon.ReadInfo(dir)
	if err != nil || info == nil {
		return func() {}, false
	}
	return func() { daemon.Nudge(info) }, true
}

func postToRoom(dir string, m rooms.Message, mayType bool, now time.Time) (roomDelivery, error) {
	targets, missed, people := roomTargets(dir, m, mayType, now)
	nudge := func() {}
	if len(targets) > 0 {
		var up bool
		if nudge, up = roomDaemon(dir); !up {
			for _, a := range targets {
				missed[a.Handle] = "the corgi daemon is not running on that machine"
			}
			targets = nil
		}
	}
	for _, a := range targets {
		m.To = append(m.To, a.Handle)
	}
	posted, err := rooms.Post(dir, m, now)
	if err != nil {
		return roomDelivery{}, err
	}
	out := roomDelivery{Message: posted}
	for _, a := range targets {
		if _, err := command.Write(dir, command.Command{Action: command.ActionSend, SessionID: a.id, Text: roomPrompt(posted), Enter: true, Source: "room"}); err != nil {
			missed[a.Handle] = err.Error()
			continue
		}
		out.Sent = append(out.Sent, a.Handle)
	}
	if len(out.Sent) > 0 {
		nudge()
	}
	if len(missed) > 0 {
		out.Missed = missed
	}
	if posted.Via != roomViaDevice && (people || roomRootFromDevice(dir, posted) || roomDeviceNamed(dir, rooms.Mentions(posted.Text))) {
		body := strings.Join(strings.Fields(posted.Text), " ")
		if len(body) > 240 {
			body = body[:240] + "…"
		}
		roomPush(dir, push.Message{Title: posted.By + " in " + posted.Room, Body: body, Thread: "room:" + posted.Room,
			Data: map[string]string{"kind": "room", "room": posted.Room, "thread": firstNonEmpty(posted.Thread, posted.ID)}})
	}
	return out, nil
}
