package rooms

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	CanRead  = "read"
	CanPost  = "post"
	Everyone = "everyone"
)

type Grant struct {
	ID   string    `json:"id"`
	Who  string    `json:"who"`
	Path string    `json:"path"`
	Can  []string  `json:"can"`
	At   time.Time `json:"at"`
}

func grantsPath(agentDir string) string { return filepath.Join(Dir(agentDir), "grants.json") }

func Grants(agentDir string) []Grant {
	raw, err := os.ReadFile(grantsPath(agentDir))
	if err != nil {
		return []Grant{}
	}
	var out []Grant
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = []Grant{}
	}
	return out
}

func saveGrants(agentDir string, list []Grant) error {
	if err := os.MkdirAll(Dir(agentDir), 0o700); err != nil {
		return err
	}
	raw, _ := json.MarshalIndent(list, "", "  ")
	return os.WriteFile(grantsPath(agentDir), raw, 0o600)
}

func ParseGrant(spec string) (path string, can []string, err error) {
	path, rights, ok := strings.Cut(spec, "=")
	path = strings.Trim(strings.ToLower(strings.TrimSpace(path)), "/")
	if !ok {
		return "", nil, fmt.Errorf("a grant is path=rights: security/=read,post or all=none, not %q", spec)
	}
	if path != "all" {
		if _, err := CleanName(path); err != nil {
			return "", nil, err
		}
	} else {
		path = ""
	}
	for _, r := range strings.Split(rights, ",") {
		switch r = strings.ToLower(strings.TrimSpace(r)); r {
		case CanRead, CanPost:
			can = append(can, r)
		case "all":
			can = []string{CanRead, CanPost}
		case "none", "":
		default:
			return "", nil, fmt.Errorf("a right is read, post, all or none, not %q", r)
		}
	}
	sort.Strings(can)
	if can == nil {
		can = []string{}
	}
	return path, can, nil
}

func AddGrant(agentDir, who, spec string, now time.Time) (Grant, error) {
	who = strings.TrimSpace(who)
	if who == "" {
		return Grant{}, fmt.Errorf("say who: a paired device's name, or %s", Everyone)
	}
	path, can, err := ParseGrant(spec)
	if err != nil {
		return Grant{}, err
	}
	unlock, err := lock(agentDir)
	if err != nil {
		return Grant{}, err
	}
	defer unlock()
	list := Grants(agentDir)
	next := 1
	kept := list[:0]
	for _, g := range list {
		var n int
		if _, _ = fmt.Sscanf(g.ID, "g%d", &n); n >= next {
			next = n + 1
		}
		if strings.EqualFold(g.Who, who) && g.Path == path {
			continue
		}
		kept = append(kept, g)
	}
	g := Grant{ID: fmt.Sprintf("g%d", next), Who: who, Path: path, Can: can, At: now.UTC()}
	return g, saveGrants(agentDir, append(kept, g))
}

func RevokeGrant(agentDir, id string) (Grant, error) {
	unlock, err := lock(agentDir)
	if err != nil {
		return Grant{}, err
	}
	defer unlock()
	list := Grants(agentDir)
	for i, g := range list {
		if g.ID == strings.TrimSpace(id) {
			return g, saveGrants(agentDir, append(list[:i:i], list[i+1:]...))
		}
	}
	return Grant{}, fmt.Errorf("no grant %s", id)
}

type Verdict struct {
	Allowed bool   `json:"allowed"`
	Because string `json:"because"`
	Grant   string `json:"grant,omitempty"`
}

func covers(path, room string) bool {
	return path == "" || room == path || strings.HasPrefix(room, path+"/")
}

func Explain(list []Grant, who, room, action string) Verdict {
	var best *Grant
	score := func(g Grant) int {
		s := 2 * len(g.Path)
		if !strings.EqualFold(g.Who, Everyone) {
			s++
		}
		return s
	}
	for i := range list {
		g := &list[i]
		if (!strings.EqualFold(g.Who, who) && !strings.EqualFold(g.Who, Everyone)) || !covers(g.Path, room) {
			continue
		}
		if best == nil || score(*g) > score(*best) {
			best = g
		}
	}
	if best == nil {
		return Verdict{Because: fmt.Sprintf("no grant covers %s for %s", room, who)}
	}
	scope := best.Path
	if scope == "" {
		scope = "every room"
	} else {
		scope += "/"
	}
	for _, c := range best.Can {
		if c == action {
			return Verdict{Allowed: true, Grant: best.ID, Because: fmt.Sprintf("%s lets %s %s in %s", best.ID, best.Who, strings.Join(best.Can, " and "), scope)}
		}
	}
	rights := strings.Join(best.Can, " and ")
	if rights == "" {
		rights = "nothing"
	}
	return Verdict{Grant: best.ID, Because: fmt.Sprintf("%s gives %s %s in %s, not %s", best.ID, best.Who, rights, scope, action)}
}

func Covered(list []Grant, who string) bool {
	for _, g := range list {
		if strings.EqualFold(g.Who, who) || strings.EqualFold(g.Who, Everyone) {
			return true
		}
	}
	return false
}
