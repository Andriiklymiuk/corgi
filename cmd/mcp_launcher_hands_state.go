package cmd

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils/agent/screen"
)

const (
	handsStatePath   = "/launch/screen/hands/state"
	handsActiveFor   = 12 * time.Second
	handsStepsKept   = 30
	handsPauseExpiry = 30 * time.Minute
)

type handsStep struct {
	At   time.Time `json:"at"`
	By   string    `json:"by"`
	Do   string    `json:"do"`
	Text string    `json:"text,omitempty"`
}

type handsState struct {
	Active   bool        `json:"active"`
	By       string      `json:"by,omitempty"`
	Step     int         `json:"step"`
	Paused   bool        `json:"paused"`
	PausedBy string      `json:"pausedBy,omitempty"`
	PausedAt *time.Time  `json:"pausedAt,omitempty"`
	Waiting  bool        `json:"waiting"`
	Steps    []handsStep `json:"steps"`
}

func stepText(r handsRequest) string {
	switch r.Do {
	case "act":
		kinds := make([]string, 0, len(r.Actions))
		for _, a := range r.Actions {
			if len(kinds) == 0 || kinds[len(kinds)-1] != a.Do {
				kinds = append(kinds, a.Do)
			}
		}
		if len(kinds) > 5 {
			kinds = append(kinds[:5], "…")
		}
		return strings.Join(kinds, ", ")
	case "menu":
		return strings.Join(r.Path, " › ")
	case "open":
		if r.App != "" {
			return r.App
		}
		return "a link"
	case "activate":
		return r.App
	case "look":
		return r.Window
	}
	return ""
}

func (h *handsHub) pausedNow(now time.Time) bool {
	return h.pausedBy != "" && now.Sub(h.pausedAt) < handsPauseExpiry
}

func (h *handsHub) admit(req handsRequest, by string, now time.Time) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pausedNow(now) {
		h.waiting = true
		return errors.New(h.pausedBy + " took the Mac and you are paused: do nothing on it, wait a minute and try this step again; it goes through once they let you carry on")
	}
	h.waiting = false
	h.holder = by
	h.step++
	h.lastStep = now
	h.steps = append(h.steps, handsStep{At: now, By: by, Do: req.Do, Text: stepText(req)})
	if len(h.steps) > handsStepsKept {
		h.steps = h.steps[len(h.steps)-handsStepsKept:]
	}
	return nil
}

func (h *handsHub) pause(by string, now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pausedNow(now) {
		return
	}
	h.pausedBy = by
	h.pausedAt = now
	h.steps = append(h.steps, handsStep{At: now, By: by, Do: "paused"})
}

func (h *handsHub) resume(by string, now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.pausedNow(now) {
		h.pausedBy = ""
		return
	}
	h.pausedBy = ""
	h.waiting = false
	h.steps = append(h.steps, handsStep{At: now, By: by, Do: "resumed"})
}

func (h *handsHub) state(now time.Time) handsState {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := handsState{Step: h.step, Steps: append([]handsStep{}, h.steps...), Waiting: h.waiting}
	if h.pausedNow(now) {
		at := h.pausedAt
		st.Paused, st.PausedBy, st.PausedAt = true, h.pausedBy, &at
		st.By = h.holder
		return st
	}
	if h.holder != "" && now.Sub(h.lastStep) < handsActiveFor {
		st.Active, st.By = true, h.holder
	}
	return st
}

type handsStateRequest struct {
	Do string `json:"do"`
}

func serveHandsState(w http.ResponseWriter, r *http.Request, who string, mayPause bool) {
	now := time.Now()
	if r.Method == http.MethodGet {
		writeLaunchJSON(w, hands.state(now))
		return
	}
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET or POST")
		return
	}
	var req handsStateRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeLaunchError(w, http.StatusBadRequest, "could not read the request")
		return
	}
	if !mayPause {
		writeLaunchError(w, http.StatusForbidden, who+" may watch "+macName()+", not control it")
		return
	}
	switch req.Do {
	case "pause":
		hands.pause(who, now)
	case "resume":
		hands.resume(who, now)
	default:
		writeLaunchError(w, http.StatusBadRequest, "do is pause or resume")
		return
	}
	writeLaunchJSON(w, hands.state(now))
}

func launchHandsStateHandler(w http.ResponseWriter, r *http.Request) {
	dir, d, ok := screenDevice(w, r)
	if !ok {
		return
	}
	s, err := screen.Load(dir)
	if err != nil || !s.Enabled {
		writeLaunchError(w, http.StatusForbidden, "remote screen is off on "+macName())
		return
	}
	role := s.RoleFor(d.TokenHash)
	if role == screen.RoleOff {
		writeLaunchError(w, http.StatusForbidden, macName()+" turned remote screen off for "+d.Name)
		return
	}
	serveHandsState(w, r, d.Name, role == screen.RoleControl || (screenMacBar(d) && fromThisMachine(r)))
}

func launchHandsStateLocal() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setLaunchHeaders(w)
		if !handsKeyOK(r) {
			writeLaunchError(w, http.StatusForbidden, "only this Mac, with its hands key")
			return
		}
		serveHandsState(w, r, "the person at "+macName(), true)
	})
}
