package cmd

import (
	"regexp"
	"sort"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils/agent/rooms"
)

const (
	roomWorkspacePrefix = "ws/"
	roomTicketPrefix    = "ticket/"
	roomKindWorkspace   = "workspace"
	roomKindTicket      = "ticket"
)

type roomPlace struct {
	Room     string    `json:"room"`
	Kind     string    `json:"kind"`
	Title    string    `json:"title"`
	Sub      string    `json:"sub,omitempty"`
	Agents   []string  `json:"agents"`
	Messages int       `json:"messages"`
	Last     time.Time `json:"last,omitzero"`
}

var notRoomLetters = regexp.MustCompile(`[^a-z0-9]+`)

func roomSlug(name string) string {
	slug := strings.Trim(notRoomLetters.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], "-")
	}
	return slug
}

func workspaceRoom(workspace string) string { return roomWorkspacePrefix + roomSlug(workspace) }
func ticketRoom(ticket string) string       { return roomTicketPrefix + roomSlug(ticket) }

func roomPlaces(agents []roomAgent, existing []rooms.Info) []roomPlace {
	byRoom := map[string]*roomPlace{}
	var order []string
	place := func(room, kind, title, sub string) *roomPlace {
		if p, ok := byRoom[room]; ok {
			return p
		}
		p := &roomPlace{Room: room, Kind: kind, Title: title, Sub: sub, Agents: []string{}}
		byRoom[room] = p
		order = append(order, room)
		return p
	}
	for _, a := range agents {
		if roomSlug(a.Workspace) != "" {
			p := place(workspaceRoom(a.Workspace), roomKindWorkspace, a.Workspace, "")
			p.Agents = append(p.Agents, a.Handle)
		}
		if roomSlug(a.Ticket) != "" {
			p := place(ticketRoom(a.Ticket), roomKindTicket, a.Ticket, a.Workspace)
			p.Agents = append(p.Agents, a.Handle)
		}
	}
	for _, info := range existing {
		switch {
		case strings.HasPrefix(info.Name, roomWorkspacePrefix):
			place(info.Name, roomKindWorkspace, strings.TrimPrefix(info.Name, roomWorkspacePrefix), "")
		case strings.HasPrefix(info.Name, roomTicketPrefix):
			place(info.Name, roomKindTicket, strings.ToUpper(strings.TrimPrefix(info.Name, roomTicketPrefix)), "")
		default:
			continue
		}
		byRoom[info.Name].Messages, byRoom[info.Name].Last = info.Messages, info.Last
	}
	out := []roomPlace{}
	for _, room := range order {
		out = append(out, *byRoom[room])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind == roomKindWorkspace
		}
		return out[i].Last.After(out[j].Last)
	})
	return out
}

func agentsOfPlace(room string, agents []roomAgent) ([]roomAgent, bool) {
	var out []roomAgent
	for _, a := range agents {
		if room == workspaceRoom(a.Workspace) || room == ticketRoom(a.Ticket) {
			out = append(out, a)
		}
	}
	isPlace := strings.HasPrefix(room, roomWorkspacePrefix) || strings.HasPrefix(room, roomTicketPrefix)
	return out, isPlace
}
